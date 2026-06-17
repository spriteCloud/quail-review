package serve

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

const sampleHTML = `<!doctype html>
<html><head><title>Sign in — Acme</title></head>
<body>
<h1>Welcome back</h1>
<h2>Sign in</h2>
<form action="/login">
  <label for="email">Email</label>
  <input id="email" name="email" type="email" required>
  <label for="password">Password</label>
  <input id="password" name="password" type="password" required>
  <button type="submit">Sign in</button>
</form>
<a href="/signup">Create an account</a>
<a href="/forgot">Forgot password?</a>
<button aria-label="Open menu">☰</button>
</body></html>`

func parseSample(t *testing.T) *DOMLandmarks {
	t.Helper()
	root, err := html.Parse(strings.NewReader(sampleHTML))
	if err != nil {
		t.Fatal(err)
	}
	lm := &DOMLandmarks{URL: "https://example.com/"}
	walkDOM(root, lm, collectLabels(root), nil)
	return lm
}

func TestParseDOM_Title(t *testing.T) {
	lm := parseSample(t)
	if lm.Title != "Sign in — Acme" {
		t.Errorf("title: got %q", lm.Title)
	}
}

func TestParseDOM_Headings(t *testing.T) {
	lm := parseSample(t)
	if len(lm.Headings) != 2 {
		t.Fatalf("expected 2 headings, got %d", len(lm.Headings))
	}
	if lm.Headings[0].Level != 1 || lm.Headings[0].Text != "Welcome back" {
		t.Errorf("h1: got %+v", lm.Headings[0])
	}
}

func TestParseDOM_FormsAndInputs(t *testing.T) {
	lm := parseSample(t)
	if len(lm.Forms) != 1 {
		t.Fatalf("expected 1 form, got %d", len(lm.Forms))
	}
	if len(lm.Forms[0].Inputs) != 2 {
		t.Fatalf("expected 2 inputs in the form, got %d", len(lm.Forms[0].Inputs))
	}
	if lm.Forms[0].Inputs[0].Label != "Email" {
		t.Errorf("label: got %q", lm.Forms[0].Inputs[0].Label)
	}
}

func TestParseDOM_LinksAndButtons(t *testing.T) {
	lm := parseSample(t)
	if len(lm.Links) != 2 {
		t.Errorf("links: got %d", len(lm.Links))
	}
	if len(lm.Buttons) != 2 {
		t.Errorf("buttons: got %d", len(lm.Buttons))
	}
}

func TestRankLocators_ButtonHintMatchesBest(t *testing.T) {
	lm := parseSample(t)
	cands := RankLocators(lm, "button", "Sign in")
	if len(cands) == 0 {
		t.Fatal("no candidates")
	}
	if cands[0].Name != "Sign in" {
		t.Errorf("best button mismatch: %+v", cands[0])
	}
	if !strings.Contains(cands[0].Selector, "getByRole('button'") {
		t.Errorf("expected getByRole selector, got %q", cands[0].Selector)
	}
}

func TestRankLocators_InputByLabel(t *testing.T) {
	lm := parseSample(t)
	cands := RankLocators(lm, "input", "Email")
	if len(cands) == 0 {
		t.Fatal("no candidates")
	}
	if !strings.Contains(cands[0].Selector, "getByLabel('Email')") {
		t.Errorf("expected getByLabel('Email'), got %q", cands[0].Selector)
	}
}

func TestRankLocators_LinkByHref(t *testing.T) {
	lm := parseSample(t)
	cands := RankLocators(lm, "link", "/signup")
	if len(cands) == 0 {
		t.Fatal("no candidates")
	}
	hit := false
	for _, c := range cands {
		if strings.Contains(c.Selector, `a[href="/signup"]`) || c.Name == "Create an account" {
			hit = true
			break
		}
	}
	if !hit {
		t.Errorf("expected /signup-related candidate; got %+v", cands)
	}
}

func TestRankLocators_HeadingMatch(t *testing.T) {
	lm := parseSample(t)
	cands := RankLocators(lm, "heading", "Welcome")
	if len(cands) == 0 {
		t.Fatal("no candidates")
	}
	if !strings.Contains(cands[0].Selector, "getByRole('heading'") {
		t.Errorf("expected getByRole('heading'), got %q", cands[0].Selector)
	}
}

func TestResolveTarget_RelativeAgainstBase(t *testing.T) {
	got, err := resolveTarget("/about", "https://example.com/contact")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.com/about" {
		t.Errorf("resolveTarget: got %q", got)
	}
}

func TestResolveTarget_AbsolutePassesThrough(t *testing.T) {
	got, _ := resolveTarget("https://elsewhere.com/", "https://example.com/")
	if got != "https://elsewhere.com/" {
		t.Errorf("resolveTarget passthrough: got %q", got)
	}
}
