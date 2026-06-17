package patterns

import (
	"regexp"
	"strings"
)

// Compiled regexes shared by hidden-class patterns. Defined at package
// scope so they're built once.
var (
	// reAriaLabelSkipLink matches the skip-to-content / skip-nav
	// vocabulary that's standard a11y practice but always hidden until
	// tab-focus.
	reAriaLabelSkipLink = regexp.MustCompile(
		`(?i)aria-label\s*=\s*['"]\s*skip\s*(to|nav|content|main|navigation|sidebar|search|menu)\b`)
	// reClassSrOnly catches the common screen-reader-only class
	// conventions. Bootstrap also defines visually-hidden under d-* utils
	// which the bootstrap_hidden pattern covers separately.
	reClassSrOnly = regexp.MustCompile(
		`(?i)class\s*=\s*['"][^'"]*\b(sr-only|visually-hidden|screen-reader-text|screenreader-only|vh-[a-z0-9-]+|is-sr-only|offscreen)\b`)
	// reClassBootstrapHidden catches Bootstrap visibility utilities
	// (d-none, hidden-md-up, visually-hidden-*) plus the "visually-hidden"
	// Bootstrap 5 utility.
	reClassBootstrapHidden = regexp.MustCompile(
		`(?i)class\s*=\s*['"][^'"]*\b(d-none|d-[a-z]+-none|hidden-(?:xs|sm|md|lg|xl|xxl)(?:-(?:up|down))?|visually-hidden(?:-[a-z0-9-]+)?)\b`)
	// reInlineHidden catches `style="display:none"`, `visibility:hidden`,
	// or the off-screen positioning trick (`left: -9999px`).
	reInlineHidden = regexp.MustCompile(
		`(?i)style\s*=\s*['"][^'"]*(display\s*:\s*none|visibility\s*:\s*hidden|left\s*:\s*-\d{4,}(?:px|em)|top\s*:\s*-\d{4,}(?:px|em))`)
	// rePrintOnly catches Bootstrap print-only utilities NOT paired with
	// a screen-block class. d-print-block d-none is "print only".
	rePrintOnly = regexp.MustCompile(
		`(?i)class\s*=\s*['"][^'"]*\bd-print-(?:block|inline|flex|grid)\b[^'"]*(?:d-none|hidden)\b`)
	// reTrackerNoScript matches noscript blocks containing well-known
	// tracker iframes / pixels — GTM, FB pixel, Hotjar.
	reTrackerNoScript = regexp.MustCompile(
		`(?i)(googletagmanager\.com|connect\.facebook\.net|static\.hotjar\.com|fbq\(\s*['"]track['"]\s*,)`)
)

// patternFunc lets us define patterns inline without a per-pattern struct
// for the simple cases.
type patternFunc struct {
	name   string
	class  Class
	action Action
	match  func(Context) bool
}

func (p patternFunc) Name() string         { return p.name }
func (p patternFunc) Class() Class         { return p.class }
func (p patternFunc) Action() Action       { return p.action }
func (p patternFunc) Matches(c Context) bool {
	return p.match(c)
}

func init() {
	// 1. a11y hidden-by-design: aria-hidden, hidden attribute, skip-link
	//    aria-label vocabulary.
	Register(patternFunc{
		name:   "a11y_hidden",
		class:  ClassHidden,
		action: ActionDrop,
		match: func(c Context) bool {
			a := c.Attrs
			if strings.Contains(a, `aria-hidden="true"`) || strings.Contains(a, `aria-hidden='true'`) {
				return true
			}
			// Bare `hidden` attribute (must be a word boundary so we don't
			// match `data-hidden-state` etc).
			if reBareAttr("hidden").MatchString(a) {
				return true
			}
			return reAriaLabelSkipLink.MatchString(a)
		},
	})

	// 2. screen-reader-only / visually-hidden conventions.
	Register(patternFunc{
		name:   "sr_only",
		class:  ClassHidden,
		action: ActionDrop,
		match:  func(c Context) bool { return reClassSrOnly.MatchString(c.Attrs) },
	})

	// 3. Bootstrap visibility utilities. Checked AFTER sr_only so the
	//    overlap (Bootstrap 5's `visually-hidden`) is attributed to the
	//    most-specific pattern, but either way the action is the same.
	Register(patternFunc{
		name:   "bootstrap_hidden",
		class:  ClassHidden,
		action: ActionDrop,
		match:  func(c Context) bool { return reClassBootstrapHidden.MatchString(c.Attrs) },
	})

	// 4. Inline-style hidden / off-screen positioning.
	Register(patternFunc{
		name:   "inline_hidden",
		class:  ClassHidden,
		action: ActionDrop,
		match:  func(c Context) bool { return reInlineHidden.MatchString(c.Attrs) },
	})

	// 5. Hidden form fields — type="hidden" inputs should not appear in
	//    the form-fill loop.
	Register(patternFunc{
		name:   "form_hidden",
		class:  ClassHidden,
		action: ActionDrop,
		match: func(c Context) bool {
			if c.Tag != "input" {
				return false
			}
			return strings.Contains(strings.ToLower(c.Attrs), `type="hidden"`) ||
				strings.Contains(strings.ToLower(c.Attrs), `type='hidden'`)
		},
	})

	// 6. Print-only content.
	Register(patternFunc{
		name:   "print_only",
		class:  ClassHidden,
		action: ActionDrop,
		match:  func(c Context) bool { return rePrintOnly.MatchString(c.Attrs) },
	})

	// 7. Third-party tracker noscript blocks. Matches based on inner
	//    content because trackers ship as noscript wrappers around
	//    GTM/FB iframes.
	Register(patternFunc{
		name:   "tracker",
		class:  ClassTracker,
		action: ActionDrop,
		match: func(c Context) bool {
			if c.Tag == "noscript" && reTrackerNoScript.MatchString(c.Inner) {
				return true
			}
			// Also drop iframe/img/script with tracker URLs in attrs.
			return reTrackerNoScript.MatchString(c.Attrs)
		},
	})
}

// reBareAttr returns a cached regex matching the given attribute name as
// a bare word (no value), e.g. `<details open>`. Word-boundary anchors
// prevent matching attribute prefixes like `data-hidden-state`.
func reBareAttr(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(^|\s)` + regexp.QuoteMeta(name) + `(\s|>|=|$)`)
}
