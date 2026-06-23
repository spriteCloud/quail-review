package main

import (
	"testing"

	"github.com/spriteCloud/quail-core/config"
)

// Regression test for the double-/v1 bug: probe-demo's LLM_BASE_URL
// secret ends in /v1, applyLLMOverride used to append another /v1, the
// chat-completions call landed on …/v1/v1/chat/completions and Ollama
// returned 404 for every composer + humanize attempt.
func TestApplyLLMOverride_NormalizesV1Suffix(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare", "http://host:11434", "http://host:11434/v1"},
		{"bare-trailing-slash", "http://host:11434/", "http://host:11434/v1"},
		{"already-v1", "http://host:11434/v1", "http://host:11434/v1"},
		{"already-v1-trailing-slash", "http://host:11434/v1/", "http://host:11434/v1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			applyLLMOverride(cfg, tc.in)
			if cfg.OpenAIBaseURL != tc.want {
				t.Fatalf("OpenAIBaseURL = %q, want %q", cfg.OpenAIBaseURL, tc.want)
			}
		})
	}
}

func TestApplyLLMOverride_EmptyIsNoOp(t *testing.T) {
	cfg := &config.Config{OpenAIBaseURL: "http://preserve-me/v1"}
	applyLLMOverride(cfg, "")
	if cfg.OpenAIBaseURL != "http://preserve-me/v1" {
		t.Fatalf("empty input should be a no-op; got %q", cfg.OpenAIBaseURL)
	}
}
