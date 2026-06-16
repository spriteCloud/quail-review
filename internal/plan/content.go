package plan

import (
	"regexp"
	"strings"

	"github.com/reviewqa/reviewqa/internal/ast"
)

// reTitle matches the <title> element content. Single-line only.
var reTitle = regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)

// reH1 matches an <h1>...</h1> body (single line; multi-line headings
// fall through). Go's RE2 doesn't support backrefs, so h1 / h2 get
// separate patterns.
var (
	reH1 = regexp.MustCompile(`(?i)<h1\b[^>]*>([^<]+)</h1>`)
	reH2 = regexp.MustCompile(`(?i)<h2\b[^>]*>([^<]+)</h2>`)
)

// ctaVocabulary is the set of text fragments that signal a user-action CTA.
// Matches are case-insensitive substrings of the visible button / link text.
var ctaVocabulary = []string{
	"get started", "sign up", "signup", "sign in", "log in", "login",
	"book a demo", "request a demo", "talk to sales", "contact us",
	"download", "learn more", "read more", "subscribe", "submit",
	"buy now", "try free", "get offer", "get a quote",
}

// ExtractContentAnchors returns the page-level text anchors usable as
// visibility fallbacks when the page carries no data-testid / aria-label.
// Cap at 5 to keep specs short.
func ExtractContentAnchors(content []byte) []ast.ContentAnchor {
	const cap = 5
	var out []ast.ContentAnchor
	out = appendTitle(out, content)
	out = appendHeadings(out, content, cap)
	out = appendCTAs(out, content, cap)
	return out
}

func appendTitle(out []ast.ContentAnchor, content []byte) []ast.ContentAnchor {
	m := reTitle.FindStringSubmatch(string(content))
	if m == nil {
		return out
	}
	t := strings.TrimSpace(m[1])
	if t == "" {
		return out
	}
	return append(out, ast.ContentAnchor{Tag: "title", Text: t})
}

func appendHeadings(out []ast.ContentAnchor, content []byte, cap int) []ast.ContentAnchor {
	out = appendMatches(out, content, reH1, "h1", cap)
	if len(out) >= cap {
		return out
	}
	return appendMatches(out, content, reH2, "h2", cap)
}

func appendMatches(out []ast.ContentAnchor, content []byte, re *regexp.Regexp, tag string, cap int) []ast.ContentAnchor {
	for _, m := range re.FindAllStringSubmatch(string(content), -1) {
		if len(out) >= cap {
			return out
		}
		text := strings.TrimSpace(m[1])
		if text == "" {
			continue
		}
		out = append(out, ast.ContentAnchor{Tag: tag, Text: text})
	}
	return out
}

func appendCTAs(out []ast.ContentAnchor, content []byte, cap int) []ast.ContentAnchor {
	for _, re := range []*regexp.Regexp{reButtonText, reAnchorText} {
		for _, m := range re.FindAllSubmatch(content, -1) {
			if len(out) >= cap {
				return out
			}
			text := strings.TrimSpace(string(m[1]))
			if isCTAText(strings.ToLower(text)) {
				out = append(out, ast.ContentAnchor{Tag: "cta", Text: text})
			}
		}
	}
	return out
}

func isCTAText(lowerText string) bool {
	for _, v := range ctaVocabulary {
		if strings.Contains(lowerText, v) {
			return true
		}
	}
	return false
}

// PageTitle returns the <title> tag's content, or empty when absent.
func PageTitle(content []byte) string {
	if m := reTitle.FindSubmatch(content); m != nil {
		return strings.TrimSpace(string(m[1]))
	}
	return ""
}

// reAnchorText matches an <a ...>Text</a> body (single line).
var reAnchorText = regexp.MustCompile(`<\s*a\b[^>]*>([^<]+)</\s*a\s*>`)
