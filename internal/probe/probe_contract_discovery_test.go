package probe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestRunAll_EmitsVisualSpecs(t *testing.T) {
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
		if it.Template == plan.TmplPlaywrightVisual {
			count++
			if !strings.HasPrefix(it.OutPath, "tests/e2e/visual/") {
				t.Errorf("visual OutPath should sit under tests/e2e/visual/; got %s", it.OutPath)
			}
		}
	}
	if count == 0 {
		t.Errorf("expected ≥1 visual spec; got 0")
	}
}

func TestGraphQLContractItems_EmitsWhenIntrospectionResponds(t *testing.T) {
	t.Setenv("QUAIL_PROBE_ALLOW_LOOPBACK", "1")
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		payload := map[string]any{
			"data": map[string]any{
				"__schema": map[string]any{
					"queryType":    map[string]any{"name": "Query"},
					"mutationType": nil,
					"types": []any{
						map[string]any{"kind": "OBJECT", "name": "Query", "fields": []any{
							map[string]any{"name": "ping", "args": []any{}, "type": map[string]any{"kind": "SCALAR", "name": "String"}},
						}},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><h1>Home</h1></body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	items := graphQLContractItems(context.Background(), srv.URL+"/", "")
	if len(items) != 1 {
		t.Fatalf("expected 1 GraphQL item; got %d", len(items))
	}
	if items[0].Template != plan.TmplPlaywrightGraphQL {
		t.Errorf("template = %s; want graphql", items[0].Template)
	}
}

func TestWebhookContractItems_EmitsFromOpenAPI(t *testing.T) {
	t.Setenv("QUAIL_PROBE_ALLOW_LOOPBACK", "1")
	mux := http.NewServeMux()
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"openapi": "3.0.0",
			"info": {"title": "X", "version": "1"},
			"paths": {
				"/webhooks/stripe": {"post": {"responses": {"200": {"description": "ok"}}}},
				"/users": {"get": {"responses": {"200": {"description": "ok"}}}}
			}
		}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	items := webhookContractItems(context.Background(), srv.URL+"/", "")
	if len(items) != 1 {
		t.Fatalf("expected 1 webhook item; got %d", len(items))
	}
	it := items[0]
	if it.Template != plan.TmplPlaywrightWebhook {
		t.Errorf("template = %s", it.Template)
	}
	gotEndpoints := []string{}
	for _, a := range it.Symbol.Anchors {
		gotEndpoints = append(gotEndpoints, a.Name)
	}
	if len(gotEndpoints) != 1 || gotEndpoints[0] != "/webhooks/stripe" {
		t.Errorf("expected single /webhooks/stripe endpoint; got %v", gotEndpoints)
	}
}

func TestLooksLikeWebhookPath(t *testing.T) {
	yes := []string{"/webhooks/stripe", "/api/webhooks/github", "/foo/webhook", "/v1/webhooks"}
	no := []string{"/users", "/api/orders", "/", "/web"}
	for _, p := range yes {
		if !looksLikeWebhookPath(p) {
			t.Errorf("%q should look like a webhook path", p)
		}
	}
	for _, p := range no {
		if looksLikeWebhookPath(p) {
			t.Errorf("%q should NOT look like a webhook path", p)
		}
	}
}
