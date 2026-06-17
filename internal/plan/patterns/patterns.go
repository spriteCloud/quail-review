// Package patterns is reviewqa's structural-pattern registry. Each
// Pattern describes one known shape that should influence extraction —
// "this element is hidden by design", "this is a tracker pixel", "this
// is a cookie banner", etc — and what the extractor should do about it
// (drop, downgrade, mark for soft-only assertion).
//
// Patterns are checked in registration order; the first one that
// matches wins. The default action when no pattern matches is to keep
// the element as-is.
//
// The registry is process-global and populated at package init via
// pattern impls registering themselves. To add a new pattern class,
// implement the Pattern interface in a sibling file and call
// Register(p) from an init() function.
package patterns

// Class taxonomises patterns so consumers can reason about families.
type Class string

const (
	ClassHidden    Class = "hidden"    // intentionally hidden by design (a11y / SEO)
	ClassTracker   Class = "tracker"   // analytics / pixel / GTM noscript content
	ClassBanner    Class = "banner"    // cookie / consent banners
	ClassFramework Class = "framework" // framework-specific markup (future)
	ClassSEO       Class = "seo"       // structured data / SEO conventions (future)
)

// Action is what the extractor should do when a pattern matches.
type Action int

const (
	// ActionInclude is the default — keep the element. Returned when no
	// pattern matches.
	ActionInclude Action = iota
	// ActionDrop drops the element entirely. Use for hidden-by-design
	// markup that should never appear in any spec.
	ActionDrop
	// ActionSoftAssert keeps the element but signals downstream that
	// any visibility assertion must be soft. Reserved for v0.17+;
	// today the extractors map ActionSoftAssert to ActionInclude.
	ActionSoftAssert
	// ActionDowngradeLocator keeps the element but flags it as
	// low-priority for nav-ranking. Reserved for v0.17+.
	ActionDowngradeLocator
)

// Context is the input each Pattern matcher inspects. Patterns should
// be conservative — match cheaply via attribute substring checks rather
// than parsing.
type Context struct {
	// Tag is the lowercased element tag name (`a`, `input`, `div`, …).
	// Empty when the caller doesn't know the tag.
	Tag string
	// Attrs is the raw attribute string captured between the tag name
	// and the closing `>`. Patterns use cheap Contains/regex matches
	// here.
	Attrs string
	// Inner is the body of the element when present (e.g. for content
	// blocks). Empty for void tags.
	Inner string
	// PageURL is the URL of the page the element lives on (when known).
	PageURL string
}

// Pattern is a registered match-and-act rule.
type Pattern interface {
	Name() string
	Class() Class
	Matches(c Context) bool
	Action() Action
}

var registry []Pattern

// Register adds a pattern to the global registry. Call from init().
// Patterns are evaluated in registration order; the first match wins.
func Register(p Pattern) { registry = append(registry, p) }

// All returns a copy of the current registry. Useful for diagnostics.
func All() []Pattern {
	out := make([]Pattern, len(registry))
	copy(out, registry)
	return out
}

// Decide consults the registry for the first pattern that matches the
// given context and returns its Action. Returns ActionInclude when
// nothing matches.
func Decide(c Context) Action {
	for _, p := range registry {
		if p.Matches(c) {
			return p.Action()
		}
	}
	return ActionInclude
}

// MatchingPattern returns the first matching pattern (or nil) — used by
// tests and diagnostics that need to know WHICH pattern fired.
func MatchingPattern(c Context) Pattern {
	for _, p := range registry {
		if p.Matches(c) {
			return p
		}
	}
	return nil
}
