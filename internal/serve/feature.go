// Package serve hosts the local browser UI for tailoring reviewqa-generated
// projects (the `reviewqa serve` subcommand introduced in v0.65).
//
// feature.go is the parser side. It reads a .feature file and emits a
// JSON-friendly shape (Feature → Scenarios → Steps). It is line-oriented
// and intentionally minimal — reviewqa generates a narrow subset of
// Gherkin (Feature header, optional As/I want/So lines, Scenario blocks
// with @tags + Given/When/Then/And/But steps). Tables, doc strings,
// outlines, and rules are out of scope for Phase A and would be added
// when they show up in the generated output.
package serve

import (
	"bufio"
	"os"
	"strings"

	"github.com/reviewqa/reviewqa/internal/composer"
)

// Feature is the parsed shape of one .feature file.
type Feature struct {
	Path      string     `json:"path"`
	Name      string     `json:"name"`
	Narrative string     `json:"narrative,omitempty"`
	Tags      []string   `json:"tags,omitempty"`
	Scenarios []Scenario `json:"scenarios"`
}

// Scenario is one Scenario block inside a Feature.
type Scenario struct {
	Name  string   `json:"name"`
	Tags  []string `json:"tags,omitempty"`
	Steps []Step   `json:"steps"`
}

// Step is one Given/When/Then/And/But line.
//
// Valid reports whether Text matches one of the registered step
// patterns in internal/composer. The UI uses it to surface
// LLM-hallucinated steps that wouldn't actually run through
// playwright-bdd — see [composer.MatchesRegisteredPattern].
type Step struct {
	Keyword string `json:"keyword"`
	Text    string `json:"text"`
	Valid   bool   `json:"valid"`
}

// ParseFeatureFile reads path and returns the parsed Feature.
func ParseFeatureFile(path string) (*Feature, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	feat, err := parseFeature(bufio.NewScanner(f))
	if err != nil {
		return nil, err
	}
	feat.Path = path
	return feat, nil
}

// ParseFeatureBytes parses a .feature document from an in-memory buffer.
// Used by tests and by /api/feature when the caller already has the
// bytes.
func ParseFeatureBytes(b []byte) (*Feature, error) {
	return parseFeature(bufio.NewScanner(strings.NewReader(string(b))))
}

func parseFeature(sc *bufio.Scanner) (*Feature, error) {
	feat := &Feature{}
	var pendingTags []string
	var current *Scenario
	var narrative []string
	inFeatureHeader := false

	for sc.Scan() {
		raw := sc.Text()
		line := strings.TrimSpace(raw)

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "@") {
			pendingTags = append(pendingTags, splitTags(line)...)
			continue
		}

		if strings.HasPrefix(line, "Feature:") {
			feat.Name = strings.TrimSpace(strings.TrimPrefix(line, "Feature:"))
			feat.Tags = pendingTags
			pendingTags = nil
			inFeatureHeader = true
			continue
		}

		if strings.HasPrefix(line, "Scenario:") || strings.HasPrefix(line, "Scenario Outline:") {
			if current != nil {
				feat.Scenarios = append(feat.Scenarios, *current)
			}
			name := strings.TrimSpace(strings.TrimPrefix(line, "Scenario:"))
			name = strings.TrimSpace(strings.TrimPrefix(name, "Scenario Outline:"))
			current = &Scenario{Name: name, Tags: pendingTags}
			pendingTags = nil
			inFeatureHeader = false
			if len(narrative) > 0 {
				feat.Narrative = strings.TrimSpace(strings.Join(narrative, "\n"))
				narrative = nil
			}
			continue
		}

		if strings.HasPrefix(line, "Background:") {
			if current != nil {
				feat.Scenarios = append(feat.Scenarios, *current)
			}
			current = &Scenario{Name: "Background"}
			inFeatureHeader = false
			continue
		}

		if kw, rest, ok := splitStep(line); ok {
			if current == nil {
				continue
			}
			current.Steps = append(current.Steps, Step{
				Keyword: kw,
				Text:    rest,
				Valid:   composer.MatchesRegisteredPattern(rest),
			})
			continue
		}

		if inFeatureHeader {
			narrative = append(narrative, line)
		}
		_ = raw
	}

	if current != nil {
		feat.Scenarios = append(feat.Scenarios, *current)
	}
	if feat.Narrative == "" && len(narrative) > 0 {
		feat.Narrative = strings.TrimSpace(strings.Join(narrative, "\n"))
	}
	return feat, sc.Err()
}

// stepKeywords lists the Gherkin keywords we recognise. Order matters
// for the longest-prefix match (so "Given" wins over "G", trivially).
var stepKeywords = []string{"Given", "When", "Then", "And", "But", "*"}

func splitStep(line string) (string, string, bool) {
	for _, kw := range stepKeywords {
		if strings.HasPrefix(line, kw+" ") || line == kw {
			rest := strings.TrimSpace(strings.TrimPrefix(line, kw))
			return kw, rest, true
		}
	}
	return "", "", false
}

func splitTags(line string) []string {
	fields := strings.Fields(line)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if strings.HasPrefix(f, "@") {
			out = append(out, f)
		}
	}
	return out
}
