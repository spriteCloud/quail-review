package plan

import "testing"

func TestExtractHTMLInteractions(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		wantKind string // first interaction expected
		wantText string // optional; empty skips
	}{
		{
			name:     "search by type",
			html:     `<form><input type="search" name="q"></form>`,
			wantKind: "search",
		},
		{
			name:     "search by role",
			html:     `<input role="searchbox" name="q">`,
			wantKind: "search",
		},
		{
			name:     "search by aria label",
			html:     `<input aria-label="Search site" name="q">`,
			wantKind: "search",
		},
		{
			name:     "dialog",
			html:     `<dialog id="d1"><p>Hi</p></dialog>`,
			wantKind: "dialog",
		},
		{
			name:     "details summary",
			html:     `<details><summary>FAQ Question</summary><p>answer</p></details>`,
			wantKind: "details",
			wantText: "FAQ Question",
		},
		{
			name:     "aria-expanded collapse pair",
			html:     `<button aria-expanded="false" aria-controls="panel1">More</button><div id="panel1">x</div>`,
			wantKind: "collapse",
		},
		{
			name:     "date input",
			html:     `<input type="date" name="when">`,
			wantKind: "date",
		},
		{
			name:     "time input",
			html:     `<input type="time" name="when">`,
			wantKind: "date",
		},
		{
			name:     "bootstrap data-toggle",
			html:     `<button data-toggle="collapse" data-target="#x">Toggle</button>`,
			wantKind: "data-toggle",
		},
		{
			name:     "bootstrap 5 data-bs-toggle",
			html:     `<button data-bs-toggle="modal" data-bs-target="#m">Open</button>`,
			wantKind: "data-toggle",
		},
		{
			name:     "popup button via aria-haspopup",
			html:     `<button aria-haspopup="menu">Menu</button>`,
			wantKind: "popup",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := ExtractHTMLInteractions("x.html", []byte(tc.html))
			if len(out) == 0 {
				t.Fatalf("expected at least one interaction for %q; got none", tc.name)
			}
			if out[0].Kind != tc.wantKind {
				t.Errorf("first interaction kind = %q; want %q", out[0].Kind, tc.wantKind)
			}
			if tc.wantText != "" && out[0].Text != tc.wantText {
				t.Errorf("first interaction text = %q; want %q", out[0].Text, tc.wantText)
			}
		})
	}
}

func TestExtractHTMLInteractions_Negative(t *testing.T) {
	// A plain text input is NOT a searchbox.
	html := []byte(`<input type="text" name="username">`)
	out := ExtractHTMLInteractions("x.html", html)
	for _, i := range out {
		if i.Kind == "search" {
			t.Errorf("plain text input must not be classified as search; got %+v", i)
		}
	}
}

func TestExtractHTMLInteractions_CapsApply(t *testing.T) {
	// 10 details elements but cap should hold us to 2.
	var html string
	for i := 0; i < 10; i++ {
		html += `<details><summary>Q</summary><p>A</p></details>`
	}
	out := ExtractHTMLInteractions("x.html", []byte(html))
	count := 0
	for _, i := range out {
		if i.Kind == "details" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 details after cap; got %d", count)
	}
}

func TestExtractHTMLInteractions_BootstrapAllowedToggleOnly(t *testing.T) {
	html := []byte(`<button data-toggle="tooltip">Hint</button>`)
	out := ExtractHTMLInteractions("x.html", html)
	for _, i := range out {
		if i.Kind == "data-toggle" {
			t.Errorf("data-toggle=tooltip must not be emitted (not in allow list); got %+v", i)
		}
	}
}

func TestExtractHTMLInteractions_PopupNotDoubleCounted(t *testing.T) {
	// aria-haspopup + aria-expanded + aria-controls = caught by collapse, not popup.
	html := []byte(`<button aria-haspopup="menu" aria-expanded="false" aria-controls="m1">Menu</button>`)
	out := ExtractHTMLInteractions("x.html", html)
	var sawCollapse, sawPopup bool
	for _, i := range out {
		switch i.Kind {
		case "collapse":
			sawCollapse = true
		case "popup":
			sawPopup = true
		}
	}
	if !sawCollapse {
		t.Error("expected the aria-expanded+aria-controls combo to be classified as collapse")
	}
	if sawPopup {
		t.Error("did not expect a duplicate popup classification for the same trigger")
	}
}
