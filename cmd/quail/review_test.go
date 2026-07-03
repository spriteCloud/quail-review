package main

import (
	"strings"
	"testing"
)

func TestStripFullMessageCodeFence_UnwrapsMarkdownFence(t *testing.T) {
	in := "```markdown\n## Verdict\n\n**Approve**: LGTM.\n```"
	got := stripFullMessageCodeFence(in)
	if strings.HasPrefix(got, "```") || strings.HasSuffix(got, "```") {
		t.Errorf("fence not stripped: %q", got)
	}
	if !strings.Contains(got, "## Verdict") {
		t.Errorf("body lost: %q", got)
	}
}

func TestStripFullMessageCodeFence_NoFence_Untouched(t *testing.T) {
	in := "## Verdict\n\n**Approve**: fine."
	if got := stripFullMessageCodeFence(in); got != in {
		t.Errorf("input mutated: %q", got)
	}
}

func TestStripFullMessageCodeFence_InlineFenceIntact(t *testing.T) {
	// A fenced code block INSIDE the verdict must survive — we only
	// strip a fence wrapping the ENTIRE message.
	in := "## Core Changes\n\n- Adds `foo`\n\n```go\nfoo := 1\n```\n\n## Verdict\n\n**Comment**: nice."
	got := stripFullMessageCodeFence(in)
	if !strings.Contains(got, "```go") {
		t.Errorf("inline fence stripped: %q", got)
	}
}

func TestBuildReviewUserPrompt_IncludesTitleAndDiff(t *testing.T) {
	got := buildReviewUserPrompt("feat: add widget", "diff --git a/x b/x\n+hi\n")
	for _, want := range []string{"PR title: feat: add widget", "Unified diff:", "+hi"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in prompt:\n%s", want, got)
		}
	}
}

// The model routinely opens with "THOUGHT:" chain-of-thought or a
// "Let me re-run…" plan-of-action. enforceReviewFormat must trim
// everything before `## Core Changes` so the reader gets a clean
// review-shaped comment.
func TestEnforceReviewFormat_StripsThoughtPreamble(t *testing.T) {
	in := "THOUGHT: I see the diff adds X.\nLet me plan…\n\n## Core Changes\n\n- Adds X\n\n## Verdict\n\n**Approve**"
	got := enforceReviewFormat(in)
	if strings.HasPrefix(got, "THOUGHT") {
		t.Fatalf("preamble not stripped: %q", got)
	}
	if !strings.HasPrefix(got, "## Core Changes") {
		t.Errorf("must start with `## Core Changes`; got %q", got[:min(40, len(got))])
	}
}

// After the Verdict paragraph, the model sometimes tacks on invented
// section headers ("## Final Workflow Step", numbered TODOs, "Let's
// do it"). enforceReviewFormat must cut everything from the next h2
// onwards so the comment ends on the Verdict.
func TestEnforceReviewFormat_CutsActionPlanAfterVerdict(t *testing.T) {
	in := "## Core Changes\n\n- Adds X\n\n## Verdict\n\n**Approve**: LGTM.\n\n## Final Workflow Step\n\n1. Commit\n2. Push"
	got := enforceReviewFormat(in)
	if strings.Contains(got, "Final Workflow Step") || strings.Contains(got, "Commit\n2.") {
		t.Errorf("action plan not stripped:\n%s", got)
	}
	if !strings.Contains(got, "## Verdict") || !strings.Contains(got, "LGTM") {
		t.Errorf("verdict content lost:\n%s", got)
	}
}

// When the model produces ONLY commentary with no `## Core Changes`
// anchor, wrap the whole thing as a Comment verdict rather than
// posting the raw text.
func TestEnforceReviewFormat_WrapsMalformedResponseAsComment(t *testing.T) {
	in := "The diff looks fine overall, adds a widget. Some minor concerns about the timeout."
	got := enforceReviewFormat(in)
	if !strings.HasPrefix(got, "## Verdict") {
		t.Errorf("malformed response should be wrapped; got %q", got)
	}
	if !strings.Contains(got, "**Comment**:") {
		t.Errorf("wrap should tag as Comment; got %q", got)
	}
	if !strings.Contains(got, "widget") {
		t.Errorf("original body should be preserved: %q", got)
	}
}

// A properly-formatted response must pass through unchanged.
func TestEnforceReviewFormat_WellFormedPassesThrough(t *testing.T) {
	in := "## Core Changes\n\n- Bumps foo v1 → v2\n- Refactors bar\n\n## Verdict\n\n**Approve**: safe upgrade."
	got := enforceReviewFormat(in)
	if got != strings.TrimSpace(in) {
		t.Errorf("well-formed response mutated:\ngot:  %q\nwant: %q", got, in)
	}
}
