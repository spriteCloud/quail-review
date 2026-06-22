package prompt

import (
	"net/url"
	"strings"

	"github.com/spriteCloud/quail-review/internal/mindmap"
)

// Apply narrows the given journeys to those that match the filter.
// Matching rules (any of the three constitutes a match):
//
//  1. The journey's Kind is in Filter.JourneyKinds.
//  2. Any step's page URL contains a PathHint substring.
//  3. Any step's page URL or title contains one of Filter.Keywords.
//
// Empty filters short-circuit and return the input unchanged — the
// caller is responsible for falling back to "no filter" when the user's
// prompt produced no recognisable signal.
func (f Filter) Apply(in []mindmap.Journey) []mindmap.Journey {
	if f.IsEmpty() {
		return in
	}
	kindSet := map[mindmap.JourneyKind]bool{}
	for _, k := range f.JourneyKinds {
		kindSet[k] = true
	}
	var out []mindmap.Journey
	for _, j := range in {
		if kindSet[j.Kind] {
			out = append(out, j)
			continue
		}
		matched := false
		for _, step := range j.Steps {
			if step.Page == nil {
				continue
			}
			if f.matchesPage(step.Page) {
				matched = true
				break
			}
		}
		if matched {
			out = append(out, j)
		}
	}
	return out
}

// matchesPage reports whether the page's URL/path/title matches any
// PathHint or Keyword in the filter.
func (f Filter) matchesPage(p *mindmap.Page) bool {
	pathLower := ""
	if u, err := url.Parse(p.URL); err == nil {
		pathLower = strings.ToLower(u.Path)
	}
	urlLower := strings.ToLower(p.URL)
	titleLower := strings.ToLower(p.Title)

	for _, hint := range f.PathHints {
		if strings.Contains(pathLower, hint) {
			return true
		}
	}
	for _, kw := range f.Keywords {
		if strings.Contains(pathLower, kw) || strings.Contains(urlLower, kw) || strings.Contains(titleLower, kw) {
			return true
		}
	}
	return false
}

// Describe returns a one-line human summary of what the filter matched
// against. Used by the CLI to echo "parsed your prompt as ..." before
// running the probe.
func (f Filter) Describe() string {
	if f.IsEmpty() {
		return "no filter (prompt produced no recognisable signal — emitting all journeys)"
	}
	parts := []string{}
	if len(f.JourneyKinds) > 0 {
		kinds := make([]string, len(f.JourneyKinds))
		for i, k := range f.JourneyKinds {
			kinds[i] = string(k)
		}
		parts = append(parts, "kinds: "+strings.Join(kinds, ", "))
	}
	if len(f.Keywords) > 0 {
		parts = append(parts, "keywords: "+strings.Join(f.Keywords, ", "))
	}
	if len(f.PathHints) > 0 {
		parts = append(parts, "paths: "+strings.Join(f.PathHints, ", "))
	}
	return strings.Join(parts, " | ")
}
