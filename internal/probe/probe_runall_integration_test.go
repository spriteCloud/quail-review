package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/mindmap"
	"github.com/spriteCloud/quail-review/internal/plan"
)

// TestRunAll_EmitsStepsCatalogueAndSummary verifies the v0.19 companion
// items reach the output: tests/e2e/lib/steps.ts, the catalogue, and the
// summary deck. Also asserts that the catalogue carries the journeys
// emitted by the rest of the run.
func TestRunAll_EmitsStepsCatalogueAndSummary(t *testing.T) {
	t.Setenv("QUAIL_PROBE_ALLOW_LOOPBACK", "1")
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<html><head><title>Home</title></head><body><h1>Home</h1><a href="/contact">Contact</a></body></html>`))
	})
	mux.HandleFunc("/contact", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head><title>Contact</title></head><body><h1>Contact</h1><form><input name="email" type="email" required><button type="submit">Send</button></form></body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	items, _ := RunAll(context.Background(), []string{srv.URL + "/"})

	got := map[string]plan.Item{}
	for _, it := range items {
		got[it.OutPath] = it
	}

	for _, want := range []string{
		"tests/e2e/lib/steps.ts",
		"tests/e2e/docs/test-catalogue.md",
		"tests/e2e/docs/summary.html",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("expected companion file %q to be emitted; got files %v", want, keysOf(got))
		}
	}
	cat := got["tests/e2e/docs/test-catalogue.md"].Catalogue
	if cat == nil {
		t.Fatalf("catalogue item must carry a non-nil Catalogue")
	}
	if len(cat.Pages) == 0 {
		t.Errorf("catalogue must list crawled pages; got 0")
	}
	if len(cat.Journeys) == 0 {
		t.Errorf("catalogue must list identified journeys; got 0")
	}
	for _, j := range cat.Journeys {
		if j.Kind == "" || j.Priority == "" || j.OutPath == "" {
			t.Errorf("catalogue journey incomplete: %+v", j)
		}
		if !strings.HasPrefix(j.OutPath, "tests/e2e/") {
			t.Errorf("catalogue journey OutPath should sit under tests/e2e: %s", j.OutPath)
		}
	}
}

// TestDomSnapshotItems_OnlyEmitsWhenDOMHTMLSet asserts the DOM-snapshot
// emission is gated on Page.DOMHTML being non-empty (browser-mode signal).
func TestDomSnapshotItems_OnlyEmitsWhenDOMHTMLSet(t *testing.T) {
	m := &mindmap.Map{
		Origin: "https://x.test",
		Order:  []string{"https://x.test/", "https://x.test/about"},
		Pages: map[string]*mindmap.Page{
			"https://x.test/":      {URL: "https://x.test/"},                              // static-crawl page, no DOMHTML
			"https://x.test/about": {URL: "https://x.test/about", DOMHTML: "<html></html>"}, // browser-mode
		},
	}
	items := domSnapshotItems("https://x.test", m, "")
	if len(items) != 1 {
		t.Fatalf("expected 1 DOM-snapshot item (only the page with DOMHTML); got %d", len(items))
	}
	it := items[0]
	if it.Template != plan.TmplRaw {
		t.Errorf("DOM snapshot must use TmplRaw; got %s", it.Template)
	}
	if !strings.HasPrefix(it.OutPath, "tests/e2e/_dom/") {
		t.Errorf("DOM snapshot must land under tests/e2e/_dom/; got %s", it.OutPath)
	}
	if !strings.HasSuffix(it.OutPath, "/about.html") {
		t.Errorf("DOM snapshot stem should include the path slug; got %s", it.OutPath)
	}
	if string(it.RawContent) != "<html></html>" {
		t.Errorf("RawContent mismatch: %q", string(it.RawContent))
	}
}

func keysOf(m map[string]plan.Item) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
