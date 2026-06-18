package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/mindmap"
)

// v0.91: auto mode used to return the static map as soon as
// len(m.Pages) > 0, even if the journey heuristics found zero
// candidates. On JS-heavy SPAs the static fetch returns thin
// shells; auto would lock to static + emit only the
// discover-fallback. The fix: if journeys=0, retry through the
// browser cascade.
func TestCrawlOriginWithFallback_AutoEscalatesOnZeroJourneys(t *testing.T) {
	// Static fetcher returns an HTML page that has no journey signals
	// (no nav links, no forms, no headings) — exactly the shape that
	// triggered the regression.
	thinPage := []byte("<html><body>Loading...</body></html>")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write(thinPage)
	}))
	t.Cleanup(srv.Close)

	prev := browserCrawler
	t.Cleanup(func() { browserCrawler = prev })
	browserCallCount := 0
	browserCrawler = func(ctx context.Context, u string, engine EngineMode, stealth bool, opts mindmap.Options) (*mindmap.Map, []error) {
		browserCallCount++
		// Browser returns a richer map — landing with several links to
		// other pages.
		landing := &mindmap.Page{
			URL:      u,
			Tags:     []string{mindmap.TagLanding},
			Contents: []ast.ContentAnchor{{Tag: "h1", Text: "Home"}},
			Title:    "Home",
		}
		sub := &mindmap.Page{URL: u + "about", Title: "About"}
		return &mindmap.Map{
			Origin: u,
			Pages:  map[string]*mindmap.Page{u: landing, u + "about": sub},
			Order:  []string{u, u + "about"},
		}, nil
	}

	fetch := func(ctx context.Context, u string) ([]byte, string, error) {
		return thinPage, "text/html", nil
	}

	m, _ := crawlOriginWithFallback(context.Background(), strings.TrimSuffix(srv.URL, "/")+"/", fetch, mindmap.Options{MaxPages: 5}, BrowserAuto)
	if browserCallCount == 0 {
		t.Fatalf("expected browser cascade to fire on zero-journeys static; browserCrawler not called")
	}
	if m == nil || len(m.Pages) == 0 {
		t.Fatalf("expected browser map to win; got %+v", m)
	}
}

