package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spriteCloud/quail-review/internal/config"
)

func TestHumanize_FallsBackOnStructureMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{
					// strip an import line -> structure must mismatch
					"content": `{"rewrites":[{"from":"import { add } from './math'","to":"// stripped"}]}`,
				},
			}},
		})
	}))
	defer srv.Close()
	c := New(config.Config{
		OpenAIBaseURL: srv.URL, OpenAIAPIKey: "x", Model: "test",
		LLMTimeout: 2 * time.Second, LLMTokenCap: 256,
	})
	in := []byte("import { add } from './math'\ndescribe('add', () => { it('works', () => expect(add(1,2)).toBe(3)) })\n")
	out := c.Humanize(context.Background(), "ts", "add", in)
	if string(out) != string(in) {
		t.Errorf("expected fallback to original, got: %s", out)
	}
}

func TestHumanize_AppliesTitleRewrites(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]string{
					"content": `{"rewrites":[{"from":"returns a value for valid input","to":"adds two numbers correctly"}]}`,
				},
			}},
		})
	}))
	defer srv.Close()
	c := New(config.Config{
		OpenAIBaseURL: srv.URL, OpenAIAPIKey: "x", Model: "test",
		LLMTimeout: 2 * time.Second, LLMTokenCap: 256,
	})
	in := []byte("import { add } from './math'\ndescribe('add', () => { it('returns a value for valid input', () => expect(add(1,2)).toBe(3)) })\n")
	out := c.Humanize(context.Background(), "ts", "add", in)
	if !strings.Contains(string(out), "adds two numbers correctly") {
		t.Errorf("rewrite not applied: %s", out)
	}
}

func TestHumanize_FallsBackOnHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", 500)
	}))
	defer srv.Close()
	c := New(config.Config{
		OpenAIBaseURL: srv.URL, OpenAIAPIKey: "x", Model: "test",
		LLMTimeout: 2 * time.Second, LLMTokenCap: 256,
	})
	in := []byte("describe('x', () => { it('y', () => {}) })\n")
	out := c.Humanize(context.Background(), "ts", "x", in)
	if string(out) != string(in) {
		t.Error("expected fallback on 500")
	}
}

func TestDisabledWhenNoKey(t *testing.T) {
	c := New(config.Config{Model: "m"})
	if c.Enabled() {
		t.Error("should be disabled without api key")
	}
}
