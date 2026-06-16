package plan

import (
	"regexp"
	"strings"

	"github.com/reviewqa/reviewqa/internal/ast"
)

// Interaction-extractor regexes. Match the OPEN tag of each interactive
// shape so we can read its attributes; multiline tag content is fine.
var (
	reInputOpen          = regexp.MustCompile(`<\s*input\b([^>]*)>`)
	reDetailsOpen        = regexp.MustCompile(`<\s*details\b([^>]*)>([\s\S]*?)</\s*details\s*>`)
	reSummaryInside      = regexp.MustCompile(`<\s*summary\b[^>]*>([\s\S]*?)</\s*summary\s*>`)
	reDialogOpen         = regexp.MustCompile(`<\s*dialog\b([^>]*)>`)
	reButtonOrLikeOpen   = regexp.MustCompile(`<\s*(button|a)\b([^>]*)>([\s\S]*?)</\s*(?:button|a)\s*>`)
	reAriaExpanded       = regexp.MustCompile(`aria-expanded\s*=\s*['"]([^'"]+)['"]`)
	reAriaControls       = regexp.MustCompile(`aria-controls\s*=\s*['"]([^'"]+)['"]`)
	reAriaHasPopup       = regexp.MustCompile(`aria-haspopup\s*=\s*['"]([^'"]+)['"]`)
	reDataToggle         = regexp.MustCompile(`data(?:-bs)?-toggle\s*=\s*['"]([^'"]+)['"]`)
)

// ExtractHTMLInteractions returns the in-page interactive components found
// in the HTML. Per-page caps are applied so a page with 20 collapsibles
// doesn't produce a 60-line spec; ordering is by detection precision so
// the highest-confidence interactions appear first.
func ExtractHTMLInteractions(file string, content []byte) []ast.Interaction {
	str := string(content)
	var out []ast.Interaction

	// 1. Search input — highest precision, 1 max.
	if s, ok := findSearchInput(file, str); ok {
		out = append(out, s)
	}

	// 2. Native <dialog> — highest precision, 1 max.
	if d, ok := findDialog(file, str); ok {
		out = append(out, d)
	}

	// 3. Tabs — capped at 4 tab triggers (one click per tab).
	out = append(out, findTabs(file, str, 4)...)

	// 4. <details>/<summary> + aria-expanded triggers — up to 2.
	out = append(out, findCollapsibles(file, str, 2)...)

	// 5. Date input — 1 max.
	if d, ok := findDateInput(file, str); ok {
		out = append(out, d)
	}

	// 6. Bootstrap-style toggles — up to 2.
	out = append(out, findDataToggles(file, str, 2)...)

	// 7. aria-haspopup buttons — up to 2.
	out = append(out, findPopupTriggers(file, str, 2)...)

	return out
}

func findSearchInput(file, html string) (ast.Interaction, bool) {
	for _, m := range reInputOpen.FindAllStringSubmatchIndex(html, -1) {
		attrs := html[m[2]:m[3]]
		// type="search" wins outright.
		if im := rePageHTMLInputType.FindStringSubmatch(attrs); im != nil && strings.EqualFold(im[1], "search") {
			i := newInteraction("search", attrs, html[:m[0]])
			i.InputType = "search"
			i.File = file
			return i, true
		}
		// role="searchbox" or aria-label matching /search/i — softer match.
		if rm := rePageHTMLRole.FindStringSubmatch(attrs); rm != nil && strings.EqualFold(rm[1], "searchbox") {
			i := newInteraction("search", attrs, html[:m[0]])
			i.Role = "searchbox"
			i.File = file
			return i, true
		}
		if am := rePageHTMLAria.FindStringSubmatch(attrs); am != nil && strings.Contains(strings.ToLower(am[1]), "search") {
			i := newInteraction("search", attrs, html[:m[0]])
			i.Aria = am[1]
			i.File = file
			return i, true
		}
	}
	return ast.Interaction{}, false
}

func findDialog(file, html string) (ast.Interaction, bool) {
	m := reDialogOpen.FindStringSubmatchIndex(html)
	if m == nil {
		return ast.Interaction{}, false
	}
	attrs := html[m[2]:m[3]]
	i := newInteraction("dialog", attrs, html[:m[0]])
	i.File = file
	return i, true
}

func findTabs(file, html string, max int) []ast.Interaction {
	var out []ast.Interaction
	for _, m := range reButtonOrLikeOpen.FindAllStringSubmatchIndex(html, -1) {
		if len(out) >= max {
			break
		}
		attrs := html[m[4]:m[5]]
		inner := html[m[6]:m[7]]
		rm := rePageHTMLRole.FindStringSubmatch(attrs)
		if rm == nil || !strings.EqualFold(rm[1], "tab") {
			continue
		}
		i := newInteraction("tab", attrs, html[:m[0]])
		i.Role = "tab"
		i.Text = strings.TrimSpace(stripTags(inner))
		i.File = file
		out = append(out, i)
	}
	return out
}

func findCollapsibles(file, html string, max int) []ast.Interaction {
	var out []ast.Interaction
	// Native <details>/<summary>.
	for _, m := range reDetailsOpen.FindAllStringSubmatchIndex(html, -1) {
		if len(out) >= max {
			break
		}
		attrs := html[m[2]:m[3]]
		inner := html[m[4]:m[5]]
		i := newInteraction("details", attrs, html[:m[0]])
		if sm := reSummaryInside.FindStringSubmatch(inner); sm != nil {
			i.Text = strings.TrimSpace(stripTags(sm[1]))
		}
		i.File = file
		out = append(out, i)
	}
	// Buttons with aria-expanded + aria-controls — accordion triggers.
	for _, m := range reButtonOrLikeOpen.FindAllStringSubmatchIndex(html, -1) {
		if len(out) >= max {
			break
		}
		attrs := html[m[4]:m[5]]
		inner := html[m[6]:m[7]]
		ex := reAriaExpanded.FindStringSubmatch(attrs)
		ctl := reAriaControls.FindStringSubmatch(attrs)
		if ex == nil || ctl == nil {
			continue
		}
		i := newInteraction("collapse", attrs, html[:m[0]])
		i.Controls = ctl[1]
		i.Text = strings.TrimSpace(stripTags(inner))
		i.File = file
		out = append(out, i)
	}
	return out
}

func findDateInput(file, html string) (ast.Interaction, bool) {
	for _, m := range reInputOpen.FindAllStringSubmatchIndex(html, -1) {
		attrs := html[m[2]:m[3]]
		im := rePageHTMLInputType.FindStringSubmatch(attrs)
		if im == nil {
			continue
		}
		t := strings.ToLower(im[1])
		switch t {
		case "date", "time", "datetime-local":
			i := newInteraction("date", attrs, html[:m[0]])
			i.InputType = t
			if nm := rePageHTMLInputName.FindStringSubmatch(attrs); nm != nil {
				i.Name = nm[1]
			}
			i.File = file
			return i, true
		}
	}
	return ast.Interaction{}, false
}

func findDataToggles(file, html string, max int) []ast.Interaction {
	allowedToggles := map[string]bool{
		"collapse": true, "modal": true, "tab": true,
		"dropdown": true, "offcanvas": true, "popover": true,
	}
	var out []ast.Interaction
	for _, m := range reButtonOrLikeOpen.FindAllStringSubmatchIndex(html, -1) {
		if len(out) >= max {
			break
		}
		attrs := html[m[4]:m[5]]
		inner := html[m[6]:m[7]]
		dt := reDataToggle.FindStringSubmatch(attrs)
		if dt == nil {
			continue
		}
		val := strings.ToLower(dt[1])
		if !allowedToggles[val] {
			continue
		}
		i := newInteraction("data-toggle", attrs, html[:m[0]])
		i.Toggle = val
		i.Text = strings.TrimSpace(stripTags(inner))
		if ctl := reAriaControls.FindStringSubmatch(attrs); ctl != nil {
			i.Controls = ctl[1]
		}
		i.File = file
		out = append(out, i)
	}
	return out
}

func findPopupTriggers(file, html string, max int) []ast.Interaction {
	var out []ast.Interaction
	for _, m := range reButtonOrLikeOpen.FindAllStringSubmatchIndex(html, -1) {
		if len(out) >= max {
			break
		}
		attrs := html[m[4]:m[5]]
		inner := html[m[6]:m[7]]
		if reAriaHasPopup.FindStringSubmatch(attrs) == nil {
			continue
		}
		// Skip if already covered by collapse (aria-expanded + aria-controls).
		if reAriaExpanded.MatchString(attrs) && reAriaControls.MatchString(attrs) {
			continue
		}
		// Skip if it's a tab (already covered).
		if rm := rePageHTMLRole.FindStringSubmatch(attrs); rm != nil && strings.EqualFold(rm[1], "tab") {
			continue
		}
		i := newInteraction("popup", attrs, html[:m[0]])
		i.Text = strings.TrimSpace(stripTags(inner))
		i.File = file
		out = append(out, i)
	}
	return out
}

// newInteraction populates the common locator hint fields (TestID, Aria,
// Role) from a tag's attribute string. Line is computed by counting
// newlines in the prefix.
func newInteraction(kind, attrs, prefix string) ast.Interaction {
	i := ast.Interaction{Kind: kind, Line: strings.Count(prefix, "\n") + 1}
	if t := rePageHTMLTestID.FindStringSubmatch(attrs); t != nil {
		i.TestID = t[1]
	}
	if a := rePageHTMLAria.FindStringSubmatch(attrs); a != nil {
		i.Aria = a[1]
	}
	if r := rePageHTMLRole.FindStringSubmatch(attrs); r != nil {
		i.Role = r[1]
	}
	return i
}

// stripTags removes anything that looks like an HTML tag. Used to derive
// the visible text of a button/summary/tab without bringing in a full
// HTML parser.
func stripTags(s string) string {
	for {
		idx := strings.Index(s, "<")
		if idx < 0 {
			break
		}
		end := strings.Index(s[idx:], ">")
		if end < 0 {
			break
		}
		s = s[:idx] + s[idx+end+1:]
	}
	// Collapse whitespace.
	parts := strings.Fields(s)
	return strings.Join(parts, " ")
}
