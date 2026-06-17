package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// useTempSettings points the LoadSettings/SaveSettings pair at a
// throwaway file under t.TempDir() via REVIEWQA_SETTINGS_PATH so
// tests never touch the real ~/.config/reviewqa/serve.json.
func useTempSettings(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv("REVIEWQA_SETTINGS_PATH", path)
	return path
}

func TestLoadSettings_MissingFileReturnsZero(t *testing.T) {
	useTempSettings(t)
	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.LLM.Enabled || s.LLM.Endpoint != "" {
		t.Errorf("expected zero LLM, got %+v", s.LLM)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	useTempSettings(t)
	want := Settings{
		LLM: LLMSettings{
			Enabled:    true,
			Endpoint:   "http://100.82.34.115:11434",
			Model:      "qwen3-coder-next:latest",
			APIKey:     "ollama",
			TimeoutSec: 90,
		},
		Probe: ProbeSettings{DefaultCoverage: "depth"},
		Run:   RunSettings{TimeoutSec: 1200, KeepReport: true},
	}
	if err := SaveSettings(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadSettings()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != want {
		t.Errorf("round-trip mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestSettingsEndpoint_GETReturnsCurrent(t *testing.T) {
	useTempSettings(t)
	_ = SaveSettings(Settings{LLM: LLMSettings{Endpoint: "http://example.test"}})

	root := fixtureProject(t)
	workdir := filepath.Join(root, "tests", "e2e")
	srv := httptest.NewServer(Handler(workdir))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var s Settings
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.LLM.Endpoint != "http://example.test" {
		t.Errorf("endpoint = %q, want %q", s.LLM.Endpoint, "http://example.test")
	}
}

func TestSettingsEndpoint_POSTPersists(t *testing.T) {
	useTempSettings(t)
	root := fixtureProject(t)
	workdir := filepath.Join(root, "tests", "e2e")
	srv := httptest.NewServer(Handler(workdir))
	defer srv.Close()

	body, _ := json.Marshal(Settings{
		LLM: LLMSettings{Enabled: true, Endpoint: "http://saved.test", Model: "m"},
	})
	resp, err := http.Post(srv.URL+"/api/settings", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	s, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s.LLM.Endpoint != "http://saved.test" {
		t.Errorf("endpoint not persisted: %+v", s)
	}
}

func TestLLMTestEndpoint_RejectsEmptyEndpoint(t *testing.T) {
	useTempSettings(t)
	root := fixtureProject(t)
	workdir := filepath.Join(root, "tests", "e2e")
	srv := httptest.NewServer(Handler(workdir))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/llm-test", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["ok"] != false {
		t.Errorf("ok = %v, want false", out["ok"])
	}
}

func TestLLMConfigFromEnv_SettingsOverrideEnv(t *testing.T) {
	useTempSettings(t)
	t.Setenv("REVIEWQA_LLM", "http://from-env")
	t.Setenv("REVIEWQA_MODEL", "")
	t.Setenv("OPENAI_API_KEY", "")
	_ = SaveSettings(Settings{
		LLM: LLMSettings{
			Enabled: true, Endpoint: "http://from-settings", Model: "saved-model", APIKey: "saved-key",
		},
	})
	cfg := llmConfigFromEnv()
	if cfg.OpenAIBaseURL != "http://from-settings/v1" {
		t.Errorf("base url: %q", cfg.OpenAIBaseURL)
	}
	if cfg.Model != "saved-model" {
		t.Errorf("model: %q", cfg.Model)
	}
	if cfg.OpenAIAPIKey != "saved-key" {
		t.Errorf("key: %q", cfg.OpenAIAPIKey)
	}
}

func TestLLMConfigFromEnv_DisabledStripsKey(t *testing.T) {
	useTempSettings(t)
	t.Setenv("REVIEWQA_LLM", "http://env-only")
	t.Setenv("OPENAI_API_KEY", "real-key")
	_ = SaveSettings(Settings{
		LLM: LLMSettings{Enabled: false, Endpoint: "http://saved", APIKey: "saved-key"},
	})
	cfg := llmConfigFromEnv()
	if cfg.OpenAIAPIKey != "" {
		t.Errorf("expected disabled (empty key), got %q", cfg.OpenAIAPIKey)
	}
}
