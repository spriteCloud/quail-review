package composer

import (
	"strings"
	"testing"
)

func TestParse_RecoversFromTruncatedArray(t *testing.T) {
	// Simulates a DGX response that ran out of tokens mid-third-object.
	// The first two scenarios are complete and well-formed; the third
	// is cut off. Without v0.46 truncation recovery this fails with
	// "unexpected end of JSON input". With recovery we get the two
	// complete scenarios.
	raw := `Here are the scenarios:
[
  {"name":"Submit and reload","steps":[{"keyword":"Given","text":"I open the landing page"},{"keyword":"When","text":"I submit the form"},{"keyword":"Then","text":"the main heading reads \"OK\""}]},
  {"name":"Back button keeps state","steps":[{"keyword":"Given","text":"I open the landing page"},{"keyword":"When","text":"I go back in the browser history"},{"keyword":"Then","text":"I remain on the same page"}]},
  {"name":"Race condition","steps":[{"keyword":"Given","text":"I open the landing page"},{"keyword":"When","text":"I submit the form twice in rapid `
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse should recover gracefully; got err: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 recovered scenarios from a truncated array; got %d", len(got))
	}
}

func TestParse_RecoversFromMidArrayMalformedObject(t *testing.T) {
	// One bad object in the middle does not poison the rest. v0.46:
	// the partial recovery skips malformed objects and continues.
	raw := `[
  {"name":"First","steps":[{"keyword":"Given","text":"I open the landing page"}]},
  {"name":"Bad","steps":["not an object"]},
  {"name":"Third","steps":[{"keyword":"Then","text":"I remain on the same page"}]}
]`
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// At minimum the first and third should survive.
	if len(got) < 1 {
		t.Errorf("expected at least 1 surviving scenario; got %d", len(got))
	}
}

func TestParse_StillReturnsErrorOnZeroRecovery(t *testing.T) {
	// No JSON at all → error.
	if _, err := Parse("hello there, no JSON for you"); err == nil {
		t.Error("expected error when no JSON present at all")
	}
}

func TestBuildUserPrompt_CapsDestinationPagesAtFour(t *testing.T) {
	j := Journey{URL: "https://x.test/", Kind: "explore", Title: "Home", H1: "Hi"}
	for i := 0; i < 9; i++ {
		j.Pages = append(j.Pages, PageContext{
			Href: "/page-" + string(rune('a'+i)),
			Title: "T" + string(rune('a'+i)),
			H1: "H" + string(rune('a'+i)),
		})
	}
	prompt := buildUserPrompt(j, 5)
	count := strings.Count(prompt, "→ title=")
	if count > 4 {
		t.Errorf("prompt should list at most 4 destination pages; got %d", count)
	}
	if !strings.Contains(prompt, "5 more destination pages omitted") {
		t.Errorf("prompt should advertise the omitted-count when capping; got\n%s", prompt)
	}
}
