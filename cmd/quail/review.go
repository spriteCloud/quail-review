// `quail review` — @quail PR reviewer. Fetches the PR's unified diff,
// asks the LLM to summarise it as `## Core Changes` + `## Verdict`, and
// posts the result as a single markdown comment on the PR. Triggered
// by an `issue_comment` workflow filtering on `@quail` in the body.
//
// Ponytail cuts: no formal PR review (Reviews API) — issue comment
// only. No diff chunking — bail with a short comment when the diff is
// over 200 KB. No caching — every @quail is a fresh call. Add when
// each cut starts to hurt.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spriteCloud/quail-core/config"
	"github.com/spriteCloud/quail-core/gh"
	"github.com/spriteCloud/quail-core/llm"
	rlog "github.com/spriteCloud/quail-core/log"
)

// maxDiffBytes is the soft cap on how much diff we send to the LLM.
// Above this we truncate to the head + a note; below it we send the
// diff whole. 512 KB (~130k tokens at ~4 chars/token) fits comfortably
// in modern chat-completion context windows with headroom for the
// system prompt + response.
const maxDiffBytes = 512 * 1024

const reviewSystemPrompt = "You are a senior code reviewer commenting on a GitHub pull request.\n" +
	"\n" +
	"Your ENTIRE response must be a markdown comment with EXACTLY these two sections and nothing else:\n" +
	"\n" +
	"## Core Changes\n" +
	"\n" +
	"- 3 to 8 bullet points, imperative voice, each naming a meaningful change\n" +
	"- Group by subsystem or feature when it helps clarity\n" +
	"- Do NOT enumerate every file — summarise\n" +
	"\n" +
	"---\n" +
	"\n" +
	"## Verdict\n" +
	"\n" +
	"**Approve** | **Request changes** | **Comment**: <one short paragraph explaining why>\n" +
	"\n" +
	"HARD RULES — violations mean the review is unusable:\n" +
	"- Your FIRST characters MUST be `## Core Changes` — no THOUGHT:, no preamble, no meta-commentary, no chain-of-thought, no \"Let me…\", no \"First,…\", no plan-of-action.\n" +
	"- Your LAST content MUST be the Verdict line — no closing sign-off, no next-step list, no \"Let's do it\".\n" +
	"- Do NOT propose running workflows, committing, pushing, or taking any action — you are ONLY writing a review comment.\n" +
	"- Do NOT wrap the whole message in a code fence.\n" +
	"- Do not invent context you cannot infer from the diff.\n" +
	"- Trivial diff (typo, comment, whitespace) → Approve concisely.\n" +
	"- Correctness risk, security issue, or breaking change → Request changes and name it specifically.\n" +
	"- Otherwise → Comment.\n" +
	"\n" +
	"EXAMPLE — this is EXACTLY the shape your response must take:\n" +
	"\n" +
	"## Core Changes\n" +
	"\n" +
	"- Bumps @playwright/test from 1.44 → 1.47 in package.json\n" +
	"- Replaces deprecated `page.waitForTimeout` with `page.waitForLoadState` in 4 specs\n" +
	"- Adds a retry wrapper around the flaky login step in `tests/auth/login.spec.ts`\n" +
	"\n" +
	"---\n" +
	"\n" +
	"## Verdict\n" +
	"\n" +
	"**Approve**: The bump is a minor version with no known breaking changes for this suite, and the deprecation swap is mechanical. The retry wrapper caps at 2 attempts so it can't mask a real regression.\n" +
	"\n" +
	"END OF EXAMPLE. Now write YOUR review of the actual PR diff below, in exactly that shape.\n"

func newReviewCmd() *cobra.Command {
	var pr int
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Post an @quail PR review verdict as a markdown comment.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			if err := cfg.Validate(); err != nil {
				return err
			}
			if pr == 0 {
				pr = cfg.PRNumber
			}
			if pr == 0 {
				pr = readPRFromEvent()
			}
			if pr == 0 {
				return fmt.Errorf("missing --pr; set $QUAIL_PR or run inside a pull_request / issue_comment event")
			}
			cfg.PRNumber = pr
			return runReview(cmd.Context(), cfg)
		},
	}
	cmd.Flags().IntVar(&pr, "pr", 0, "PR number")
	return cmd
}

func runReview(ctx context.Context, cfg config.Config) error {
	client, err := gh.New(ctx, cfg)
	if err != nil {
		return err
	}
	rawDiff, prObj, err := client.FetchDiff(ctx, cfg.PRNumber)
	if err != nil {
		return fmt.Errorf("fetch pr diff: %w", err)
	}
	rlog.Info("review: fetched diff", "pr", cfg.PRNumber,
		"title", prObj.GetTitle(), "bytes", len(rawDiff))

	if strings.TrimSpace(rawDiff) == "" {
		return postAndLog(ctx, client, cfg.PRNumber,
			"## Verdict\n\n**Comment**: the PR diff is empty — nothing to review.")
	}
	// v0.10.16 — truncate instead of bailing when the diff is
	// oversized. A truncated head + a clear note beats no review at
	// all; the model still gets enough signal for a Core Changes
	// summary and a directional Verdict.
	truncated := false
	if len(rawDiff) > maxDiffBytes {
		rawDiff = rawDiff[:maxDiffBytes]
		truncated = true
		rlog.Info("review: truncated oversized diff",
			"kept_bytes", maxDiffBytes, "cap_kb", maxDiffBytes/1024)
	}

	lm := llm.New(cfg)
	if !lm.Enabled() {
		return fmt.Errorf("review: no LLM configured (set OPENAI_API_KEY / QUAIL_MODEL)")
	}

	userPrompt := buildReviewUserPrompt(prObj.GetTitle(), rawDiff)
	if truncated {
		userPrompt = "NOTE: this diff was truncated to the first " +
			fmt.Sprintf("%d KB", maxDiffBytes/1024) +
			" because it exceeded the inline review budget. Reflect this partial coverage in your rationale.\n\n" +
			userPrompt
	}
	// v0.10.16 — ask the LLM for a structured JSON verdict, then render
	// the markdown ourselves. Ollama-compat `response_format: json_object`
	// forces valid JSON regardless of how rambly the underlying model
	// (looking at you, qwen3-coder-next) tends to be. Falls back to the
	// free-text prompt + salvager if the JSON layer errors.
	body, err := reviewViaJSON(ctx, lm, userPrompt)
	if err != nil {
		rlog.Warn("review: JSON mode failed, falling back to prose", "err", err)
		body, err = reviewViaProse(ctx, lm, userPrompt)
		if err != nil {
			return fmt.Errorf("llm chat: %w", err)
		}
	}

	return postAndLog(ctx, client, cfg.PRNumber, body)
}

// reviewVerdict is the shape the LLM MUST return under JSON mode. Keys
// are lowercase-snake because rambly models mangle CamelCase into
// half-typed alternatives more often than they mangle snake_case.
type reviewVerdict struct {
	CoreChanges []string `json:"core_changes"`
	Verdict     string   `json:"verdict"`
	Rationale   string   `json:"rationale"`
}

const reviewJSONSystemPrompt = `You are a senior code reviewer commenting on a GitHub pull request.

Respond with ONLY a JSON object matching this exact schema — no prose, no code fences, no THOUGHT, no plan-of-action:

{
  "core_changes": ["3 to 8 short imperative-voice bullet points naming meaningful changes"],
  "verdict": "Approve" | "Request changes" | "Comment",
  "rationale": "One short paragraph explaining why you chose this verdict"
}

Rules:
- core_changes: 3-8 items, each a single sentence, no numbering, no leading dashes.
- Group by subsystem when it clarifies — do not enumerate every file.
- verdict must be one of the three literal strings above (spelled exactly).
- Trivial diff (typo, whitespace, comment) → Approve.
- Correctness risk, security issue, breaking change → Request changes; name the risk in the rationale.
- Otherwise → Comment.
- If the diff is truncated, note the partial coverage in the rationale.

Return ONLY the JSON object. Nothing before, nothing after.`

func reviewViaJSON(ctx context.Context, lm *llm.Client, userPrompt string) (string, error) {
	raw, err := lm.ChatJSON(ctx, reviewJSONSystemPrompt, userPrompt)
	if err != nil {
		return "", err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty JSON response")
	}
	// Some models still wrap JSON in ```json ... ``` even under
	// json_object mode. Strip a full-message fence just in case.
	raw = stripFullMessageCodeFence(raw)
	rlog.Info("review: JSON raw response", "bytes", len(raw), "head", firstN(raw, 400))
	var v reviewVerdict
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return "", fmt.Errorf("parse JSON verdict: %w — raw=%.200q", err, raw)
	}
	// The model sometimes returns valid-but-empty JSON when it can't
	// figure out the diff (or ignores the schema and returns `{}`).
	// Treat that as failure so the caller falls back to prose mode,
	// which at least routes through the salvager.
	if len(v.CoreChanges) == 0 && strings.TrimSpace(v.Rationale) == "" && strings.TrimSpace(v.Verdict) == "" {
		return "", fmt.Errorf("JSON verdict was empty (all fields blank): %.200q", raw)
	}
	return renderVerdictMarkdown(v), nil
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// renderVerdictMarkdown converts a structured verdict into the standard
// `## Core Changes` + `## Verdict` markdown shape. Normalises the
// verdict tag to one of the three known values; anything else is
// treated as a Comment.
func renderVerdictMarkdown(v reviewVerdict) string {
	tag := normaliseVerdictTag(v.Verdict)
	var b strings.Builder
	b.WriteString("## Core Changes\n\n")
	if len(v.CoreChanges) == 0 {
		b.WriteString("- (no meaningful changes surfaced)\n")
	}
	for _, c := range v.CoreChanges {
		c = strings.TrimSpace(strings.TrimLeft(c, "-* \t"))
		if c == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(c)
		b.WriteByte('\n')
	}
	b.WriteString("\n---\n\n## Verdict\n\n**")
	b.WriteString(tag)
	b.WriteString("**")
	rationale := strings.TrimSpace(v.Rationale)
	if rationale != "" {
		b.WriteString(": ")
		b.WriteString(rationale)
	}
	return b.String()
}

func normaliseVerdictTag(s string) string {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "approve"):
		return "Approve"
	case strings.Contains(low, "request") || strings.Contains(low, "changes needed") || strings.Contains(low, "block"):
		return "Request changes"
	default:
		return "Comment"
	}
}

// reviewViaProse is the pre-v0.10.16 free-text path — reused when JSON
// mode errors (endpoint doesn't support response_format, transport
// failure, etc.). Runs the LLM through the original strict prompt +
// salvager pipeline.
func reviewViaProse(ctx context.Context, lm *llm.Client, userPrompt string) (string, error) {
	body, err := lm.Chat(ctx, reviewSystemPrompt, userPrompt)
	if err != nil {
		return "", err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("llm returned empty verdict")
	}
	body = stripFullMessageCodeFence(body)
	body = enforceReviewFormat(body)
	return body, nil
}

// enforceReviewFormat salvages the well-formed portion of an LLM
// response that ignored the "start with ## Core Changes, end with the
// Verdict line" contract:
//   - If the response contains a `## Core Changes` header, everything
//     before it is discarded (drops THOUGHT: / plan-of-action preamble).
//   - After that, everything from the Verdict's Approve/Request/Comment
//     line onwards is kept up to the next h2 (drops trailing "next
//     steps" lists the model sometimes tacks on).
//   - If no `## Core Changes` anchor is present at all, wrap the whole
//     response as a fallback Comment verdict so the reader still gets
//     the model's take, just labelled.
func enforceReviewFormat(s string) string {
	s = strings.TrimSpace(s)
	coreIdx := strings.Index(s, "## Core Changes")
	if coreIdx < 0 {
		// No Core Changes anchor at all — treat the whole response as
		// commentary and wrap it.
		return "## Verdict\n\n**Comment**: " + s
	}
	s = s[coreIdx:]
	// Trim trailing action-plan content after the Verdict paragraph.
	// The Verdict line starts with **Approve** / **Request changes** /
	// **Comment**; keep from the first Verdict header onward, but cut
	// at the next h2/h3 that looks like a section the model invented
	// (e.g. "## Final Workflow Step").
	if v := strings.Index(s, "## Verdict"); v >= 0 {
		tail := s[v:]
		// Find the next top-level heading after the Verdict header
		// itself. Skip past the "## Verdict" line first.
		if nl := strings.IndexByte(tail, '\n'); nl > 0 {
			rest := tail[nl+1:]
			if nextH := findNextH2(rest); nextH >= 0 {
				tail = tail[:nl+1+nextH]
			}
		}
		s = s[:v] + strings.TrimRight(tail, "\n")
	}
	return strings.TrimSpace(s)
}

// findNextH2 returns the offset of the next `## ` header in s (start of
// line), or -1 if none.
func findNextH2(s string) int {
	i := 0
	for i < len(s) {
		if strings.HasPrefix(s[i:], "## ") {
			return i
		}
		if nl := strings.IndexByte(s[i:], '\n'); nl >= 0 {
			i += nl + 1
			continue
		}
		return -1
	}
	return -1
}

func buildReviewUserPrompt(prTitle, rawDiff string) string {
	var b strings.Builder
	if prTitle != "" {
		b.WriteString("PR title: ")
		b.WriteString(prTitle)
		b.WriteString("\n\n")
	}
	b.WriteString("Unified diff:\n\n")
	b.WriteString(rawDiff)
	return b.String()
}

// stripFullMessageCodeFence removes a fence that wraps the ENTIRE
// verdict body. In-body fences (e.g. around a code snippet inside the
// verdict) are left alone.
func stripFullMessageCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// drop the opening ```lang line
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	// drop a trailing ``` on its own line
	s = strings.TrimRight(s, "\n")
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimRight(s, "\n")
	}
	return s
}

func postAndLog(ctx context.Context, client *gh.Client, pr int, body string) error {
	url, err := client.PostIssueComment(ctx, pr, body)
	if err != nil {
		return err
	}
	rlog.Info("review: posted verdict", "url", url)
	writeStepSummary(fmt.Sprintf("Posted @quail verdict on PR #%d — %s\n", pr, url))
	// Also log the body for the CI runner to eyeball.
	if os.Getenv("QUAIL_LOG_REVIEW_BODY") == "1" {
		rlog.Info("review body", "body", body)
	}
	return nil
}
