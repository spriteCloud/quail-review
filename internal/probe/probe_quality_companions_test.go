package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestRunAll_EmitsQualityCompanions(t *testing.T) {
	t.Setenv("QUAIL_PROBE_ALLOW_LOOPBACK", "1")
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<html lang="en"><head>
<link rel="alternate" hreflang="en" href="https://x.test/en">
<link rel="alternate" hreflang="es" href="https://x.test/es">
</head><body><h1>Home</h1><a href="/about">About</a></body></html>`))
	})
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><h1>About</h1></body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	items, _ := RunAll(context.Background(), []string{srv.URL + "/"})

	have := map[string]int{
		"a11y":          0,
		"responsive":    0,
		"perf":          0,
		"security":      0,
		"health":        0,
		"observability": 0,
		"i18n":          0,
	}
	for _, it := range items {
		for k := range have {
			if strings.Contains(it.OutPath, "/"+k+"/") && strings.HasSuffix(it.OutPath, k+".spec.ts") {
				have[k]++
			}
		}
	}
	for k, n := range have {
		if n == 0 {
			t.Errorf("expected ≥1 %s spec; got %d", k, n)
		}
	}
}

// v0.59 — a11y / landmarks / keyboard are decoupled from the per-page
// cap and emit on EVERY crawled page. Validates the Pass-A / Pass-B
// split in qualityCompanions.
func TestRunAll_A11yTrioUncapped_v059(t *testing.T) {
	t.Setenv("QUAIL_PROBE_ALLOW_LOOPBACK", "1")
	// Build a server with 8 distinct pages, all linked from /. The
	// breadth coverage cap is 3, so before v0.59 only 3 pages would
	// get the quality companions; with the cap lifted on the a11y
	// trio we expect 8 of each.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			body := `<html><body><h1>Home</h1>
<a href="/a">A</a><a href="/b">B</a><a href="/c">C</a><a href="/d">D</a>
<a href="/e">E</a><a href="/f">F</a><a href="/g">G</a></body></html>`
			_, _ = w.Write([]byte(body))
			return
		}
		// Each child page has its own h1 so the spider records it as
		// a distinct page.
		_, _ = w.Write([]byte(`<html><body><h1>` + r.URL.Path + `</h1></body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Probe in breadth mode (cap=3) — pre-v0.59 only 3 pages got a11y.
	items, _ := RunAllWithCoverage(context.Background(), []string{srv.URL + "/"}, nil, CoverageBreadth)

	a11yCount := 0
	keyboardCount := 0
	landmarksCount := 0
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightA11y {
			a11yCount++
		}
		if it.Template == plan.TmplPlaywrightKeyboardNav {
			keyboardCount++
		}
		if it.Template == plan.TmplPlaywrightA11yLandmarks {
			landmarksCount++
		}
	}
	// Crawled pages should equal a11y emissions — uncapped.
	// Breadth's MaxPages=8 means up to 8 pages crawled; even at 4
	// pages we must get one a11y spec per page (4 > breadth cap of 3).
	if a11yCount < 4 {
		t.Errorf("a11y trio should be uncapped; got a11y=%d on a >3-page crawl", a11yCount)
	}
	if keyboardCount != a11yCount {
		t.Errorf("keyboard nav should emit on every page same as a11y; got keyboard=%d a11y=%d", keyboardCount, a11yCount)
	}
	if landmarksCount != a11yCount {
		t.Errorf("landmarks should emit on every page same as a11y; got landmarks=%d a11y=%d", landmarksCount, a11yCount)
	}
}

// v0.43 — i18n now always emits with a fallback "html lang present" check
// even on sites with no hreflang siblings. The old assertion (no spec
// emitted) is no longer the desired contract.
func TestRunAll_EmitsI18nFallbackWhenNoHreflang(t *testing.T) {
	t.Setenv("QUAIL_PROBE_ALLOW_LOOPBACK", "1")
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<html><body><h1>Home</h1><a href="/about">About</a></body></html>`))
	})
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><h1>About</h1></body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	items, _ := RunAll(context.Background(), []string{srv.URL + "/"})
	count := 0
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightI18n {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 i18n fallback spec; got %d", count)
	}
}
