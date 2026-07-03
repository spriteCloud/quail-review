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
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spriteCloud/quail-core/config"
	"github.com/spriteCloud/quail-core/gh"
	"github.com/spriteCloud/quail-core/llm"
	rlog "github.com/spriteCloud/quail-core/log"
)

// maxDiffBytes gates the LLM call. Above this, the review comment is a
// short bailout — the diff is too big for a useful single-shot review.
// Ponytail: raise when a real PR hits it; add chunking if the pattern
// repeats.
const maxDiffBytes = 200 * 1024

const reviewSystemPrompt = `You are a senior code reviewer commenting on a GitHub pull request.

Produce ONLY a markdown comment with exactly two sections, in this order:

## Core Changes

- 3 to 8 bullet points, imperative voice, each naming a meaningful change
- Group by subsystem or feature when it helps clarity
- Do NOT enumerate every file — summarise

---

## Verdict

**Approve** | **Request changes** | **Comment**: <one short paragraph explaining why>

Rules:
- No preamble, no closing sign-off, no code fences around the whole message
- Do not invent context you cannot infer from the diff
- Do not suggest additions the diff does not need
- If the diff is trivial (typo, comment tweak, whitespace), Approve concisely
- If the diff introduces a correctness risk, security issue, or breaking change, Request changes and name it specifically
- Otherwise use Comment for observational feedback that isn't blocking
`

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
	if len(rawDiff) > maxDiffBytes {
		return postAndLog(ctx, client, cfg.PRNumber, fmt.Sprintf(
			"## Verdict\n\n**Comment**: this diff is %d KB — beyond the inline review budget. "+
				"Split into smaller PRs or run a manual review.", len(rawDiff)/1024))
	}

	lm := llm.New(cfg)
	if !lm.Enabled() {
		return fmt.Errorf("review: no LLM configured (set OPENAI_API_KEY / QUAIL_MODEL)")
	}

	userPrompt := buildReviewUserPrompt(prObj.GetTitle(), rawDiff)
	body, err := lm.Chat(ctx, reviewSystemPrompt, userPrompt)
	if err != nil {
		return fmt.Errorf("llm chat: %w", err)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("llm returned empty verdict")
	}
	// Trim a leading ```markdown or ``` fence the model sometimes emits
	// despite the system prompt, plus the matching trailing fence.
	body = stripFullMessageCodeFence(body)

	return postAndLog(ctx, client, cfg.PRNumber, body)
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
