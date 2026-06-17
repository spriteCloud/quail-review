package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/mindmap"
	"github.com/reviewqa/reviewqa/internal/plan"
)

func TestApiSpecItems_EmitsOnePerFormSameOrigin(t *testing.T) {
	m := &mindmap.Map{
		Origin: "https://x.test",
		Order:  []string{"https://x.test/", "https://x.test/contact", "https://x.test/external"},
		Pages: map[string]*mindmap.Page{
			"https://x.test/": {
				URL: "https://x.test/",
				// no forms
			},
			"https://x.test/contact": {
				URL: "https://x.test/contact",
				Forms: []ast.FormSpec{{
					Action: "/api/contact",
					Method: "post",
					Inputs: []ast.FormInput{{Name: "email", Type: "email", Required: true}},
				}},
			},
			"https://x.test/external": {
				URL: "https://x.test/external",
				// cross-origin action should be dropped (SSRF posture).
				Forms: []ast.FormSpec{{
					Action: "https://attacker.test/api/sink",
					Method: "post",
					Inputs: []ast.FormInput{{Name: "email", Type: "email"}},
				}},
			},
		},
	}
	items := apiSpecItems("https://x.test", m)
	if len(items) != 1 {
		t.Fatalf("expected 1 same-origin API spec; got %d (%+v)", len(items), items)
	}
	it := items[0]
	if it.Template != plan.TmplPlaywrightAPI {
		t.Errorf("template mismatch: %s", it.Template)
	}
	if !strings.HasPrefix(it.OutPath, "tests/e2e/api/") || !strings.HasSuffix(it.OutPath, ".api.spec.ts") {
		t.Errorf("API outpath should sit under tests/e2e/api/*.api.spec.ts; got %s", it.OutPath)
	}
	if it.PageURL != "https://x.test/api/contact" {
		t.Errorf("PageURL should be the resolved endpoint; got %s", it.PageURL)
	}
	if it.Form == nil || len(it.Form.Inputs) == 0 {
		t.Errorf("Item.Form must be populated with form inputs")
	}
}

func TestResolveFormAction_HandlesEmptyAndAbsolute(t *testing.T) {
	cases := []struct {
		page, action, want string
	}{
		{"https://x.test/contact", "", "https://x.test/contact"},
		{"https://x.test/contact", "/api/v1", "https://x.test/api/v1"},
		{"https://x.test/", "https://other.test/sink", "https://other.test/sink"},
		{"https://x.test/", "mailto:hi@x.test", ""},
		{"https://x.test/", "javascript:void(0)", ""},
	}
	for _, c := range cases {
		got := resolveFormAction(c.page, c.action)
		if got != c.want {
			t.Errorf("resolveFormAction(%q, %q) = %q; want %q", c.page, c.action, got, c.want)
		}
	}
}

func TestRunAll_EmitsApiSpecForForm(t *testing.T) {
	t.Setenv("REVIEWQA_PROBE_ALLOW_LOOPBACK", "1")
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<html><body><h1>Home</h1><a href="/contact">Contact</a></body></html>`))
	})
	mux.HandleFunc("/contact", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><h1>Contact</h1>
<form action="/api/contact" method="post">
<input name="email" type="email" required>
<button type="submit">Send</button>
</form></body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	items, _ := RunAll(context.Background(), []string{srv.URL + "/"})
	gotAPI := false
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightAPI {
			gotAPI = true
			if !strings.Contains(it.PageURL, "/api/contact") {
				t.Errorf("API item PageURL should resolve to /api/contact; got %s", it.PageURL)
			}
		}
	}
	if !gotAPI {
		t.Errorf("expected at least one API spec item; got files: %v", outPaths(items))
	}
}

func outPaths(items []plan.Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.OutPath)
	}
	return out
}
