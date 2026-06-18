package mindmap

import (
	"net/url"
	"strings"
)

// Page-shape tags. A single page can carry multiple tags.
const (
	TagLanding     = "landing"     // homepage / brochure-style entry
	TagForm        = "form"        // page with a real submit-form (≥1 required input)
	TagList        = "list"        // page with many same-shape outbound links (blog, products)
	TagDetail      = "detail"      // long content page (case study, blog post, article)
	TagPricing     = "pricing"     // pricing / plans page with price markers
	TagContact     = "contact"     // contact-shaped form (name + email + message)
	TagAuth        = "auth"        // login / signup with password field
	TagService     = "service"     // product / service / solution / feature page
	TagCaseStudy   = "case-study"  // customer success / case study entry
	TagInteractive = "interactive" // ≥1 in-page interactive component detected
)

// TagsFromPage is the public entry point for tagging a page whose other
// fields are already populated (used by the browser-driven probe, which
// builds pages from rendered DOM JSON instead of raw HTML). Returns the
// same shape as the internal tagPage path, just without the HTML
// fallback for body-marker pricing detection.
func TagsFromPage(p *Page) []string {
	return tagPage(p, nil)
}

// tagPage applies heuristics. Multiple tags are allowed. The raw HTML is
// only used by markers that need body text (e.g. pricing detection); pass
// nil to skip body-based tags.
func tagPage(p *Page, html []byte) []string {
	var tags []string
	if isLandingURL(p.URL) {
		tags = append(tags, TagLanding)
	}
	if isFormPage(p) {
		tags = append(tags, TagForm)
	}
	if isListPage(p) {
		tags = append(tags, TagList)
	}
	if isDetailPage(p) {
		tags = append(tags, TagDetail)
	}
	if isPricingPage(p, html) {
		tags = append(tags, TagPricing)
	}
	if isContactPage(p) {
		tags = append(tags, TagContact)
	}
	if isAuthPage(p) {
		tags = append(tags, TagAuth)
	}
	if isServicePage(p) {
		tags = append(tags, TagService)
	}
	if isCaseStudyPage(p) {
		tags = append(tags, TagCaseStudy)
	}
	if len(p.Interactions) > 0 {
		tags = append(tags, TagInteractive)
	}
	return tags
}

func isLandingURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	p := strings.TrimSuffix(u.Path, "/")
	return p == "" || p == "/index" || p == "/home"
}

func isFormPage(p *Page) bool {
	if !p.HasForm {
		return false
	}
	hasRequired := false
	for _, i := range p.Inputs {
		if i.Required {
			hasRequired = true
			break
		}
	}
	hasSubmit := false
	for _, a := range p.Anchors {
		// v0.91: role-tagged submits count too. <div role="button">
		// becomes an Anchor with Role="button"; combined with
		// HasForm + ≥1 required input, that's a legitimate submit
		// signal even when the literal HTML tag is a div.
		if a.Tag == "submit" || a.Role == "button" || a.Role == "submit" {
			hasSubmit = true
			break
		}
	}
	return hasRequired && hasSubmit
}

// isListPage flags a page whose outbound same-origin links form an obvious
// list — e.g. /blog has 20 links to /blog/* posts. Heuristic: ≥6 same-origin
// links sharing a common path prefix.
func isListPage(p *Page) bool {
	if len(p.Links) < 6 {
		return false
	}
	prefixCounts := map[string]int{}
	for _, l := range p.Links {
		href := l.Aria
		if !strings.HasPrefix(href, "/") || strings.HasPrefix(href, "//") {
			continue
		}
		segs := strings.SplitN(strings.TrimPrefix(href, "/"), "/", 2)
		if len(segs) < 2 || segs[1] == "" {
			continue
		}
		prefix := "/" + segs[0] + "/"
		prefixCounts[prefix]++
	}
	for _, n := range prefixCounts {
		if n >= 4 {
			return true
		}
	}
	return false
}

// isDetailPage flags a page that looks like a single piece of content.
// Two acceptance branches:
//   - Short content path: ≥1 heading AND <20 outbound links — short
//     posts, marketing detail pages, simple landing-style content.
//   - Article path: ≥1 h1 AND ≥3 h2 — long-form structured articles
//     (wiki entries, in-depth guides) that often carry hundreds of
//     cross-links and would fail the <20 cap.
func isDetailPage(p *Page) bool {
	h1Count, h2Count := 0, 0
	for _, c := range p.Contents {
		switch c.Tag {
		case "h1":
			h1Count++
		case "h2":
			h2Count++
		}
	}
	headingCount := h1Count + h2Count
	if headingCount >= 1 && len(p.Links) < 20 {
		return true
	}
	if h1Count >= 1 && h2Count >= 3 {
		return true
	}
	return false
}

func pathLower(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Path)
}

// isPricingPage: URL contains pricing/plans/tariff/subscription. Body
// markers (currency near digit) were too noisy on legal pages — URL
// shape is the reliable signal.
func isPricingPage(p *Page, _ []byte) bool {
	path := pathLower(p.URL)
	for _, kw := range []string{"pricing", "plans", "tariff", "subscription"} {
		if strings.Contains(path, kw) {
			return true
		}
	}
	return false
}

// isContactPage: URL suggests contact AND page hosts a form. The form
// being present is the real signal — a "Contact" landing page without a
// form is just a detail/info page.
func isContactPage(p *Page) bool {
	if !p.HasForm {
		return false
	}
	path := pathLower(p.URL)
	for _, kw := range []string{"contact", "reach-us", "talk-to-us", "get-in-touch"} {
		if strings.Contains(path, kw) {
			return true
		}
	}
	return false
}

// isAuthPage: login / signup / register URL AND a password input.
func isAuthPage(p *Page) bool {
	hasPassword := false
	for _, i := range p.Inputs {
		if strings.EqualFold(i.Type, "password") {
			hasPassword = true
			break
		}
	}
	if !hasPassword {
		return false
	}
	path := pathLower(p.URL)
	for _, kw := range []string{"login", "sign-in", "signin", "signup", "sign-up", "register"} {
		if strings.Contains(path, kw) {
			return true
		}
	}
	return false
}

// isServicePage: first path segment is service/solution/product/feature
// (singular or plural). Avoids matching /blog/services-overview-2024.
func isServicePage(p *Page) bool {
	u, err := url.Parse(p.URL)
	if err != nil {
		return false
	}
	parts := strings.Split(strings.Trim(strings.ToLower(u.Path), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return false
	}
	switch parts[0] {
	case "service", "services", "solution", "solutions", "product", "products", "feature", "features":
		return true
	}
	return false
}

// isCaseStudyPage: URL contains case-stud / customer-story / stories AND
// the page already looks like content (has an h1).
func isCaseStudyPage(p *Page) bool {
	path := pathLower(p.URL)
	matched := false
	for _, kw := range []string{"case-stud", "customer-story", "customer-stories", "/stories"} {
		if strings.Contains(path, kw) {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	for _, c := range p.Contents {
		if c.Tag == "h1" {
			return true
		}
	}
	return false
}
