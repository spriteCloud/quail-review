package serve

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/reviewqa/reviewqa/internal/config"
	"github.com/reviewqa/reviewqa/internal/llm"
)

// llmConfigFromEnv mirrors the `applyLLMOverride` logic from
// cmd/reviewqa/main.go so the serve subcommand can pick up the same
// REVIEWQA_LLM env conventions: REVIEWQA_LLM sets the endpoint,
// REVIEWQA_MODEL overrides the model, an "ollama" sentinel populates
// the API key when none is provided.
//
// Saved settings from ~/.config/reviewqa/serve.json overlay the env-
// var defaults so a user can edit them from the Settings page without
// restarting. Settings.LLM.Enabled = false zeroes the API key,
// disabling the LLM regardless of env.
func llmConfigFromEnv() config.Config {
	cfg := config.FromEnv()
	if llm := strings.TrimSpace(os.Getenv("REVIEWQA_LLM")); llm != "" {
		cfg.OpenAIBaseURL = strings.TrimRight(llm, "/") + "/v1"
		if cfg.Model == "" || cfg.Model == "gpt-4o-mini" {
			cfg.Model = "qwen3-coder-next:latest"
		}
		if cfg.OpenAIAPIKey == "" {
			cfg.OpenAIAPIKey = "ollama"
		}
	}
	s, _ := LoadSettings()
	if s.LLM.Endpoint != "" {
		cfg.OpenAIBaseURL = strings.TrimRight(s.LLM.Endpoint, "/") + "/v1"
	}
	if s.LLM.Model != "" {
		cfg.Model = s.LLM.Model
	}
	if s.LLM.APIKey != "" {
		cfg.OpenAIAPIKey = s.LLM.APIKey
	} else if s.LLM.Endpoint != "" && cfg.OpenAIAPIKey == "" {
		cfg.OpenAIAPIKey = "ollama"
	}
	// Explicit OFF wins — strip the API key so downstream sees the
	// LLM as disabled and falls back to deterministic mode.
	if !s.LLM.Enabled && (s.LLM.Endpoint != "" || s.LLM.APIKey != "") {
		cfg.OpenAIAPIKey = ""
	}
	return cfg
}

// ComposeInput is the payload the UI sends to /api/compose-steps.
type ComposeInput struct {
	Gherkin string `json:"gherkin"`
	URL     string `json:"url"`
}

// ComposeResult is the LLM's composed Scenario plus the model/endpoint
// that produced it (so the user can audit the trail).
type ComposeResult struct {
	Gherkin string `json:"gherkin"`
	Model   string `json:"model,omitempty"`
	Notes   string `json:"notes,omitempty"`
}

// ComposeSteps takes the user's hand-written Gherkin (likely natural
// language) and produces a valid Scenario block whose steps match the
// registered step patterns. Uses the LLM if REVIEWQA_LLM is set;
// otherwise returns a permissive deterministic mapping plus a note.
func ComposeSteps(ctx context.Context, in ComposeInput) (*ComposeResult, error) {
	if strings.TrimSpace(in.Gherkin) == "" {
		return nil, errors.New("gherkin is empty")
	}
	if strings.TrimSpace(in.URL) == "" {
		return nil, errors.New("url is empty")
	}

	resolved, err := resolveTarget(in.URL, "")
	if err != nil {
		return nil, fmt.Errorf("resolve url: %w", err)
	}
	lm, err := FetchAndParseDOM(ctx, resolved)
	if err != nil {
		return nil, fmt.Errorf("probe %s: %w", resolved, err)
	}

	cfg := llmConfigFromEnv()
	if cfg.OpenAIAPIKey != "" {
		client := llm.New(cfg)
		if client.Enabled() {
			block, err := composeViaLLM(ctx, client, in.Gherkin, resolved, lm)
			if err == nil {
				return &ComposeResult{Gherkin: block, Model: cfg.Model}, nil
			}
			res := composeDeterministic(in.Gherkin, lm)
			res.Notes = "LLM call failed (" + err.Error() + "); used deterministic fallback."
			return res, nil
		}
	}
	res := composeDeterministic(in.Gherkin, lm)
	res.Notes = "LLM not configured (REVIEWQA_LLM unset). Used deterministic step-to-DOM matching; review and refine manually."
	return res, nil
}

const composeSystemPrompt = `You are a senior QA engineer producing one Playwright + playwright-bdd Scenario.

Given the user's plain-English description and the destination page's DOM landmarks, produce ONE Scenario whose steps EACH match exactly one of the registered patterns below. Substitute the angle-bracketed placeholders with concrete values drawn from the DOM.

Rules:
- Output STRICT GHERKIN ONLY. No markdown fences, no commentary.
- Exactly one Scenario block. Start with "Scenario: <name>".
- The first step MUST be "Given I open the landing page" or "Given I open the page \"<path>\"" or "Given I am on the landing page".
- 3-8 steps total. Tag with @ai-composed.
- Use only the registered patterns; substitute placeholders.
- Prefer destination-page H1/title text the DOM shows for headings/titles.

Registered step patterns:
Given I open the landing page
Given I am on the landing page
Given I open the page "<path>"
When I click the link to "<href>"
When I navigate directly to "<href>"
When I enter "<value>" into the "<field>" field
When I submit the form
Then the page title contains "<title>"
Then the main heading reads "<text>"
Then I see the heading "<text>"
Then the URL contains "<fragment>"
Then no error message is shown in the form region
Then the page has at least <n> items
When I scroll to the bottom of the page
When I reload the page
`

func composeViaLLM(ctx context.Context, client *llm.Client, userGherkin, url string, lm *DOMLandmarks) (string, error) {
	user := buildComposeUserPrompt(userGherkin, url, lm)
	raw, err := client.Chat(ctx, composeSystemPrompt, user)
	if err != nil {
		return "", err
	}
	block := extractScenarioBlock(raw)
	if err := validateScenarioBlock(block); err != nil {
		// Surface the validation error for the UI's compose-result
		// note; the deterministic fallback still kicks in upstream.
		return "", fmt.Errorf("LLM output did not validate: %w", err)
	}
	return block, nil
}

func buildComposeUserPrompt(userGherkin, target string, lm *DOMLandmarks) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Destination URL: %s\n", target)
	if lm.Title != "" {
		fmt.Fprintf(&b, "Page title: %s\n", lm.Title)
	}
	if len(lm.Headings) > 0 {
		headings := make([]string, 0, len(lm.Headings))
		for _, h := range lm.Headings {
			headings = append(headings, fmt.Sprintf("h%d: %q", h.Level, h.Text))
		}
		fmt.Fprintf(&b, "Headings: %s\n", strings.Join(headings, "; "))
	}
	if len(lm.Links) > 0 {
		links := topLinks(lm.Links, 10)
		fmt.Fprintf(&b, "Links: %s\n", strings.Join(links, "; "))
	}
	if len(lm.Forms) > 0 {
		forms := summariseForms(lm.Forms)
		fmt.Fprintf(&b, "Forms: %s\n", forms)
	}
	if len(lm.Buttons) > 0 {
		btns := make([]string, 0, len(lm.Buttons))
		for _, b := range lm.Buttons {
			name := b.Text
			if name == "" {
				name = b.AriaLabel
			}
			btns = append(btns, fmt.Sprintf("%q", name))
		}
		fmt.Fprintf(&b, "Buttons: %s\n", strings.Join(btns, ", "))
	}
	b.WriteString("\nUser's draft Scenario (natural language, may use placeholders):\n")
	b.WriteString(userGherkin)
	b.WriteString("\n")
	return b.String()
}

func topLinks(in []DOMLink, n int) []string {
	out := make([]string, 0, n)
	for i, l := range in {
		if i >= n {
			break
		}
		out = append(out, fmt.Sprintf("%q→%s", l.Text, l.Href))
	}
	return out
}

func summariseForms(forms []DOMForm) string {
	parts := make([]string, 0, len(forms))
	for _, f := range forms {
		names := make([]string, 0, len(f.Inputs))
		for _, in := range f.Inputs {
			name := in.Label
			if name == "" {
				name = in.Placeholder
			}
			if name == "" {
				name = in.Name
			}
			if name != "" {
				names = append(names, name)
			}
		}
		desc := f.Action
		if desc == "" {
			desc = "(no action)"
		}
		parts = append(parts, fmt.Sprintf("%s [%s]", desc, strings.Join(names, ", ")))
	}
	return strings.Join(parts, " | ")
}

// extractScenarioBlock pulls the Scenario from the LLM's response,
// trimming any preamble or trailing chatter the model added despite
// the prompt's strict instructions.
func extractScenarioBlock(raw string) string {
	lines := strings.Split(raw, "\n")
	start := -1
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "@") || strings.HasPrefix(t, "Scenario:") || strings.HasPrefix(t, "Scenario Outline:") {
			start = i
			break
		}
	}
	if start == -1 {
		return raw
	}
	// Stop at the first blank line followed by non-step content (the
	// LLM sometimes appends an explanation).
	end := len(lines)
	for i := start; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			continue
		}
		if i == start {
			continue
		}
		if !looksLikeGherkinLine(t) {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

func looksLikeGherkinLine(t string) bool {
	if strings.HasPrefix(t, "@") || strings.HasPrefix(t, "#") {
		return true
	}
	if strings.HasPrefix(t, "Scenario:") || strings.HasPrefix(t, "Scenario Outline:") {
		return true
	}
	for _, kw := range []string{"Given", "When", "Then", "And", "But"} {
		if strings.HasPrefix(t, kw+" ") {
			return true
		}
	}
	return false
}

// composeDeterministic is the LLM-off fallback. It returns a Scenario
// that starts at the landing page, navigates to the URL's path, and
// asserts on the page's H1. The block is always valid for ParseFeature.
func composeDeterministic(userGherkin string, lm *DOMLandmarks) *ComposeResult {
	heading := ""
	for _, h := range lm.Headings {
		if h.Level == 1 {
			heading = h.Text
			break
		}
	}
	if heading == "" && len(lm.Headings) > 0 {
		heading = lm.Headings[0].Text
	}
	path := "/"
	if u, err := url.Parse(lm.URL); err == nil && u.Path != "" {
		path = u.Path
	}
	name := "ai-composed from user input"
	if first := firstLineOfFreeGherkin(userGherkin); first != "" {
		name = first
	}
	var b strings.Builder
	b.WriteString("  @ai-composed\n")
	fmt.Fprintf(&b, "  Scenario: %s\n", name)
	b.WriteString("    Given I open the landing page\n")
	fmt.Fprintf(&b, "    When I navigate directly to \"%s\"\n", path)
	if heading != "" {
		fmt.Fprintf(&b, "    Then I see the heading \"%s\"\n", heading)
	} else {
		b.WriteString("    Then I remain on the same page\n")
	}
	return &ComposeResult{Gherkin: b.String()}
}

func firstLineOfFreeGherkin(s string) string {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		// Normalize "Scenario: foo" → "foo".
		t = strings.TrimSpace(strings.TrimPrefix(t, "Scenario:"))
		// Strip surrounding quotes if the user pasted "..."
		t = strings.Trim(t, `"`)
		// Cap length so the Scenario name stays readable.
		if len(t) > 80 {
			t = t[:80]
		}
		return t
	}
	return ""
}

