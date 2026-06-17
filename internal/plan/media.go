package plan

import (
	"regexp"
	"strings"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan/patterns"
)

// reImgOpen matches an <img …> tag. Multiline attribute lists are fine.
var reImgOpen = regexp.MustCompile(`<\s*img\b([^>]*)>`)

// reImgAlt captures the alt attribute. Empty-string alts (alt="") are
// allowed by HTML for decorative images — we treat them as "no signal"
// and drop them downstream.
var reImgAlt = regexp.MustCompile(`alt\s*=\s*['"]([^'"]*)['"]`)

// reImgSrc captures the src attribute (or data-src for lazy-loaded
// images on some marketing frameworks).
var (
	reImgSrc     = regexp.MustCompile(`\bsrc\s*=\s*['"]([^'"]+)['"]`)
	reImgDataSrc = regexp.MustCompile(`\bdata-src\s*=\s*['"]([^'"]+)['"]`)
)

// reMetaTag captures a single <meta name|property="X" content="Y"> in
// either order of attributes.
var (
	reMetaNameFirst     = regexp.MustCompile(`<meta\b[^>]*\bname\s*=\s*['"]([^'"]+)['"][^>]*\bcontent\s*=\s*['"]([^'"]+)['"]`)
	reMetaPropertyFirst = regexp.MustCompile(`<meta\b[^>]*\bproperty\s*=\s*['"]([^'"]+)['"][^>]*\bcontent\s*=\s*['"]([^'"]+)['"]`)
	reMetaContentFirst  = regexp.MustCompile(`<meta\b[^>]*\bcontent\s*=\s*['"]([^'"]+)['"][^>]*\b(?:name|property)\s*=\s*['"]([^'"]+)['"]`)
)

// reLinkCanonical captures <link rel="canonical" href="...">.
var reLinkCanonical = regexp.MustCompile(`(?i)<link\b[^>]*\brel\s*=\s*['"]canonical['"][^>]*\bhref\s*=\s*['"]([^'"]+)['"]`)

// ExtractImages returns the page's <img> tags carrying non-empty alt
// text, capped at 8. Decorative images (alt="") and images without an
// alt attribute at all are dropped — they carry no testable semantic
// signal and emitting locators for them would only be locator noise.
func ExtractImages(file string, content []byte) []ast.ImageRef {
	const cap = 8
	var out []ast.ImageRef
	str := string(content)
	for _, m := range reImgOpen.FindAllStringSubmatchIndex(str, -1) {
		if len(out) >= cap {
			break
		}
		attrs := str[m[2]:m[3]]
		// Pattern registry: drop aria-hidden / sr-only / Bootstrap-hidden
		// images — they're in DOM but never visible. Asserting alt-text
		// visibility on those produces false negatives.
		if patterns.Decide(patterns.Context{Tag: "img", Attrs: attrs}) == patterns.ActionDrop {
			continue
		}
		altMatch := reImgAlt.FindStringSubmatch(attrs)
		if altMatch == nil {
			continue
		}
		alt := strings.TrimSpace(altMatch[1])
		if alt == "" {
			continue
		}
		ref := ast.ImageRef{
			Alt:  alt,
			File: file,
			Line: strings.Count(str[:m[0]], "\n") + 1,
		}
		if sm := reImgSrc.FindStringSubmatch(attrs); sm != nil {
			ref.Src = sm[1]
		} else if sm := reImgDataSrc.FindStringSubmatch(attrs); sm != nil {
			ref.Src = sm[1]
		}
		out = append(out, ref)
	}
	return out
}

// ExtractMetaTags collects head metadata into a MetaTags bag. Order of
// `name=` vs `property=` vs `content=` attributes in the tag doesn't
// matter. Missing fields stay as zero strings.
func ExtractMetaTags(content []byte) ast.MetaTags {
	var meta ast.MetaTags
	set := func(key, val string) {
		key = strings.ToLower(key)
		val = strings.TrimSpace(val)
		if val == "" {
			return
		}
		switch key {
		case "description":
			if meta.Description == "" {
				meta.Description = val
			}
		case "viewport":
			if meta.ViewportContent == "" {
				meta.ViewportContent = val
			}
		case "og:title":
			if meta.OGTitle == "" {
				meta.OGTitle = val
			}
		case "og:type":
			if meta.OGType == "" {
				meta.OGType = val
			}
		case "og:description":
			if meta.OGDescription == "" {
				meta.OGDescription = val
			}
		}
	}
	str := string(content)
	// name="X" content="Y"
	for _, m := range reMetaNameFirst.FindAllStringSubmatch(str, -1) {
		set(m[1], m[2])
	}
	// property="X" content="Y"
	for _, m := range reMetaPropertyFirst.FindAllStringSubmatch(str, -1) {
		set(m[1], m[2])
	}
	// content="Y" name|property="X" (reverse order)
	for _, m := range reMetaContentFirst.FindAllStringSubmatch(str, -1) {
		set(m[2], m[1])
	}
	if m := reLinkCanonical.FindStringSubmatch(str); m != nil {
		meta.Canonical = strings.TrimSpace(m[1])
	}
	return meta
}
