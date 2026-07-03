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
