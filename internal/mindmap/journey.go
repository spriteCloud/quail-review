package mindmap

import (
	"sort"
	"strings"

	"github.com/reviewqa/reviewqa/internal/ast"
)

// JourneyKind names the user goal a generated test reflects.
type JourneyKind string

const (
	JourneyConvert JourneyKind = "convert" // home → form → submit
	JourneyBrowse  JourneyKind = "browse"  // home → list → detail
	JourneyExplore JourneyKind = "explore" // home → top-nav link → content
	JourneyRead    JourneyKind = "read"    // home → article-shaped page
)

// Step is one leg of a journey. EnteredVia is empty for the first step
// (visited via direct goto); the path of the clicked link for chained
// steps.
type Step struct {
	Page       *Page
	EnteredVia string // href clicked to reach this page; empty for step 1
}

// Journey is a sequence of Steps tied to a user goal. Each Journey becomes
// one generated spec file.
type Journey struct {
	Kind  JourneyKind
	Steps []Step
}

// IdentifyJourneys walks the Map and returns up to maxPerKind journeys per
// kind. Deterministic ordering: convert first, browse second, explore
// third, read fourth — matches typical test-suite priorities.
func IdentifyJourneys(m *Map, maxPerKind int) []Journey {
	if maxPerKind <= 0 {
		maxPerKind = 1
	}
	var out []Journey
	out = append(out, findConvertJourneys(m, maxPerKind)...)
	out = append(out, findBrowseJourneys(m, maxPerKind)...)
	out = append(out, findExploreJourneys(m, maxPerKind)...)
	out = append(out, findReadJourneys(m, maxPerKind)...)
	return out
}

// findConvertJourneys returns journeys that start at the landing page and
// end at a form submit. Each form page surfaces as one journey.
func findConvertJourneys(m *Map, max int) []Journey {
	landing := landingPage(m)
	if landing == nil {
		return nil
	}
	var out []Journey
	for _, url := range m.Order {
		page := m.Pages[url]
		if !hasTag(page, TagForm) {
			continue
		}
		journey := Journey{Kind: JourneyConvert}
		if page.URL == landing.URL {
			// Form lives ON the landing page — single-step journey.
			journey.Steps = []Step{{Page: page}}
		} else {
			via := relativePath(page.URL)
			journey.Steps = []Step{
				{Page: landing},
				{Page: page, EnteredVia: via},
			}
		}
		out = append(out, journey)
		if len(out) >= max {
			break
		}
	}
	return out
}

// findBrowseJourneys returns journeys that walk landing → list → detail.
// At most one journey per list page; picks the first detail page reached
// via a list's outbound link.
func findBrowseJourneys(m *Map, max int) []Journey {
	landing := landingPage(m)
	if landing == nil {
		return nil
	}
	var out []Journey
	for _, listURL := range m.Order {
		listPage := m.Pages[listURL]
		if !hasTag(listPage, TagList) || listPage.URL == landing.URL {
			continue
		}
		detail := findDetailFrom(m, listPage)
		via := relativePath(listPage.URL)
		journey := Journey{Kind: JourneyBrowse}
		if listPage.URL == landing.URL {
			journey.Steps = []Step{{Page: listPage}}
		} else {
			journey.Steps = []Step{
				{Page: landing},
				{Page: listPage, EnteredVia: via},
			}
		}
		if detail != nil {
			journey.Steps = append(journey.Steps, Step{Page: detail, EnteredVia: relativePath(detail.URL)})
		}
		out = append(out, journey)
		if len(out) >= max {
			break
		}
	}
	return out
}

// findExploreJourneys returns journeys that walk the landing page through
// its top-ranked nav links to one or two non-list, non-form sub-pages.
func findExploreJourneys(m *Map, max int) []Journey {
	landing := landingPage(m)
	if landing == nil || len(landing.Links) == 0 {
		return nil
	}
	ranked := rankLinks(landing.Links)
	var out []Journey
	for _, l := range ranked {
		target := absoluteSameOrigin(m.Origin, landing.URL, l.Aria)
		if target == "" {
			continue
		}
		page, ok := m.Pages[target]
		if !ok {
			continue
		}
		// Don't double-count list or form pages — those are covered by
		// browse / convert journeys.
		if hasTag(page, TagList) || hasTag(page, TagForm) {
			continue
		}
		journey := Journey{Kind: JourneyExplore, Steps: []Step{
			{Page: landing},
			{Page: page, EnteredVia: relativePath(page.URL)},
		}}
		out = append(out, journey)
		if len(out) >= max {
			break
		}
	}
	return out
}

// findReadJourneys returns journeys against detail-tagged pages (long
// content). Useful for blog posts, articles, case-studies entries.
func findReadJourneys(m *Map, max int) []Journey {
	landing := landingPage(m)
	if landing == nil {
		return nil
	}
	var out []Journey
	for _, url := range m.Order {
		page := m.Pages[url]
		if !hasTag(page, TagDetail) || page.URL == landing.URL {
			continue
		}
		// Skip pages already used as browse-detail or convert end.
		if hasTag(page, TagForm) || hasTag(page, TagList) {
			continue
		}
		journey := Journey{Kind: JourneyRead, Steps: []Step{
			{Page: landing},
			{Page: page, EnteredVia: relativePath(page.URL)},
		}}
		out = append(out, journey)
		if len(out) >= max {
			break
		}
	}
	return out
}

func landingPage(m *Map) *Page {
	for _, url := range m.Order {
		page := m.Pages[url]
		if hasTag(page, TagLanding) {
			return page
		}
	}
	if len(m.Order) > 0 {
		return m.Pages[m.Order[0]]
	}
	return nil
}

func hasTag(p *Page, t string) bool {
	for _, x := range p.Tags {
		if x == t {
			return true
		}
	}
	return false
}

func findDetailFrom(m *Map, list *Page) *Page {
	for _, l := range list.Links {
		abs := absoluteSameOrigin(m.Origin, list.URL, l.Aria)
		if abs == "" {
			continue
		}
		candidate, ok := m.Pages[abs]
		if !ok {
			continue
		}
		if hasTag(candidate, TagDetail) && candidate.URL != list.URL {
			return candidate
		}
	}
	return nil
}

func relativePath(absURL string) string {
	// Take the path component — used as the link's href in the generated
	// `<a href="…">` selector.
	if idx := strings.Index(absURL, "://"); idx != -1 {
		rest := absURL[idx+3:]
		if slash := strings.Index(rest, "/"); slash != -1 {
			return rest[slash:]
		}
	}
	return absURL
}

// rankLinks scores links by user-action vocabulary and returns them in
// best-first order. Mirrors gen.rankedNavTargets so behaviour matches the
// templates' fallback nav picker.
func rankLinks(links []ast.LocatorAnchor) []ast.LocatorAnchor {
	type scored struct {
		anchor ast.LocatorAnchor
		score  int
	}
	var all []scored
	seen := map[string]bool{}
	vocab := []string{
		"contact", "pricing", "case studies", "case study", "services",
		"products", "features", "learn more", "book a demo", "get started",
	}
	avoid := []string{"privacy", "terms", "cookie", "legal", "sitemap"}
	for _, l := range links {
		if !strings.HasPrefix(l.Aria, "/") || strings.HasPrefix(l.Aria, "//") {
			continue
		}
		if seen[l.Aria] {
			continue
		}
		seen[l.Aria] = true
		s := scored{anchor: l}
		lowerText := strings.ToLower(l.Text)
		lowerHref := strings.ToLower(l.Aria)
		for _, v := range vocab {
			if strings.Contains(lowerText, v) {
				s.score += 3
				break
			}
		}
		for _, v := range vocab {
			if strings.Contains(lowerHref, strings.ReplaceAll(v, " ", "-")) {
				s.score += 2
				break
			}
		}
		for _, v := range avoid {
			if strings.Contains(lowerHref, v) {
				s.score -= 3
			}
		}
		if strings.Count(l.Aria, "/") <= 1 {
			s.score++
		}
		all = append(all, s)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	out := make([]ast.LocatorAnchor, 0, len(all))
	for _, s := range all {
		out = append(out, s.anchor)
	}
	return out
}
