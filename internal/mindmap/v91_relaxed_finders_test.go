package mindmap

import (
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
)

// v0.91: isFormPage's hasSubmit check accepts role-tagged submits.
// A <div role="button"> inside a form-with-required-input should
// trigger TagForm now, where pre-v0.91 it silently no-op'd.
func TestIsFormPage_AcceptsRoleButton(t *testing.T) {
	p := &Page{
		URL:     "https://example.com/contact",
		HasForm: true,
		Inputs:  []ast.FormInput{{Name: "email", Required: true}},
		Anchors: []ast.LocatorAnchor{{Role: "button", Tag: "submit", Name: "Send"}},
	}
	if !isFormPage(p) {
		t.Errorf("isFormPage should accept role=button as submit signal")
	}
}

func TestIsFormPage_RoleSubmitAlsoCounts(t *testing.T) {
	p := &Page{
		URL:     "https://example.com/contact",
		HasForm: true,
		Inputs:  []ast.FormInput{{Name: "email", Required: true}},
		Anchors: []ast.LocatorAnchor{{Role: "submit", Tag: "div"}},
	}
	if !isFormPage(p) {
		t.Errorf("isFormPage should accept role=submit even when Tag=div")
	}
}

func TestIsFormPage_StillNeedsForm(t *testing.T) {
	p := &Page{
		URL:     "https://example.com/page",
		HasForm: false,
		Inputs:  []ast.FormInput{{Name: "email", Required: true}},
		Anchors: []ast.LocatorAnchor{{Role: "button", Tag: "submit"}},
	}
	if isFormPage(p) {
		t.Errorf("isFormPage must still require HasForm; got true with HasForm=false")
	}
}

// v0.91: findReadJourneys used to require TagDetail. JS-heavy SPAs
// don't trigger TagDetail because heavy nav inflates the link count
// above the <20 gate. Relaxation: title + at least one heading
// (h1/h2 in Contents) is enough to emit a Read journey.
func TestFindReadJourneys_AcceptsTitlePlusHeadingShape(t *testing.T) {
	landingURL := "https://example.com/"
	subURL := "https://example.com/article"
	m := &Map{
		Origin: landingURL,
		Order:  []string{landingURL, subURL},
		Pages: map[string]*Page{
			landingURL: {URL: landingURL, Tags: []string{TagLanding}},
			subURL: {
				URL:   subURL,
				Title: "An interesting article",
				Contents: []ast.ContentAnchor{
					{Tag: "title", Text: "An interesting article"},
					{Tag: "h1", Text: "Hello"},
				},
				// no TagDetail — the relaxation path is what catches this
			},
		},
	}
	out := findReadJourneys(m, 5)
	if len(out) != 1 {
		t.Fatalf("expected 1 read journey from title+heading shape; got %d: %+v", len(out), out)
	}
	if out[0].Kind != JourneyRead {
		t.Errorf("kind = %q, want read", out[0].Kind)
	}
}

// Pages with no title + no heading must NOT become read journeys
// (avoid the "every thin shell is a read journey" failure mode).
func TestFindReadJourneys_RejectsThinShells(t *testing.T) {
	landingURL := "https://example.com/"
	subURL := "https://example.com/empty"
	m := &Map{
		Origin: landingURL,
		Order:  []string{landingURL, subURL},
		Pages: map[string]*Page{
			landingURL: {URL: landingURL, Tags: []string{TagLanding}},
			subURL:     {URL: subURL}, // no title, no contents, no detail tag
		},
	}
	out := findReadJourneys(m, 5)
	if len(out) != 0 {
		t.Errorf("expected no read journeys for thin shell; got %d", len(out))
	}
}
