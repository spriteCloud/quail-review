package mindmap

import (
	"sort"
	"strings"

	"github.com/reviewqa/reviewqa/internal/ast"
)

// JourneyKind names the user goal a generated test reflects.
type JourneyKind string

const (
	JourneyConvert  JourneyKind = "convert"  // home → form → submit (newsletter / lead-gen)
	JourneyExercise JourneyKind = "exercise" // single-page in-page component interactions
	JourneyContact  JourneyKind = "contact"  // home → contact page → fill+submit
	JourneyEvaluate JourneyKind = "evaluate" // home → pricing page (assert prices visible)
	JourneyResearch JourneyKind = "research" // home → case-studies list → one case-study
	JourneyBrowse   JourneyKind = "browse"   // home → list → detail
	JourneyDiscover JourneyKind = "discover" // home → service page → CTA on that page
	JourneyExplore  JourneyKind = "explore"  // home → top-nav link → sub-page
	JourneyRead     JourneyKind = "read"     // home → article-shaped page
)

// JourneyExercisesForm reports whether a journey kind's purpose is to
// fill+submit a form. Only these three keep form anchors on the landing
// step; others have form anchors stripped so they don't accidentally
// submit the homepage email signup before navigating.
func JourneyExercisesForm(k JourneyKind) bool {
	switch k {
	case JourneyConvert, JourneyContact:
		return true
	}
	return false
}

// journeyPriority orders kinds for dedup tie-breaking — higher priority
// wins when two journeys terminate at the same page.
var journeyPriority = map[JourneyKind]int{
	JourneyConvert:  100,
	JourneyExercise: 95,
	JourneyContact:  90,
	JourneyEvaluate: 80,
	JourneyResearch: 70,
	JourneyBrowse:   60,
	JourneyDiscover: 50,
	JourneyExplore:  40,
	JourneyRead:     30,
}

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
// kind. Order matches journeyPriority (convert first, read last). Result
// is deduped by (terminal-page-URL) — when two kinds want the same end
// page the higher-priority kind keeps it.
func IdentifyJourneys(m *Map, maxPerKind int) []Journey {
	if maxPerKind <= 0 {
		maxPerKind = 1
	}
	var out []Journey
	out = append(out, findConvertJourneys(m, maxPerKind)...)
	out = append(out, findExerciseJourneys(m, maxPerKind)...)
	out = append(out, findContactJourneys(m, maxPerKind)...)
	out = append(out, findEvaluateJourneys(m, maxPerKind)...)
	out = append(out, findResearchJourneys(m, maxPerKind)...)
	out = append(out, findBrowseJourneys(m, maxPerKind)...)
	out = append(out, findDiscoverJourneys(m, maxPerKind)...)
	out = append(out, findExploreJourneys(m, maxPerKind)...)
	out = append(out, findReadJourneys(m, maxPerKind)...)
	return dedupJourneys(out)
}

// dedupJourneys drops journeys whose terminal page is already covered by
// a higher-priority kind, and drops within-kind duplicates that end on the
// same page.
func dedupJourneys(in []Journey) []Journey {
	type slot struct {
		j   Journey
		pri int
	}
	bestByTerminal := map[string]slot{}
	order := []string{}
	for _, j := range in {
		if len(j.Steps) == 0 {
			continue
		}
		terminal := j.Steps[len(j.Steps)-1].Page.URL
		pri := journeyPriority[j.Kind]
		if cur, ok := bestByTerminal[terminal]; ok {
			if pri <= cur.pri {
				continue
			}
		} else {
			order = append(order, terminal)
		}
		bestByTerminal[terminal] = slot{j: j, pri: pri}
	}
	out := make([]Journey, 0, len(order))
	for _, t := range order {
		out = append(out, bestByTerminal[t].j)
	}
	// Re-sort by kind priority for deterministic file naming.
	sort.SliceStable(out, func(i, j int) bool {
		return journeyPriority[out[i].Kind] > journeyPriority[out[j].Kind]
	})
	return out
}

// findConvertJourneys returns journeys that start at the landing page and
// end at a form submit. The landing-as-form case is emitted as a single
// step; further form pages chain from landing. Skips re-emitting the
// landing page when it has already been emitted as a single-step convert.
func findConvertJourneys(m *Map, max int) []Journey {
	landing := landingPage(m)
	if landing == nil {
		return nil
	}
	var out []Journey
	landingEmitted := false
	for _, url := range m.Order {
		page := m.Pages[url]
		if !hasTag(page, TagForm) {
			continue
		}
		// Skip contact-form pages — they have their own dedicated journey.
		if hasTag(page, TagContact) {
			continue
		}
		journey := Journey{Kind: JourneyConvert}
		if page.URL == landing.URL {
			if landingEmitted {
				continue
			}
			landingEmitted = true
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

// findExerciseJourneys: single-page journeys against pages that carry
// interactive components. The first such page (typically landing) is the
// primary target; if other crawled pages also have interactions, additional
// exercise journeys are emitted up to max.
func findExerciseJourneys(m *Map, max int) []Journey {
	var out []Journey
	for _, url := range m.Order {
		page := m.Pages[url]
		if !hasTag(page, TagInteractive) {
			continue
		}
		out = append(out, Journey{Kind: JourneyExercise, Steps: []Step{{Page: page}}})
		if len(out) >= max {
			break
		}
	}
	return out
}

// findContactJourneys: landing → contact page (with form) → fill+submit.
func findContactJourneys(m *Map, max int) []Journey {
	landing := landingPage(m)
	if landing == nil {
		return nil
	}
	var out []Journey
	for _, url := range m.Order {
		page := m.Pages[url]
		if !hasTag(page, TagContact) {
			continue
		}
		journey := Journey{Kind: JourneyContact}
		if page.URL == landing.URL {
			journey.Steps = []Step{{Page: page}}
		} else {
			journey.Steps = []Step{
				{Page: landing},
				{Page: page, EnteredVia: relativePath(page.URL)},
			}
		}
		out = append(out, journey)
		if len(out) >= max {
			break
		}
	}
	return out
}

// findEvaluateJourneys: landing → pricing page.
func findEvaluateJourneys(m *Map, max int) []Journey {
	landing := landingPage(m)
	if landing == nil {
		return nil
	}
	var out []Journey
	for _, url := range m.Order {
		page := m.Pages[url]
		if !hasTag(page, TagPricing) {
			continue
		}
		journey := Journey{Kind: JourneyEvaluate}
		if page.URL == landing.URL {
			journey.Steps = []Step{{Page: page}}
		} else {
			journey.Steps = []Step{
				{Page: landing},
				{Page: page, EnteredVia: relativePath(page.URL)},
			}
		}
		out = append(out, journey)
		if len(out) >= max {
			break
		}
	}
	return out
}

// findResearchJourneys: landing → list-of-case-studies → case-study detail.
// Falls back to landing → case-study (direct) if no list is in the map.
func findResearchJourneys(m *Map, max int) []Journey {
	landing := landingPage(m)
	if landing == nil {
		return nil
	}
	var out []Journey
	emitted := map[string]bool{}
	// Path A: a case-study list page exists (e.g. /case-studies).
	for _, listURL := range m.Order {
		listPage := m.Pages[listURL]
		if !hasTag(listPage, TagList) {
			continue
		}
		path := pathLower(listPage.URL)
		if !strings.Contains(path, "case-stud") && !strings.Contains(path, "stories") {
			continue
		}
		detail := findFirst(m, listPage, func(p *Page) bool { return hasTag(p, TagCaseStudy) })
		journey := Journey{Kind: JourneyResearch, Steps: []Step{{Page: landing}}}
		if listPage.URL != landing.URL {
			journey.Steps = append(journey.Steps, Step{Page: listPage, EnteredVia: relativePath(listPage.URL)})
		}
		if detail != nil {
			journey.Steps = append(journey.Steps, Step{Page: detail, EnteredVia: relativePath(detail.URL)})
			emitted[detail.URL] = true
		}
		if len(journey.Steps) > 1 {
			out = append(out, journey)
			if len(out) >= max {
				return out
			}
		}
	}
	// Path B: case-study pages with no list hub — surface them directly.
	for _, url := range m.Order {
		page := m.Pages[url]
		if !hasTag(page, TagCaseStudy) || emitted[page.URL] {
			continue
		}
		if page.URL == landing.URL {
			continue
		}
		journey := Journey{Kind: JourneyResearch, Steps: []Step{
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

// findBrowseJourneys returns journeys that walk landing → list → detail.
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
		// Case-study lists belong to the research journey.
		path := pathLower(listPage.URL)
		if strings.Contains(path, "case-stud") || strings.Contains(path, "stories") {
			continue
		}
		detail := findFirst(m, listPage, func(p *Page) bool { return hasTag(p, TagDetail) && p.URL != listPage.URL })
		via := relativePath(listPage.URL)
		journey := Journey{Kind: JourneyBrowse, Steps: []Step{
			{Page: landing},
			{Page: listPage, EnteredVia: via},
		}}
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

// findDiscoverJourneys: landing → service page. Each service page becomes
// one spec — gives marketing sites depth across distinct offerings.
func findDiscoverJourneys(m *Map, max int) []Journey {
	landing := landingPage(m)
	if landing == nil {
		return nil
	}
	var out []Journey
	for _, url := range m.Order {
		page := m.Pages[url]
		if !hasTag(page, TagService) || page.URL == landing.URL {
			continue
		}
		journey := Journey{Kind: JourneyDiscover, Steps: []Step{
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

// findExploreJourneys returns journeys through top-ranked nav links to
// pages NOT already covered by a more specific journey kind.
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
		// Skip pages already covered by more-specific kinds.
		if hasTag(page, TagList) || hasTag(page, TagForm) ||
			hasTag(page, TagPricing) || hasTag(page, TagContact) ||
			hasTag(page, TagAuth) || hasTag(page, TagService) ||
			hasTag(page, TagCaseStudy) {
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
// content). Useful for blog posts, articles.
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
		// Skip pages owned by other journey kinds.
		if hasTag(page, TagForm) || hasTag(page, TagList) ||
			hasTag(page, TagPricing) || hasTag(page, TagContact) ||
			hasTag(page, TagAuth) || hasTag(page, TagService) ||
			hasTag(page, TagCaseStudy) {
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

// findFirst walks the outbound links of `from` and returns the first
// linked page whose tags satisfy `pred`. Returns nil if none match.
func findFirst(m *Map, from *Page, pred func(*Page) bool) *Page {
	for _, l := range from.Links {
		abs := absoluteSameOrigin(m.Origin, from.URL, l.Aria)
		if abs == "" {
			continue
		}
		candidate, ok := m.Pages[abs]
		if !ok {
			continue
		}
		if candidate.URL == from.URL {
			continue
		}
		if pred(candidate) {
			return candidate
		}
	}
	return nil
}

func relativePath(absURL string) string {
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
