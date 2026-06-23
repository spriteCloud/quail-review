package llm

import (
	"context"
	"testing"

	"github.com/spriteCloud/quail-core/config"
)

func TestHumanize_HonorsHumanizeEnvSkip(t *testing.T) {
	t.Setenv("QUAIL_HUMANIZE", "0")
	// Pretend the LLM is enabled but the env var forces a skip — the
	// content must come back unchanged without any HTTP call.
	c := New(config.Config{OpenAIAPIKey: "test", Model: "x", OpenAIBaseURL: "http://does-not-resolve.invalid/v1"})
	in := []byte("/* deterministic */\nexport const x = 1\n")
	out := c.Humanize(context.Background(), "ts", "X", in)
	if string(out) != string(in) {
		t.Errorf("QUAIL_HUMANIZE=0 should short-circuit; got modified content")
	}
}

func TestHumanize_NormalSkipWhenDisabled(t *testing.T) {
	t.Setenv("QUAIL_HUMANIZE", "")
	c := New(config.Config{}) // no key, no model
	in := []byte("content")
	out := c.Humanize(context.Background(), "ts", "X", in)
	if string(out) != string(in) {
		t.Errorf("disabled client should pass content through unchanged")
	}
}
