package mindmap

import (
	"net/url"
	"strings"
)

// Page-shape tags. A single page can carry multiple tags.
const (
	TagLanding = "landing" // homepage / brochure-style entry
	TagForm    = "form"    // page with a real submit-form (≥1 required input)
	TagList    = "list"    // page with many same-shape outbound links (blog, products)
	TagDetail  = "detail"  // long content page (case study, blog post, article)
)

// tagPage applies heuristics. Multiple tags are allowed.
func tagPage(p *Page) []string {
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
		if a.Tag == "submit" {
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
		// Take the first path segment after the leading slash.
		segs := strings.SplitN(strings.TrimPrefix(href, "/"), "/", 2)
		if len(segs) < 2 || segs[1] == "" {
			// /foo with no second segment isn't a list member; skip.
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

// isDetailPage flags a page that looks like a single piece of content:
// has an h1 OR ≥2 h2 headings, and few outbound nav-style links.
func isDetailPage(p *Page) bool {
	headingCount := 0
	for _, c := range p.Contents {
		if c.Tag == "h1" || c.Tag == "h2" {
			headingCount++
		}
	}
	return headingCount >= 1 && len(p.Links) < 20
}
