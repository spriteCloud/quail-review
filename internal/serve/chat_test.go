package serve

import (
	"context"
	"strings"
	"testing"
)

func TestParseChatResponse_TextOnly(t *testing.T) {
	got := parseChatResponse("Sure, here's what I'd change... but I want to confirm the page first. Could you share the exact URL?")
	if got.Proposed != "" {
		t.Errorf("expected empty proposed for text-only reply; got %q", got.Proposed)
	}
	if !strings.Contains(got.Assistant, "Could you share") {
		t.Errorf("assistant text mismatch: %q", got.Assistant)
	}
}

func TestParseChatResponse_WithProposedBlock(t *testing.T) {
	raw := "I've added a step to assert the success toast appears.\n---SCENARIO---\n  @ai-refined\n  Scenario: submit and see the success toast\n    Given I open the landing page\n    When I submit the form\n    Then I see the heading \"Thank you\""
	got := parseChatResponse(raw)
	if !strings.Contains(got.Assistant, "added a step") {
		t.Errorf("assistant text: %q", got.Assistant)
	}
	if !strings.Contains(got.Proposed, "Scenario: submit and see the success toast") {
		t.Errorf("proposed block: %q", got.Proposed)
	}
}

func TestParseChatResponse_StripsCodeFences(t *testing.T) {
	raw := "Done.\n---SCENARIO---\n```gherkin\n  Scenario: foo\n    Given I open the landing page\n    Then I remain on the same page\n```"
	got := parseChatResponse(raw)
	if strings.Contains(got.Proposed, "```") {
		t.Errorf("backtick fence not stripped: %q", got.Proposed)
	}
}

func TestChat_RequiresUserMessage(t *testing.T) {
	if _, err := Chat(context.Background(), ChatInput{Scenario: "Scenario: x\n  Given I open the landing page"}); err == nil {
		t.Errorf("expected error when user message empty")
	}
}

func TestFirstUnregisteredStep_FindsHallucinatedStep(t *testing.T) {
	block := `  @ai-refined
  Scenario: hallucinated
    Given I open the landing page
    Then the form contains an email address field
`
	got := firstUnregisteredStep(block)
	if got == "" || !strings.Contains(got, "the form contains an email address field") {
		t.Errorf("expected the hallucinated step to be surfaced; got %q", got)
	}
}

func TestFirstUnregisteredStep_AllValidReturnsEmpty(t *testing.T) {
	block := `  @ai-refined
  Scenario: clean
    Given I open the landing page
    Then I see the heading "Hello"
`
	if got := firstUnregisteredStep(block); got != "" {
		t.Errorf("expected empty; got %q", got)
	}
}

func TestBuildChatUserPrompt_IncludesHistoryAndScenario(t *testing.T) {
	prompt := buildChatUserPrompt(ChatInput{
		Scenario: "Scenario: foo",
		User:     "tighten the assertion",
		History: []ChatMessage{
			{Role: "user", Content: "what does this Scenario do?"},
			{Role: "assistant", Content: "It opens the landing page..."},
		},
	}, nil)
	if !strings.Contains(prompt, "Scenario: foo") {
		t.Errorf("current scenario missing")
	}
	if !strings.Contains(prompt, "user: tighten the assertion") {
		t.Errorf("user message missing")
	}
	if !strings.Contains(prompt, "what does this Scenario do?") {
		t.Errorf("history missing")
	}
}
