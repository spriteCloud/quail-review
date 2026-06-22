package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestRunAll_AlwaysEmitsGraphQLStub(t *testing.T) {
	t.Setenv("QUAIL_PROBE_ALLOW_LOOPBACK", "1")
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<html><head><title>Home</title></head><body><h1>Home</h1><a href="/about">About</a></body></html>`))
	})
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><h1>About</h1></body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	items, _ := RunAll(context.Background(), []string{srv.URL + "/"})
	count := 0
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightGraphQLStub {
			count++
			if !strings.Contains(it.OutPath, "/graphql/") {
				t.Errorf("graphql-stub spec at unexpected path: %s", it.OutPath)
			}
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 graphql-stub per origin; got %d", count)
	}
}

func TestRunAll_AlwaysEmitsWebhookStub(t *testing.T) {
	t.Setenv("QUAIL_PROBE_ALLOW_LOOPBACK", "1")
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<html><head><title>Home</title></head><body><h1>Home</h1><a href="/about">About</a></body></html>`))
	})
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><h1>About</h1></body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	items, _ := RunAll(context.Background(), []string{srv.URL + "/"})
	count := 0
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightWebhookStub {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 webhook-stub per origin; got %d", count)
	}
}
