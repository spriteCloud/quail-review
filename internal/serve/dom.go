package serve

import (
	"context"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"github.com/reviewqa/reviewqa/internal/probe"
)

// DOMLandmarks is the JSON-friendly shape returned by /api/probe-dom.
// Each slice is ordered by document position so the UI can highlight
// the first hit of any candidate.
type DOMLandmarks struct {
	URL      string         `json:"url"`
	Title    string         `json:"title,omitempty"`
	Headings []DOMHeading   `json:"headings,omitempty"`
	Forms    []DOMForm      `json:"forms,omitempty"`
	Buttons  []DOMButton    `json:"buttons,omitempty"`
	Links    []DOMLink      `json:"links,omitempty"`
	Inputs   []DOMInput     `json:"inputs,omitempty"`
}

type DOMHeading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}

type DOMForm struct {
	Name   string     `json:"name,omitempty"`
	Action string     `json:"action,omitempty"`
	Inputs []DOMInput `json:"inputs,omitempty"`
}

type DOMInput struct {
	Name        string `json:"name,omitempty"`
	Type        string `json:"type,omitempty"`
	Label       string `json:"label,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Required    bool   `json:"required,omitempty"`
	AriaLabel   string `json:"ariaLabel,omitempty"`
	ID          string `json:"id,omitempty"`
}

type DOMButton struct {
	Text      string `json:"text"`
	Type      string `json:"type,omitempty"`
	AriaLabel string `json:"ariaLabel,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
}

type DOMLink struct {
	Text string `json:"text"`
	Href string `json:"href"`
}

// FetchAndParseDOM downloads `target` via probe.Fetch (inheriting its
// SSRF guards) and extracts the landmarks the UI needs to surface
// locator candidates.
func FetchAndParseDOM(ctx context.Context, target string) (*DOMLandmarks, error) {
	res, err := probe.Fetch(ctx, target)
	if err != nil {
		return nil, err
	}
	root, err := html.Parse(strings.NewReader(string(res.Body)))
	if err != nil {
		return nil, err
	}
	lm := &DOMLandmarks{URL: res.URL}
	idToLabel := collectLabels(root)
	walkDOM(root, lm, idToLabel, nil)
	return lm, nil
}

func collectLabels(n *html.Node) map[string]string {
	out := map[string]string{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "label" {
			for _, a := range n.Attr {
				if a.Key == "for" {
					out[a.Val] = textOf(n)
					break
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}

// walkDOM populates lm in document order. currentForm captures the
// enclosing form (if any) so inputs append to the correct group.
func walkDOM(n *html.Node, lm *DOMLandmarks, labels map[string]string, currentForm *DOMForm) {
	if n == nil {
		return
	}
	switch {
	case n.Type == html.ElementNode && n.Data == "title":
		if lm.Title == "" {
			lm.Title = strings.TrimSpace(textOf(n))
		}
	case n.Type == html.ElementNode && (n.Data == "h1" || n.Data == "h2" || n.Data == "h3" || n.Data == "h4" || n.Data == "h5" || n.Data == "h6"):
		t := strings.TrimSpace(textOf(n))
		if t != "" {
			lm.Headings = append(lm.Headings, DOMHeading{Level: int(n.Data[1] - '0'), Text: t})
		}
	case n.Type == html.ElementNode && n.Data == "form":
		f := &DOMForm{Name: attrOf(n, "name"), Action: attrOf(n, "action")}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkDOM(c, lm, labels, f)
		}
		lm.Forms = append(lm.Forms, *f)
		return
	case n.Type == html.ElementNode && (n.Data == "input" || n.Data == "textarea" || n.Data == "select"):
		input := DOMInput{
			Name:        attrOf(n, "name"),
			Type:        attrOf(n, "type"),
			Placeholder: attrOf(n, "placeholder"),
			Required:    hasAttr(n, "required"),
			AriaLabel:   attrOf(n, "aria-label"),
			ID:          attrOf(n, "id"),
		}
		if input.ID != "" {
			if l, ok := labels[input.ID]; ok {
				input.Label = strings.TrimSpace(l)
			}
		}
		if !shouldSkipInput(input) {
			lm.Inputs = append(lm.Inputs, input)
			if currentForm != nil {
				currentForm.Inputs = append(currentForm.Inputs, input)
			}
		}
	case n.Type == html.ElementNode && n.Data == "button":
		b := DOMButton{
			Text:      strings.TrimSpace(textOf(n)),
			Type:      attrOf(n, "type"),
			AriaLabel: attrOf(n, "aria-label"),
			Disabled:  hasAttr(n, "disabled"),
		}
		if b.Text != "" || b.AriaLabel != "" {
			lm.Buttons = append(lm.Buttons, b)
		}
	case n.Type == html.ElementNode && n.Data == "a":
		href := attrOf(n, "href")
		text := strings.TrimSpace(textOf(n))
		if href != "" && text != "" {
			lm.Links = append(lm.Links, DOMLink{Text: text, Href: href})
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkDOM(c, lm, labels, currentForm)
	}
}

func shouldSkipInput(in DOMInput) bool {
	t := strings.ToLower(in.Type)
	return t == "hidden" || t == "submit" || t == "button" || t == "image"
}

func textOf(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(textOf(c))
	}
	return sb.String()
}

func attrOf(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

// LocatorCandidate is one suggestion for a step's locator. selector is
// the Playwright code to copy; rank is the similarity score.
type LocatorCandidate struct {
	Selector string  `json:"selector"`
	Role     string  `json:"role"`
	Name     string  `json:"name"`
	Score    float64 `json:"score"`
	Source   string  `json:"source,omitempty"` // "form-input" | "button" | "link" | "heading"
}

// RankLocators produces a ranked list of Playwright selectors against
// the given landmarks. kind narrows the candidate pool ("button" /
// "input" / "link" / "heading"); hint is the text the user wants to
// match.
func RankLocators(lm *DOMLandmarks, kind, hint string) []LocatorCandidate {
	hint = strings.ToLower(strings.TrimSpace(hint))
	var out []LocatorCandidate
	switch kind {
	case "button":
		for _, b := range lm.Buttons {
			name := preferText(b.Text, b.AriaLabel)
			sc := similarity(hint, strings.ToLower(name))
			out = append(out, LocatorCandidate{
				Selector: `page.getByRole('button', { name: '` + escapeSingleQuote(name) + `' })`,
				Role:     "button",
				Name:     name,
				Score:    sc,
				Source:   "button",
			})
		}
	case "input", "field":
		for _, in := range lm.Inputs {
			label := preferText(in.Label, in.AriaLabel, in.Placeholder, in.Name)
			sc := similarity(hint, strings.ToLower(label))
			selector := selectorForInput(in)
			out = append(out, LocatorCandidate{
				Selector: selector,
				Role:     "textbox",
				Name:     label,
				Score:    sc,
				Source:   "form-input",
			})
		}
	case "link":
		for _, l := range lm.Links {
			sc := similarity(hint, strings.ToLower(l.Text))
			// If the hint looks like a path, prefer href match.
			if strings.HasPrefix(hint, "/") || strings.Contains(hint, "://") {
				if l.Href == hint || strings.HasSuffix(l.Href, hint) {
					sc = 1
				}
			}
			out = append(out, LocatorCandidate{
				Selector: `page.getByRole('link', { name: '` + escapeSingleQuote(l.Text) + `' })`,
				Role:     "link",
				Name:     l.Text,
				Score:    sc,
				Source:   "link",
			})
			// Also offer the href-based selector — useful when the link
			// text is generic ("Read more").
			out = append(out, LocatorCandidate{
				Selector: `page.locator('a[href="` + escapeDoubleQuote(l.Href) + `"]')`,
				Role:     "link",
				Name:     l.Href,
				Score:    sc * 0.9,
				Source:   "link",
			})
		}
	case "heading":
		for _, h := range lm.Headings {
			sc := similarity(hint, strings.ToLower(h.Text))
			out = append(out, LocatorCandidate{
				Selector: `page.getByRole('heading', { level: ` + intToString(h.Level) + `, name: '` + escapeSingleQuote(h.Text) + `' })`,
				Role:     "heading",
				Name:     h.Text,
				Score:    sc,
				Source:   "heading",
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	// Cap the list at 10 — past that the suggestions add noise.
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

func selectorForInput(in DOMInput) string {
	switch {
	case in.Label != "":
		return `page.getByLabel('` + escapeSingleQuote(in.Label) + `')`
	case in.Placeholder != "":
		return `page.getByPlaceholder('` + escapeSingleQuote(in.Placeholder) + `')`
	case in.AriaLabel != "":
		return `page.getByLabel('` + escapeSingleQuote(in.AriaLabel) + `')`
	case in.Name != "":
		return `page.locator('[name="` + escapeDoubleQuote(in.Name) + `"]')`
	case in.ID != "":
		return `page.locator('#` + in.ID + `')`
	}
	return `page.locator('input')`
}

func similarity(hint, candidate string) float64 {
	if hint == "" || candidate == "" {
		return 0
	}
	if hint == candidate {
		return 1
	}
	if strings.Contains(candidate, hint) {
		return 0.85
	}
	if strings.Contains(hint, candidate) {
		return 0.75
	}
	// Token-overlap fallback: how many tokens of hint also appear in
	// candidate? Helps "Sign up" vs "Sign Up Now".
	hintTokens := strings.Fields(hint)
	if len(hintTokens) == 0 {
		return 0
	}
	hit := 0
	for _, t := range hintTokens {
		if strings.Contains(candidate, t) {
			hit++
		}
	}
	if hit == 0 {
		return 0
	}
	return float64(hit) / float64(len(hintTokens)) * 0.5
}

func preferText(opts ...string) string {
	for _, s := range opts {
		if t := strings.TrimSpace(s); t != "" {
			return t
		}
	}
	return ""
}

func escapeSingleQuote(s string) string { return strings.ReplaceAll(s, "'", `\'`) }
func escapeDoubleQuote(s string) string { return strings.ReplaceAll(s, `"`, `\"`) }
func intToString(n int) string {
	if n < 0 || n > 9 {
		return "1"
	}
	return string(rune('0' + n))
}

// resolveTarget interprets a user-supplied URL relative to a base. The
// UI passes either a full URL or a path like "/about" when the
// Scenario uses placeholders; this helper picks the right one.
func resolveTarget(target, base string) (string, error) {
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return target, nil
	}
	if base == "" {
		return target, nil
	}
	bu, err := url.Parse(base)
	if err != nil {
		return target, nil
	}
	resolved, err := bu.Parse(target)
	if err != nil {
		return target, err
	}
	return resolved.String(), nil
}
