package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/plan"
)

func v043TestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<html><head><title>Home</title></head><body><h1>Home</h1><a href="/about">About</a></body></html>`))
	})
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head><title>About</title></head><body><h1>About</h1></body></html>`))
	})
	return httptest.NewServer(mux)
}

func TestRunAll_EmitsMobileUnconditionally(t *testing.T) {
	t.Setenv("QUAIL_PROBE_ALLOW_LOOPBACK", "1")
	srv := v043TestServer(t)
	defer srv.Close()

	items, _ := RunAll(context.Background(), []string{srv.URL + "/"})
	found := false
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightMobile {
			found = true
			if !strings.Contains(it.OutPath, "/mobile/") {
				t.Errorf("mobile spec emitted to unexpected path: %s", it.OutPath)
			}
		}
	}
	if !found {
		t.Error("v0.43 — Mobile template must emit on every probe; no item found")
	}
}

func TestRunAll_EmitsIntegrationStubPerOrigin(t *testing.T) {
	t.Setenv("QUAIL_PROBE_ALLOW_LOOPBACK", "1")
	srv := v043TestServer(t)
	defer srv.Close()

	items, _ := RunAll(context.Background(), []string{srv.URL + "/"})
	count := 0
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightIntegrationStub {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 integration-stub spec per origin; got %d", count)
	}
}

// v0.57 — four new per-kind integration stubs emit unconditionally
// per origin alongside the catch-all integration_api_stub from v0.43.
func TestRunAll_EmitsAllPerKindIntegrationStubs_v057(t *testing.T) {
	t.Setenv("QUAIL_PROBE_ALLOW_LOOPBACK", "1")
	srv := v043TestServer(t)
	defer srv.Close()

	items, _ := RunAll(context.Background(), []string{srv.URL + "/"})
	counts := map[plan.Template]int{
		plan.TmplPlaywrightIntegrationDBStub:    0,
		plan.TmplPlaywrightIntegrationCacheStub: 0,
		plan.TmplPlaywrightIntegrationObsStub:   0,
		plan.TmplPlaywrightIntegrationAuthStub:  0,
	}
	for _, it := range items {
		if _, tracked := counts[it.Template]; tracked {
			counts[it.Template]++
		}
	}
	for tmpl, got := range counts {
		if got != 1 {
			t.Errorf("expected exactly 1 %s per origin; got %d", tmpl, got)
		}
	}
}
