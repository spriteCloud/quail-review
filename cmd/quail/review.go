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
	// v0.10.17 — two-step LLM: one focused call for the bullet list of
	// core changes, one focused call for the verdict + rationale. Each
	// prompt is simpler (small models handle simple schemas better) and
	// we control the section headers ourselves — so `## Core Changes`
	// and `## Verdict` are always right regardless of what the model
	// does with formatting. Falls back to single-shot JSON, then prose
	// + salvager, so a working model swap doesn't lose the reliability.
	body, err := reviewViaTwoStep(ctx, lm, prObj.GetTitle(), rawDiff, truncated)
	if err != nil {
		rlog.Warn("review: two-step mode failed, falling back to single JSON", "err", err)
		body, err = reviewViaJSON(ctx, lm, userPrompt)
		if err != nil {
			rlog.Warn("review: JSON mode failed, falling back to prose", "err", err)
			body, err = reviewViaProse(ctx, lm, userPrompt)
			if err != nil {
				return fmt.Errorf("llm chat: %w", err)
			}
		}
	}

	return postAndLog(ctx, client, cfg.PRNumber, body)
}

// reviewViaTwoStep issues TWO focused LLM calls — one for the bullet
// list of core changes, one for the verdict + rationale — and renders
// the two `##` headers ourselves. Small chat-tuned models (qwen2.5:7b,
// llama3.1:8b) reliably follow narrow JSON schemas but often ignore
// section headers when asked to produce a whole review in one go; this
// split keeps each ask small and puts the presentation contract on
// our side, not theirs.
func reviewViaTwoStep(ctx context.Context, lm *llm.Client, prTitle, rawDiff string, truncated bool) (string, error) {
	bullets, err := extractCoreChanges(ctx, lm, prTitle, rawDiff, truncated)
	if err != nil {
		return "", fmt.Errorf("core-changes step: %w", err)
	}
	if len(bullets) == 0 {
		return "", fmt.Errorf("core-changes step returned no bullets")
	}
	rlog.Info("review: extracted core changes", "count", len(bullets))
	v, err := computeVerdict(ctx, lm, prTitle, rawDiff, bullets, truncated)
	if err != nil {
		return "", fmt.Errorf("verdict step: %w", err)
	}
	rlog.Info("review: computed verdict", "verdict", v.Verdict)
	return renderVerdictMarkdown(reviewVerdict{
		CoreChanges: bullets,
		Verdict:     v.Verdict,
		Rationale:   v.Rationale,
	}), nil
}

const coreChangesSystemPrompt = `You are a senior code reviewer summarising a GitHub pull request.

Respond with a JSON object containing exactly ONE key: "core_changes".

EXAMPLE — a valid response:

{
  "core_changes": [
    "Bumps @playwright/test from 1.44 to 1.47 in package.json",
    "Replaces deprecated page.waitForTimeout with page.waitForLoadState in 4 specs",
    "Adds a retry wrapper around the flaky login step in tests/auth/login.spec.ts"
  ]
}

Rules:
- "core_changes" must be a JSON array of 3 to 8 strings.
- Each string is a single sentence in imperative voice describing ONE meaningful change.
- Do not number the items, do not include a leading dash or bullet, do not repeat the array key inside the strings.
- Group by subsystem when it clarifies. Do NOT enumerate every file — summarise.
- Return the JSON object and NOTHING else. Empty {} is invalid.`

const verdictSystemPrompt = `You are a senior code reviewer deciding an overall verdict for a GitHub pull request.

Respond with a JSON object containing exactly TWO keys: "verdict" and "rationale".

EXAMPLE — a valid response:

{
  "verdict": "Approve",
  "rationale": "The bump is a minor version with no known breaking changes for this suite. The retry wrapper caps at 2 attempts so it can't mask a real regression."
}

Rules:
- "verdict" must be exactly one of: "Approve", "Request changes", "Comment".
  - Trivial diff (typo, comment, whitespace) → "Approve".
  - Correctness risk, security issue, breaking change → "Request changes" and name the risk in the rationale.
  - Otherwise → "Comment".
- "rationale" is a single paragraph explaining the verdict — never empty.
- If the diff was truncated, note the partial coverage in the rationale.
- Return the JSON object and NOTHING else. Empty {} is invalid.`

type coreChangesShape struct {
	CoreChanges []string `json:"core_changes"`
}

type verdictShape struct {
	Verdict   string `json:"verdict"`
	Rationale string `json:"rationale"`
}

func extractCoreChanges(ctx context.Context, lm *llm.Client, prTitle, rawDiff string, truncated bool) ([]string, error) {
	prompt := buildReviewUserPrompt(prTitle, rawDiff)
	if truncated {
		prompt = "NOTE: this diff was truncated to " +
			fmt.Sprintf("%d KB", maxDiffBytes/1024) +
			" — summarise from what's below.\n\n" + prompt
	}
	// Ollama's `format: json_object` strict mode makes qwen2.5:7b (and
	// similar 7B instruct models) take the escape hatch and return
	// literal `{}`. Plain Chat with a prompt asking for JSON gets
	// non-empty responses. We extract the JSON block via regex.
	raw, err := lm.Chat(ctx, coreChangesSystemPrompt, prompt)
	if err != nil {
		return nil, err
	}
	raw = strings.TrimSpace(stripFullMessageCodeFence(raw))
	rlog.Info("review: core_changes raw", "bytes", len(raw), "head", firstN(raw, 300))
	jsonBlock := extractJSONObject(raw)
	if jsonBlock == "" {
		return nil, fmt.Errorf("no JSON object in response: raw=%.200q", raw)
	}
	var s coreChangesShape
	if err := json.Unmarshal([]byte(jsonBlock), &s); err != nil {
		return nil, fmt.Errorf("parse core_changes: %w — raw=%.200q", err, jsonBlock)
	}
	var out []string
	for _, c := range s.CoreChanges {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out, nil
}

func computeVerdict(ctx context.Context, lm *llm.Client, prTitle, rawDiff string, bullets []string, truncated bool) (verdictShape, error) {
	var b strings.Builder
	if prTitle != "" {
		b.WriteString("PR title: ")
		b.WriteString(prTitle)
		b.WriteString("\n\n")
	}
	b.WriteString("Summary of the changes (from a prior pass):\n")
	for _, c := range bullets {
		b.WriteString("- ")
		b.WriteString(c)
		b.WriteByte('\n')
	}
	if truncated {
		b.WriteString("\nNOTE: the diff was truncated to ")
		b.WriteString(fmt.Sprintf("%d KB", maxDiffBytes/1024))
		b.WriteString(" — note partial coverage in your rationale.\n")
	}
	b.WriteString("\nUnified diff (may be truncated):\n\n")
	b.WriteString(rawDiff)
	raw, err := lm.Chat(ctx, verdictSystemPrompt, b.String())
	if err != nil {
		return verdictShape{}, err
	}
	raw = strings.TrimSpace(stripFullMessageCodeFence(raw))
	rlog.Info("review: verdict raw", "bytes", len(raw), "head", firstN(raw, 300))
	jsonBlock := extractJSONObject(raw)
	if jsonBlock == "" {
		return verdictShape{}, fmt.Errorf("no JSON object in verdict response: raw=%.200q", raw)
	}
	var v verdictShape
	if err := json.Unmarshal([]byte(jsonBlock), &v); err != nil {
		return verdictShape{}, fmt.Errorf("parse verdict: %w — raw=%.200q", err, jsonBlock)
	}
	if strings.TrimSpace(v.Verdict) == "" && strings.TrimSpace(v.Rationale) == "" {
		return verdictShape{}, fmt.Errorf("verdict step returned empty object: %.200q", raw)
	}
	return v, nil
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

Respond with a JSON object using EXACTLY these three keys: "core_changes", "verdict", "rationale". No other keys. No prose outside the JSON. No code fences.

EXAMPLE — a valid response for a hypothetical PR:

{
  "core_changes": [
    "Bumps @playwright/test from 1.44 to 1.47 in package.json",
    "Replaces deprecated page.waitForTimeout with page.waitForLoadState in 4 specs",
    "Adds a retry wrapper around the flaky login step in tests/auth/login.spec.ts"
  ],
  "verdict": "Approve",
  "rationale": "The bump is a minor version with no known breaking changes for this suite, and the deprecation swap is mechanical. The retry wrapper caps at 2 attempts so it can't mask a real regression."
}

Now write YOUR response for the actual PR diff below, in the same JSON shape.

Rules:
- "core_changes" must be a JSON array of 3-8 strings, each a single sentence describing one meaningful change. No numbering. No leading dashes or bullets. Group by subsystem when it clarifies.
- "verdict" must be exactly one of: "Approve", "Request changes", "Comment".
  - Trivial diff (typo, whitespace, comment) → "Approve".
  - Correctness risk, security issue, or breaking change → "Request changes" and name the risk in the rationale.
  - Otherwise → "Comment".
- "rationale" must be a single paragraph string explaining the verdict. Not empty.
- If the diff was truncated, mention partial coverage in the rationale.
- An empty object {} is NOT a valid response. Every field must be populated based on the diff.`

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

// extractJSONObject returns the outermost balanced `{...}` block in s,
// or "" if none. Handles nested braces and quoted strings (including
// escaped quotes). Used to pull a JSON object out of a model's prose
// response when we can't force `response_format:json_object` because
// the model returns `{}` under that constraint.
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if inString {
			switch c {
			case '\\':
				escape = true
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
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
