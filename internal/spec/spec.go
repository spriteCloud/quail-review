// Package spec embeds the specs the quail binary references at runtime.
package spec

import _ "embed"

// ExploreGuardrails is the AI guardrails spec that gates every LLM response
// in `quail explore`. Sourced from explore_guardrails.md at build time.
//
//go:embed explore_guardrails.md
var ExploreGuardrails string
