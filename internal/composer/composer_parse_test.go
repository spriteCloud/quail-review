package composer

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSanitizeDirtyJSON_TrailingCommas(t *testing.T) {
	cases := map[string]string{
		`[{"a":1,},]`:                   `[{"a":1}]`,
		`[{"a":1,"b":2,},{"c":3,}]`:     `[{"a":1,"b":2},{"c":3}]`,
		`[1, 2, 3,]`:                    `[1, 2, 3]`,
		`{"x":[1,2,], "y":{"z":3,}}`:    `{"x":[1,2], "y":{"z":3}}`,
	}
	for dirty, want := range cases {
		if got := sanitizeDirtyJSON(dirty); got != want {
			t.Errorf("sanitize(%q) = %q; want %q", dirty, got, want)
		}
	}
}

func TestSanitizeDirtyJSON_SmartQuotesAndDoubledCommas(t *testing.T) {
	in := `[{“a”: 1,,}]`
	got := sanitizeDirtyJSON(in)
	want := `[{"a": 1}]`
	if got != want {
		t.Errorf("sanitize(%q) = %q; want %q", in, got, want)
	}
}

func TestParse_DirtyJSONFromDGXRun(t *testing.T) {
	// The actual dirty-JSON shape logged from the spritecloud.com DGX
	// run: trailing comma after `}`, before `]`.
	dirty := `[
  {"name":"good","steps":[
    {"keyword":"Given","text":"I open the landing page"},
  ]},
]`
	got, err := Parse(dirty)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 1 || got[0].Name != "good" {
		t.Errorf("unexpected: %+v", got)
	}
}

func TestPropose_RetriesOnParseFailure(t *testing.T) {
	calls := 0
	var systemSeen [2]string
	client := callbackClient(func(system, user string) (string, error) {
		systemSeen[calls] = system
		calls++
		switch calls {
		case 1:
			// First attempt: dirty + unparseable.
			return `nope, not JSON`, nil
		case 2:
			// Retry: valid JSON.
			return `[{"name":"ok","steps":[{"keyword":"Given","text":"I open the landing page"}]}]`, nil
		}
		return "", errors.New("too many calls")
	})
	out, err := Propose(context.Background(), client, Journey{Kind: "convert"}, 3)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 LLM calls (initial + retry); got %d", calls)
	}
	if len(out) != 1 {
		t.Errorf("expected 1 scenario after retry; got %d", len(out))
	}
	if !strings.Contains(systemSeen[1], "IMPORTANT: Your previous response") {
		t.Errorf("retry should use the stricter system prompt")
	}
}

func TestPropose_GivesUpAfterTwoFailures(t *testing.T) {
	calls := 0
	client := callbackClient(func(string, string) (string, error) {
		calls++
		return `garbage`, nil
	})
	_, err := Propose(context.Background(), client, Journey{}, 3)
	if err == nil {
		t.Error("expected error after retry also fails")
	}
	if calls != 2 {
		t.Errorf("expected exactly 2 calls; got %d", calls)
	}
}

func TestDedup_DropsMatchingStepSequences(t *testing.T) {
	in := []ExtraScenario{
		{Name: "A", Steps: []Step{{Keyword: "Given", Text: "I open the landing page"}}},
		{Name: "B-different-title-same-steps", Steps: []Step{{Keyword: "Given", Text: "I open the landing page"}}},
		{Name: "C-different-steps", Steps: []Step{{Keyword: "Given", Text: "I open the landing page"}, {Keyword: "Then", Text: `the page title contains "X"`}}},
	}
	out := Dedup(in)
	if len(out) != 2 {
		t.Errorf("expected 2 after dedup; got %d (%+v)", len(out), out)
	}
	if out[0].Name != "A" || out[1].Name != "C-different-steps" {
		t.Errorf("unexpected dedup order: %+v", out)
	}
}

func TestBuildUserPrompt_IncludesDestinationPages(t *testing.T) {
	j := Journey{
		URL: "https://x.test/", Kind: "research",
		Pages: []PageContext{
			{Href: "/case-study", Title: "Case Study", H1: "Performance Testing"},
		},
	}
	user := buildUserPrompt(j, 3)
	if !strings.Contains(user, "Destination pages") {
		t.Errorf("user prompt should include destination-pages context: %s", user)
	}
	if !strings.Contains(user, "/case-study") || !strings.Contains(user, "Performance Testing") {
		t.Errorf("user prompt should embed Href + H1: %s", user)
	}
}

// callbackClient wraps a func into the Client interface.
type callbackClient func(system, user string) (string, error)

func (c callbackClient) Chat(_ context.Context, system, user string) (string, error) {
	return c(system, user)
}
