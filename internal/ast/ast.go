// Package ast defines a uniform Symbol shape and an Extractor registry
// keyed by file extension. Per-language extractors live in subpackages.
//
// Implementation note: v1 uses regex/heuristic extractors so the binary is
// pure Go and trivially cross-compiles. The interface is designed so each
// extractor can be swapped for a tree-sitter implementation later without
// touching plan/, gen/, or heal/.
package ast

import (
	"path/filepath"
	"strings"
)

type Kind string

const (
	KindFunction  Kind = "function"
	KindMethod    Kind = "method"
	KindRoute     Kind = "route"     // HTTP route handler
	KindComponent Kind = "component" // UI component (TSX/Vue/Svelte)
)

type Param struct{ Name, Type string }

type Symbol struct {
	Kind     Kind
	Name     string
	Receiver string
	Params   []Param
	Returns  string
	File     string
	Language string
	Line     int // 1-based start line in NEW file
	EndLine  int
	// FrameworkHint is a free-form tag the emitter consults
	// (e.g. "express", "fastify", "fastapi", "flask", "spring", "nethttp").
	FrameworkHint string
	// HTTP-shaped data when Kind == KindRoute.
	Method string
	Path   string
	// Return-shape hints used by the Go scaffold template. Only the golang
	// extractor populates these today; other languages may leave them zero.
	HasError      bool
	HasResult     bool
	PrimaryReturn string
	// Component-shaped signals. Populated by the TS extractor for KindComponent
	// symbols; drive multi-scenario Playwright scaffolds.
	Anchors     []LocatorAnchor
	HasState    bool
	HasOnClick  bool
	HasOnSubmit bool
	// User-flow signals. Populated by the TS extractor (per-component) and the
	// HTML page extractor (per-page); drive fill+submit and navigation
	// scenarios in the Playwright templates.
	Inputs      []FormInput
	Links       []LocatorAnchor // Aria field reused to carry href / to= target
	Contents    []ContentAnchor // page-level text fallbacks (h1, title, CTA labels)
	PageTitle   string          // <title> contents — populated by HTML probe path
	HasForm     bool
	HasNavigate bool
	// Interactions are in-page interactive components detected by the HTML
	// extractor (search boxes, accordions, dialogs, tabs, …). The exercise
	// journey emits one Playwright test block per Interaction.
	Interactions []Interaction
	// EnteredVia is the href the journey clicked to reach this page. Empty
	// for the first symbol in a chain (the page is visited via direct goto).
	EnteredVia string
	// DirectGoto is true when EnteredVia could not be located as a clickable
	// link on the previous step's page (e.g. sitemap-discovered URLs not
	// linked from the landing). The template uses `page.goto(<abs URL>)`
	// in that case instead of `locator(...).click()`.
	DirectGoto bool
	// AbsoluteURL is the full URL of this step's page. Used by the template
	// when DirectGoto is true.
	AbsoluteURL string
}

// ContentAnchor describes a page-level text anchor — the <title>, an <h1>,
// or a high-signal CTA label. Used as a visibility fallback when the page
// carries no data-testid / aria-label / role attributes.
type ContentAnchor struct {
	Tag  string // "title" | "h1" | "h2" | "cta"
	Text string // verbatim text content (trimmed)
}

// Interaction describes an in-page interactive component detected by the
// HTML extractor. Drives the exercise journey's test emissions: click,
// fill, expand, dismiss, etc.
type Interaction struct {
	// Kind classifies the shape of interaction:
	//   "search"          — search input
	//   "details"         — native <details>/<summary>
	//   "collapse"        — button with aria-expanded + aria-controls
	//   "dialog"          — <dialog> element
	//   "tab"             — role=tab paired with a role=tabpanel
	//   "date"            — input type=date|time|datetime-local
	//   "data-toggle"     — Bootstrap-style data-toggle attribute
	//   "popup"           — button with aria-haspopup
	Kind string
	// Toggle ("expand" | "modal" | "dropdown" | "collapse" | "tab" | "offcanvas" | "popover"):
	// the Bootstrap toggle subtype, present only when Kind == "data-toggle".
	Toggle string
	// TestID, Aria, Role: preferred locator hints (highest stability first).
	TestID string
	Aria   string
	Role   string
	// Text is the visible interactive text (summary text, tab label, button
	// label). Used as the accessible-name fallback.
	Text string
	// Controls is the value of aria-controls (target element id) — empty
	// when not present.
	Controls string
	// InputType for the date/search interactions (e.g. "date", "time",
	// "datetime-local", "search").
	InputType string
	// Name is the input's name attribute, populated for search/date.
	Name string
	// File / Line for diagnostic provenance.
	File string
	Line int
}

// FormInput describes a single form field detected in a component or page.
// Drives deterministic fill values in the Playwright happy-flow template.
type FormInput struct {
	Name        string // <input name="...">
	Type        string // text | email | password | number | checkbox | radio | tel | url | date | select | textarea
	Required    bool
	TestID      string // when present, preferred locator
	Aria        string // aria-label, when present
	Placeholder string // placeholder text, when present
	LabelText   string // associated <label> text (via for=id match)
	Tag         string // "input" | "select" | "textarea"
	File        string
	Line        int
}

type LocatorAnchor struct {
	Role   string
	Name   string
	TestID string
	Aria   string
	Text   string
	CSS    string
	File   string
	Line   int
	// Tag is the lowercased HTML element the anchor sits on (e.g. "button",
	// "summary", "form"). Used by templates to pick clickable shapes.
	Tag string
}

type Extractor interface {
	Language() string
	Extract(file string, content []byte) ([]Symbol, []LocatorAnchor)
}

var registry = map[string]Extractor{}

func Register(exts []string, e Extractor) {
	for _, x := range exts {
		registry[strings.ToLower(x)] = e
	}
}

func ForFile(path string) Extractor {
	return registry[strings.ToLower(filepath.Ext(path))]
}

func Registered() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}
