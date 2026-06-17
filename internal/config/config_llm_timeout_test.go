package config

import (
	"testing"
	"time"
)

func TestLLMTimeout_HonorsEnvOverride(t *testing.T) {
	t.Setenv("REVIEWQA_LLM_TIMEOUT", "120s")
	c := FromEnv()
	if c.LLMTimeout != 120*time.Second {
		t.Errorf("REVIEWQA_LLM_TIMEOUT override not honored: %v", c.LLMTimeout)
	}
}

func TestLLMTimeout_DefaultBumpedTo60s(t *testing.T) {
	t.Setenv("REVIEWQA_LLM_TIMEOUT", "")
	c := FromEnv()
	if c.LLMTimeout != 60*time.Second {
		t.Errorf("v0.48 — default LLMTimeout should be 60s (bumped from 20s for slower local LLMs); got %v", c.LLMTimeout)
	}
}
