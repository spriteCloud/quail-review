package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/reviewqa/reviewqa/internal/plan"
)

func TestRunAll_EmitsQualityCompanions(t *testing.T) {
	t.Setenv("REVIEWQA_PROBE_ALLOW_LOOPBACK", "1")
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
			if strings.Contains(it.OutPath, "/"+k+"/") && strings.HasSuffix(it.OutPath, "."+k+".spec.ts") {
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

func TestRunAll_OmitsI18nWhenNoHreflang(t *testing.T) {
	t.Setenv("REVIEWQA_PROBE_ALLOW_LOOPBACK", "1")
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
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightI18n {
			t.Errorf("i18n spec emitted but no hreflang siblings present: %s", it.OutPath)
		}
	}
}
