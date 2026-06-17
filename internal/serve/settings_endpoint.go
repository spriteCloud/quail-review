package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/reviewqa/reviewqa/internal/llm"
)

// handleSettings implements GET (load) and POST (save) for the user
// Settings file at ~/.config/reviewqa/serve.json.
//
// The API key is returned as-is — the serve UI is local-only and the
// browser already speaks to 127.0.0.1; masking it here would force a
// separate /api/settings/secret endpoint without changing the threat
// model. The frontend renders the key as a password field.
func handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s, err := LoadSettings()
		if err != nil {
			http.Error(w, "load settings: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, s)
	case http.MethodPost:
		var s Settings
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := SaveSettings(s); err != nil {
			http.Error(w, "save settings: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// LLMTestRequest accepts the fields the user is about to save so the
// "Test connection" button can validate them BEFORE they're written
// to disk.
type LLMTestRequest struct {
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
	APIKey   string `json:"apiKey"`
}

// handleLLMTest sends a single tiny chat completion to the configured
// endpoint and reports whether the round-trip succeeded. Used by the
// Settings page's Test-connection button.
//
// We do NOT save the inputs — the UI calls /api/settings POST
// separately once the test passes.
func handleLLMTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req LLMTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Endpoint) == "" {
		writeJSON(w, map[string]any{"ok": false, "error": "endpoint is empty"})
		return
	}

	// Build an ephemeral config from the request (does NOT touch
	// disk or env). Falls back to the "ollama" sentinel for keyless
	// local endpoints, mirroring llmConfigFromEnv.
	cfg := llmConfigFromEnv()
	cfg.OpenAIBaseURL = strings.TrimRight(req.Endpoint, "/") + "/v1"
	if req.Model != "" {
		cfg.Model = req.Model
	}
	if req.APIKey != "" {
		cfg.OpenAIAPIKey = req.APIKey
	} else if cfg.OpenAIAPIKey == "" {
		cfg.OpenAIAPIKey = "ollama"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	client := llm.New(cfg)
	if !client.Enabled() {
		writeJSON(w, map[string]any{"ok": false, "error": "llm client could not be constructed (missing model?)"})
		return
	}
	raw, err := client.Chat(ctx, "Respond with the single word OK.", "ping")
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{
		"ok":     true,
		"model":  cfg.Model,
		"sample": truncate(raw, 80),
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// silence unused-import linter when client.Enabled returns false and
// we never use errors.New. Kept so the file compiles even if the LLM
// client interface evolves.
var _ = errors.New
