// Package gen renders deterministic test scaffolds from plan.Items using
// Go text/template. Templates are embedded into the binary so the CLI ships
// as a single artifact with no runtime asset path.
package gen

import (
	"bytes"
	"embed"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/log"
	"github.com/reviewqa/reviewqa/internal/mindmap"
	"github.com/reviewqa/reviewqa/internal/plan"
)

//go:embed all:templates
var templatesFS embed.FS

// Mount point: we embed via a re-export below; the embed directive can't
// reach ../../templates, so we duplicate-link at build time via a //go:embed
// in the wrapper file.

type Rendered struct {
	Path         string
	Content      []byte
	Symbol       ast.Symbol
	QualityNotes []string // one entry per weak / skipped locator found in this spec
	// IfMissingOnly mirrors plan.Item.IfMissingOnly through to the
	// PR-write layer — when true, the writer must NOT overwrite an
	// existing file. Used by the bug-discovery ledger.
	IfMissingOnly bool
}

func Render(items []plan.Item, workDir string) ([]Rendered, error) {
	var out []Rendered
	for _, it := range items {
		// Raw-content pathway: gen.Render writes Item.RawContent verbatim
		// without ever consulting a template. Used by the browser-mode DOM
		// snapshot pipeline so probed HTML lands beside the specs without
		// going through Go's text/template (which would escape it).
		if it.Template == plan.TmplRaw {
			out = append(out, Rendered{Path: it.OutPath, Content: it.RawContent, Symbol: it.Symbol, IfMissingOnly: it.IfMissingOnly})
			log.Debug("emitted raw artifact", "path", it.OutPath, "bytes", len(it.RawContent))
			continue
		}
		tmpl, err := load(it.Template)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", it.Template, err)
		}
		data := buildData(it, workDir)
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("render %s for %s: %w", it.Template, it.Symbol.Name, err)
		}
		// Quality report only makes sense for code-shaped templates that
		// embed weak-locator markers. Skip for the catalogue / summary /
		// steps templates — they're stakeholder docs, not specs.
		var content []byte
		var notes []string
		switch it.Template {
		case plan.TmplPlaywrightCatalogue, plan.TmplPlaywrightSummary,
			plan.TmplPlaywrightSteps, plan.TmplPlaywrightFindings,
			plan.TmplPlaywrightStepsBDD:
			content = buf.Bytes()
		default:
			content, notes = annotateQualityReport(buf.Bytes(), it.Symbol)
		}
		out = append(out, Rendered{Path: it.OutPath, Content: content, Symbol: it.Symbol, QualityNotes: notes, IfMissingOnly: it.IfMissingOnly})
		log.Debug("rendered scaffold", "template", it.Template, "symbol", it.Symbol.Name, "path", it.OutPath, "quality_notes", len(notes))
	}
	return out, nil
}

// annotateQualityReport scans a rendered spec for weak-locator markers
// (`// SKIP:` and `// note: using <...>`) AND for the proactive signal —
// a Symbol with real anchors/inputs but zero data-testid. Prepends a block
// comment summarising the findings. Returns the (possibly modified)
// content + the list of note strings for the caller to surface in the
// PR body.
func annotateQualityReport(content []byte, sym ast.Symbol) ([]byte, []string) {
	var notes []string
	for line := range strings.SplitSeq(string(content), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "// SKIP:"):
			notes = append(notes, strings.TrimPrefix(t, "// "))
		case strings.HasPrefix(t, "// note:"):
			notes = append(notes, strings.TrimPrefix(t, "// "))
		}
	}
	// Proactive: a page with real surface area (≥1 form input OR ≥3
	// anchors) but zero data-testids never reaches a fallback — but it
	// IS the customer's main quality problem. Surface it explicitly.
	if len(notes) == 0 && hasTestableSurface(sym) && !anyTestID(sym) {
		notes = append(notes, fmt.Sprintf("no data-testid attributes found on this page (%d inputs, %d anchors). Tests rely on text/role and break under copy edits.", len(sym.Inputs), len(sym.Anchors)))
	}
	if len(notes) == 0 {
		return content, nil
	}
	var b strings.Builder
	b.WriteString("/* reviewqa quality report\n")
	b.WriteString(" * Weak / missing locators on this page:\n")
	for _, n := range notes {
		fmt.Fprintf(&b, " *   - %s\n", n)
	}
	b.WriteString(" * Add data-testid to these elements for stable tests.\n")
	b.WriteString(" */\n")
	return append([]byte(b.String()), content...), notes
}

func hasTestableSurface(s ast.Symbol) bool {
	return len(s.Inputs) >= 1 || len(s.Anchors) >= 3
}

func anyTestID(s ast.Symbol) bool {
	for _, a := range s.Anchors {
		if a.TestID != "" {
			return true
		}
	}
	for _, i := range s.Inputs {
		if i.TestID != "" {
			return true
		}
	}
	return false
}

func load(t plan.Template) (*template.Template, error) {
	sub, file := templateLocation(t)
	body, err := templatesFS.ReadFile(path.Join("templates", sub, file))
	if err != nil {
		return nil, err
	}
	return template.New(string(t)).Funcs(funcs).Parse(string(body))
}

// templateRegistry maps plan.Template constants to (subdir, filename)
// pairs under internal/gen/templates/. Lookup is O(1) and adding a new
// template is a one-line registration (no growing switch). Cyclomatic
// complexity drops from 75 (the old switch) to 2.
type templateLoc struct {
	subdir, file string
}

var templateRegistry = map[plan.Template]templateLoc{
	plan.TmplJestUnit:                   {"ts", "jest_unit.tmpl"},
	plan.TmplJestAPI:                    {"ts", "jest_api.tmpl"},
	plan.TmplPlaywrightE2E:              {"ts", "pw_e2e.tmpl"},
	plan.TmplPlaywrightHappyFlow:        {"ts", "pw_happyflow.tmpl"},
	plan.TmplPlaywrightFixtures:         {"ts", "pw_fixtures.tmpl"},
	plan.TmplPlaywrightConfig:           {"ts", "pw_config.tmpl"},
	plan.TmplPlaywrightReadme:           {"ts", "pw_readme.tmpl"},
	plan.TmplPlaywrightPackage:          {"ts", "pw_package.tmpl"},
	plan.TmplPlaywrightTsconfig:         {"ts", "pw_tsconfig.tmpl"},
	plan.TmplPlaywrightCIFile:           {"ts", "pw_ci_workflow.tmpl"},
	plan.TmplPlaywrightFuzz:             {"ts", "pw_fuzz.tmpl"},
	plan.TmplPlaywrightFeature:          {"ts", "pw_feature.tmpl"},
	plan.TmplPlaywrightSteps:            {"ts", "pw_steps.tmpl"},
	plan.TmplPlaywrightCatalogue:        {"ts", "pw_test_catalogue.tmpl"},
	plan.TmplPlaywrightSummary:          {"ts", "pw_work_summary.tmpl"},
	plan.TmplPlaywrightAPI:              {"ts", "pw_api.tmpl"},
	plan.TmplPlaywrightFindings:         {"ts", "pw_findings.tmpl"},
	plan.TmplPlaywrightStepsBDD:         {"ts", "pw_steps_bdd.tmpl"},
	plan.TmplPlaywrightA11y:             {"ts", "pw_a11y.tmpl"},
	plan.TmplPlaywrightResponsive:       {"ts", "pw_responsive.tmpl"},
	plan.TmplPlaywrightPerf:             {"ts", "pw_perf.tmpl"},
	plan.TmplPlaywrightSecurity:         {"ts", "pw_security.tmpl"},
	plan.TmplPlaywrightHealth:           {"ts", "pw_health.tmpl"},
	plan.TmplPlaywrightContract:         {"ts", "pw_contract.tmpl"},
	plan.TmplPlaywrightObservability:    {"ts", "pw_observability.tmpl"},
	plan.TmplPlaywrightI18n:             {"ts", "pw_i18n.tmpl"},
	plan.TmplJestProperty:               {"ts", "jest_property.tmpl"},
	plan.TmplJestSerialization:          {"ts", "jest_serialization.tmpl"},
	plan.TmplJestValidatorPos:           {"ts", "jest_validator_positive.tmpl"},
	plan.TmplPytestProperty:             {"py", "pytest_property.tmpl"},
	plan.TmplPytestSerialization:        {"py", "pytest_serialization.tmpl"},
	plan.TmplPytestValidatorPos:         {"py", "pytest_validator_positive.tmpl"},
	plan.TmplPlaywrightVisual:           {"ts", "pw_visual.tmpl"},
	plan.TmplPlaywrightGraphQL:          {"ts", "pw_graphql.tmpl"},
	plan.TmplPlaywrightWebhook:          {"ts", "pw_webhook.tmpl"},
	plan.TmplGRPCUnary:                  {"ts", "grpc_unary.tmpl"},
	plan.TmplGRPCServerStream:           {"ts", "grpc_server_stream.tmpl"},
	plan.TmplGRPCClientStream:           {"ts", "grpc_client_stream.tmpl"},
	plan.TmplGRPCBidi:                   {"ts", "grpc_bidi.tmpl"},
	plan.TmplPlaywrightIdempotency:      {"ts", "pw_idempotency.tmpl"},
	plan.TmplPlaywrightPagination:       {"ts", "pw_pagination.tmpl"},
	plan.TmplPlaywrightContentNegotiation: {"ts", "pw_content_negotiation.tmpl"},
	plan.TmplPlaywrightAuthHeaders:      {"ts", "pw_auth_headers.tmpl"},
	plan.TmplPlaywrightVersioning:       {"ts", "pw_versioning.tmpl"},
	plan.TmplOpenAPICompat:              {"ts", "openapi_compat.tmpl"},
	plan.TmplProtoCompat:                {"ts", "proto_compat.tmpl"},
	plan.TmplAsyncAPICompat:             {"ts", "asyncapi_compat.tmpl"},
	plan.TmplJestStore:                  {"ts", "jest_store.tmpl"},
	plan.TmplJestConstructor:            {"ts", "jest_constructor.tmpl"},
	plan.TmplPytestConstructor:          {"py", "pytest_constructor.tmpl"},
	plan.TmplScheduledJob:               {"ts", "scheduled_job.tmpl"},
	plan.TmplEventHandler:               {"ts", "event_handler.tmpl"},
	plan.TmplEmailTemplate:              {"ts", "email_template.tmpl"},
	plan.TmplIntegrationDB:              {"ts", "integration_db.tmpl"},
	plan.TmplIntegrationBroker:          {"ts", "integration_broker.tmpl"},
	plan.TmplIntegrationCache:           {"ts", "integration_cache.tmpl"},
	plan.TmplIntegrationStorage:         {"ts", "integration_storage.tmpl"},
	plan.TmplIntegrationSearch:          {"ts", "integration_search.tmpl"},
	plan.TmplIntegrationAuth:            {"ts", "integration_auth.tmpl"},
	plan.TmplIntegrationContainers:      {"ts", "integration_containers.tmpl"},
	plan.TmplIntegrationCompose:         {"ts", "integration_compose.tmpl"},
	plan.TmplPlaywrightMobile:           {"ts", "pw_mobile.tmpl"},
	plan.TmplPlaywrightDeepLink:         {"ts", "pw_deeplink.tmpl"},
	plan.TmplRNHappyFlow:                {"ts", "rn_happyflow.tmpl"},
	plan.TmplFlutterHappyFlow:           {"ts", "flutter_happyflow.tmpl"},
	plan.TmplDbtSchema:                  {"py", "dbt_schema.tmpl"},
	plan.TmplPanderaConformance:         {"py", "pandera_conformance.tmpl"},
	plan.TmplGreatExpectations:          {"py", "great_expectations.tmpl"},
	plan.TmplPytestUnit:                 {"py", "pytest_unit.tmpl"},
	plan.TmplPytestAPI:                  {"py", "pytest_api.tmpl"},
	plan.TmplGoUnit:                     {"go", "gotest_unit.tmpl"},
	plan.TmplGoHTTPTest:                 {"go", "gotest_httptest.tmpl"},
	plan.TmplJUnit5Unit:                 {"java", "junit5_unit.tmpl"},
	plan.TmplJUnit5RestAssured:          {"java", "junit5_restassured.tmpl"},
}

func templateLocation(t plan.Template) (string, string) {
	if loc, ok := templateRegistry[t]; ok {
		return loc.subdir, loc.file
	}
	return "", ""
}

var funcs = template.FuncMap{
	"lower":     strings.ToLower,
	"upper":     strings.ToUpper,
	"hasPrefix": strings.HasPrefix,
	"firstClickable": func(as []ast.LocatorAnchor) []ast.LocatorAnchor {
		for _, a := range as {
			switch a.Tag {
			case "button", "summary", "a", "input":
				return []ast.LocatorAnchor{a}
			}
		}
		return nil
	},
	"locatorFor": func(a ast.LocatorAnchor) string {
		switch {
		case a.TestID != "":
			return fmt.Sprintf("getByTestId('%s')", a.TestID)
		case a.Aria != "":
			return fmt.Sprintf("getByLabel('%s')", a.Aria)
		case a.Role != "":
			return fmt.Sprintf("getByRole('%s')", a.Role)
		case a.Name != "" && a.Tag == "submit":
			// input[type=submit] / button[type=submit] surface their visible
			// text (value= or button body) as the accessible name; address by
			// role+name so the locator survives styling churn.
			return fmt.Sprintf("getByRole('button', { name: '%s' })", a.Name)
		case a.Name != "":
			return fmt.Sprintf("getByRole('button', { name: '%s' })", a.Name)
		}
		return ""
	},
	"anchorLabel": func(a ast.LocatorAnchor) string {
		switch {
		case a.TestID != "":
			return a.TestID
		case a.Aria != "":
			return a.Aria
		case a.Role != "":
			return a.Role
		}
		return "element"
	},
	"isPrimitiveType": func(t string) bool {
		switch strings.TrimSpace(t) {
		case "int", "long", "short", "byte", "double", "float", "boolean", "char":
			return true
		}
		return false
	},
	"defaultForType": func(t string) string {
		switch strings.TrimSpace(t) {
		case "int", "long", "short", "byte", "double", "float":
			return "0"
		case "boolean":
			return "false"
		case "char":
			return "'\\0'"
		case "string", "String":
			return "\"\""
		}
		return "null"
	},
	"fillValueFor":        fillValueFor,
	"inputLocator":        inputLocator,
	"firstSubmit":         firstSubmit,
	"firstSameOriginLink": firstSameOriginLink,
	"linkHref":            linkHref,
	"shouldCheck":         func(i ast.FormInput) bool { return i.Type == "checkbox" || i.Type == "radio" },
	"hasRequiredInput":      hasRequiredInput,
	"firstRequiredInput":    firstRequiredInput,
	"firstEmailInput":       firstEmailInput,
	"firstOversizableInput": firstOversizableInput,
	"isAbsoluteURL": func(s string) bool {
		return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
	},
	// landingPath returns the baseURL-relative path for the landing
	// page.goto() call. For an absolute probe URL like
	// "https://x.test/foo" we emit page.goto('/foo'); for the root we
	// emit page.goto('/'). Quotes are included in the returned string
	// so the template can emit it raw.
	"landingPath": func(pageURL string) string {
		u, err := url.Parse(pageURL)
		if err != nil || u == nil {
			return "'/'"
		}
		p := u.Path
		if p == "" {
			p = "/"
		}
		return fmt.Sprintf("'%s'", p)
	},
	// hasKnownFailurePattern reports whether the symbol carries one of
	// the known-broken patterns reviewqa recognises (today: Webflow
	// data-wait submit). When true, the template marks the test with
	// test.fail() so CI doesn't burn on a known-broken spec.
	"hasKnownFailurePattern": func(s ast.Symbol) bool {
		for _, a := range s.Anchors {
			if a.Tag == "submit" && a.CSS == "data-wait" {
				return true
			}
		}
		return false
	},
	"intentFor":         intentFor,
	"locatorProvenance": locatorProvenance,
	"contentLocator":    contentLocator,
	"regexEscape":       regexEscape,
	"rankedNavTargets":  rankedNavTargets,
	// interactionLocator builds a Playwright locator for an Interaction.
	// Falls back through testid → aria-label → role+name → tag text.
	"interactionLocator": func(i ast.Interaction) string {
		if i.TestID != "" {
			return fmt.Sprintf("getByTestId('%s')", escapeJSString(i.TestID))
		}
		if i.Kind == "search" && i.InputType == "search" {
			return "locator('input[type=\"search\"]')"
		}
		if i.Kind == "date" && i.InputType != "" {
			return fmt.Sprintf("locator('input[type=\"%s\"]')", i.InputType)
		}
		if i.Kind == "dialog" {
			return "locator('dialog')"
		}
		if i.Kind == "details" {
			if i.Text != "" {
				return fmt.Sprintf("locator('details', { has: page.locator('summary', { hasText: '%s' }) })", escapeJSString(i.Text))
			}
			return "locator('details').first()"
		}
		if i.Kind == "tab" && i.Text != "" {
			return fmt.Sprintf("getByRole('tab', { name: /%s/i })", regexEscape(i.Text))
		}
		if i.Aria != "" {
			return fmt.Sprintf("getByLabel('%s')", escapeJSString(i.Aria))
		}
		if i.Role != "" && i.Text != "" {
			return fmt.Sprintf("getByRole('%s', { name: /%s/i })", i.Role, regexEscape(i.Text))
		}
		if i.Text != "" {
			return fmt.Sprintf("getByText(/%s/i)", regexEscape(i.Text))
		}
		return "locator('button').first()"
	},
	// fillValueForInteraction picks a deterministic test value for a
	// fillable interaction (search query, date, etc).
	"fillValueForInteraction": func(i ast.Interaction) string {
		switch i.Kind {
		case "search":
			return "test"
		case "date":
			switch i.InputType {
			case "time":
				return "12:00"
			case "datetime-local":
				return "2026-06-17T12:00"
			}
			return "2026-06-17"
		}
		return ""
	},
	"firstH1": func(cs []ast.ContentAnchor) ast.ContentAnchor {
		for _, c := range cs {
			if c.Tag == "h1" {
				return c
			}
		}
		return ast.ContentAnchor{}
	},
	// firstH2s returns the first n h2 anchors in `cs`. Used per step so
	// chained-step assertions include sub-heading visibility — h2 was
	// previously emitted only as a fallback when no anchors existed.
	"firstH2s": func(cs []ast.ContentAnchor, n int) []ast.ContentAnchor {
		var out []ast.ContentAnchor
		for _, c := range cs {
			if c.Tag != "h2" {
				continue
			}
			out = append(out, c)
			if len(out) >= n {
				break
			}
		}
		return out
	},
	// topImages returns the first n images carrying non-empty alt text.
	"topImages": func(imgs []ast.ImageRef, n int) []ast.ImageRef {
		if len(imgs) > n {
			return imgs[:n]
		}
		return imgs
	},
	"rankedNavTargetsExcluding": func(links []ast.LocatorAnchor, n int, exclude string) []ast.LocatorAnchor {
		filtered := make([]ast.LocatorAnchor, 0, len(links))
		for _, l := range links {
			if l.Aria == exclude {
				continue
			}
			filtered = append(filtered, l)
		}
		return rankedNavTargets(filtered, n)
	},
	// rankedNavTargetsRotated picks one outbound target from the ranked
	// list, offsetting by a salt hash. Different specs in the same suite
	// get different outbound clicks instead of all ending at the
	// highest-scoring link.
	"rankedNavTargetsRotated": func(links []ast.LocatorAnchor, exclude, salt string) []ast.LocatorAnchor {
		filtered := make([]ast.LocatorAnchor, 0, len(links))
		for _, l := range links {
			if l.Aria == exclude {
				continue
			}
			filtered = append(filtered, l)
		}
		all := rankedNavTargets(filtered, 8)
		if len(all) == 0 {
			return nil
		}
		h := 0
		for _, r := range salt {
			h = h*31 + int(r)
		}
		if h < 0 {
			h = -h
		}
		return []ast.LocatorAnchor{all[h%len(all)]}
	},
	"add":               func(a, b int) int { return a + b },
	"sub":               func(a, b int) int { return a - b },
	// journeyPriority resolves a journey-kind string to its priority bucket
	// (critical / standard / nice-to-have). Used by the happyflow template
	// to emit `@priority:<level>` tags alongside `@journey:<kind>`.
	"journeyPriority": func(kind string) string {
		return mindmap.JourneyPriority(mindmap.JourneyKind(kind))
	},
	// countByPriority counts how many journeys carry the given priority.
	// Used by the work-summary template's priority-mix bar.
	"countByPriority": func(js []plan.CatalogueJourney, level string) int {
		n := 0
		for _, j := range js {
			if j.Priority == level {
				n++
			}
		}
		return n
	},
	// percent returns ceil(100 * n / d) capped at 100. Used to size the
	// priority-mix bar segments in the work-summary HTML.
	"percent": func(n, d int) int {
		if d <= 0 {
			return 0
		}
		p := (n * 100) / d
		if p > 100 {
			return 100
		}
		return p
	},
	// apiMethod normalises a form's method attribute to a Playwright
	// request fixture verb. Empty / unknown methods fall back to "post"
	// because that matches the dominant intent for forms with an action
	// — GET-style forms are uncommon enough that defaulting them to
	// POST surfaces the misconfiguration in the response.
	"apiMethod": func(m string) string {
		switch strings.ToLower(strings.TrimSpace(m)) {
		case "get":
			return "get"
		case "put":
			return "put"
		case "patch":
			return "patch"
		case "delete":
			return "delete"
		case "":
			return "post"
		}
		return "post"
	},
	// apiEncType returns a human-readable enctype label for the docstring.
	"apiEncType": func(e string) string {
		if e == "" {
			return "application/x-www-form-urlencoded (default)"
		}
		return e
	},
	// apiBodyKey returns the Playwright request option key for a given
	// enctype: `form` for url-encoded, `multipart` for file uploads,
	// `data` for application/json. Matches @playwright/test's request
	// fixture API.
	"apiBodyKey": func(e string) string {
		switch strings.ToLower(strings.TrimSpace(e)) {
		case "multipart/form-data":
			return "multipart"
		case "application/json":
			return "data"
		}
		return "form"
	},
	// isOversizable mirrors firstOversizableInput's predicate but for
	// a single input. The API negative-payload test fills the longest
	// text-shaped field with 50k chars; other fields keep their normal
	// values so the body is still structurally valid.
	"isIDColumn": func(name string) bool {
		n := strings.ToLower(name)
		return n == "id" || strings.HasSuffix(n, "_id")
	},
	"isRequiredField": func(p ast.Param) bool {
		// dbt schema heuristic: any column not nullable is required.
		// Without metadata we default to true for the v0.29 scaffold;
		// consumers tighten by hand.
		return true
	},
	"isOversizable": func(i ast.FormInput) bool {
		switch i.Type {
		case "textarea", "text", "email", "url", "tel":
			return true
		}
		return false
	},
	// propertyArbitraryFor maps a Param type to a fast-check arbitrary.
	// Falls back to fc.anything() for unknown types.
	"propertyArbitraryFor": func(p ast.Param) string {
		switch strings.ToLower(strings.TrimSpace(p.Type)) {
		case "number", "int", "float", "double":
			return "fc.integer()"
		case "string":
			return "fc.string()"
		case "boolean", "bool":
			return "fc.boolean()"
		case "string[]", "array<string>":
			return "fc.array(fc.string())"
		case "number[]", "array<number>":
			return "fc.array(fc.integer())"
		}
		return "fc.anything()"
	},
	// pyStrategyFor maps a Param type to a hypothesis strategy.
	"pyStrategyFor": func(t string) string {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "int":
			return "st.integers()"
		case "float":
			return "st.floats(allow_nan=False, allow_infinity=False)"
		case "str", "string":
			return "st.text()"
		case "bool":
			return "st.booleans()"
		case "list", "list[int]":
			return "st.lists(st.integers())"
		case "list[str]":
			return "st.lists(st.text())"
		case "dict":
			return "st.dictionaries(st.text(), st.integers())"
		}
		return "st.none() | st.integers() | st.text()"
	},
	// dtoSampleValue produces a deterministic placeholder value matching
	// the TS field type for the serialization round-trip and validator
	// templates.
	"dtoSampleValue": func(t string) string {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "number", "int", "float":
			return "0"
		case "string":
			return `"sample"`
		case "boolean", "bool":
			return "false"
		case "string[]", "array<string>":
			return `[]`
		case "number[]", "array<number>":
			return `[]`
		}
		return `"sample"`
	},
	// firstTextInput returns a single-element slice with the first
	// text-like input. Used by the v0.36 boundary + tab-order scenarios.
	"firstTextInput": func(inputs []ast.FormInput) []ast.FormInput {
		for _, i := range inputs {
			switch i.Type {
			case "text", "email", "search", "url", "tel", "textarea":
				return []ast.FormInput{i}
			}
		}
		return nil
	},
	// boundaryValueFor produces a value at the upper boundary of what
	// a typical input accepts. For text-like fields that's 200 chars;
	// for email it's a 200-char local part respecting RFC max.
	"boundaryValueFor": func(i ast.FormInput) string {
		switch i.Type {
		case "email":
			return strings.Repeat("a", 60) + "@example.com"
		case "url":
			return "https://example.com/" + strings.Repeat("a", 180)
		case "tel":
			return "+15551234567" + strings.Repeat("0", 10)
		case "textarea":
			return strings.Repeat("lorem ipsum ", 80)
		}
		return strings.Repeat("a", 200)
	},
	// oversizedValueFor produces a deliberately huge value (5000 chars
	// of one repeated character). For an oversized-input test.
	"oversizedValueFor": func(i ast.FormInput) string {
		return strings.Repeat("a", 5000)
	},
	// paramRowsFor returns 0-3 deterministic valid-value rows for a
	// text-like input, used to drive Scenario Outline `Examples:`
	// tables. Empty slice when the input shape doesn't support
	// parameterized sweeps (e.g. checkbox, select).
	"paramRowsFor": paramRowsFor,
	"paramRowVariant": func(r paramRow) string { return r.Variant },
	"paramRowValue":   func(r paramRow) string { return r.Value },
	// pageIsListLike reports whether the symbol's tags / content
	// suggest a list / search shape that warrants an empty-state test.
	"pageIsListLike": func(s ast.Symbol) bool {
		// Heuristic: any content anchor with tag "h2" + page has
		// links — implies a content list. Conservative.
		h2s := 0
		for _, c := range s.Contents {
			if c.Tag == "h2" {
				h2s++
			}
		}
		return h2s >= 2 && len(s.Links) > 0
	},
	// dtoSampleValuePy is the Python sibling of dtoSampleValue.
	"dtoSampleValuePy": func(t string) string {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "int":
			return "0"
		case "float":
			return "0.0"
		case "str", "string":
			return `"sample"`
		case "bool":
			return "False"
		case "list", "list[int]", "list[str]":
			return "[]"
		case "dict":
			return "{}"
		}
		return `"sample"`
	},
}

// contentLocator builds a Playwright locator for a content-text anchor —
// h1/h2/CTA text.
//
// h1 uses getByRole (the accessibility-tree path) because h1 is expected
// to be visible at the top of every page; if it isn't, the test should
// fail. h2 uses a tag-based locator instead because some sites hide h2s
// via CSS (display:none for SEO outline purposes) — those get filtered
// out of the accessibility tree but DO exist in DOM, and we want
// toBeAttached to still find them.
//
// The result embeds inside a regex literal, so apostrophes are not
// pre-escaped (regex literals don't need them; pre-escaping would double
// up against regexEscape's backslash handling).
func contentLocator(c ast.ContentAnchor) string {
	switch c.Tag {
	case "h1":
		return fmt.Sprintf("getByRole('heading', { level: 1, name: /%s/i })", regexEscape(c.Text))
	case "h2":
		return fmt.Sprintf("locator('h2', { hasText: /%s/i })", regexEscape(c.Text))
	}
	return fmt.Sprintf("getByText(/%s/i)", regexEscape(c.Text))
}

// escapeJSString conservatively escapes single quotes and backslashes so
// the value can be embedded inside a single-quoted JS string literal.
func escapeJSString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	return s
}

// regexEscape escapes a string for embedding in a JS regex literal between
// slashes. Conservative — only escapes characters that could terminate the
// regex or change its meaning.
func regexEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '/', '\\', '.', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|', '^', '$':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// fillByType is the deterministic test-value table for form inputs.
// Empty value signals .check() (for checkbox/radio) or .selectOption (for
// select) — i.e. a fill() call is NOT what the caller should emit.
var fillByType = map[string]string{
	"email":    "test@example.com",
	"password": "Passw0rd!",
	"tel":      "+15551234567",
	"phone":    "+15551234567",
	"url":      "https://example.com",
	"number":   "42",
	"date":     "2026-01-01",
	"time":     "12:00",
	"search":   "query",
	"textarea": "sample text",
	"checkbox": "",
	"radio":    "",
	"select":   "",
}

// fillValueFor returns a deterministic test value for a form input. The
// value never reflects PR diff content — purely type/name-derived.
func fillValueFor(i ast.FormInput) string {
	if v, ok := fillByType[i.Type]; ok {
		return v
	}
	if i.Name != "" {
		return i.Name + "-value"
	}
	return "test"
}

// inputLocator chooses the Playwright locator for a form input in priority
// order. Returns "" when no stable locator can be derived — the template
// then emits a SKIP comment instead of a meaningless `locator('input')`.
func inputLocator(i ast.FormInput) string {
	switch {
	case i.TestID != "":
		return fmt.Sprintf("getByTestId('%s')", i.TestID)
	case i.Aria != "":
		return fmt.Sprintf("getByLabel('%s')", i.Aria)
	case i.Placeholder != "":
		return fmt.Sprintf("getByPlaceholder('%s')", i.Placeholder)
	case i.LabelText != "":
		return fmt.Sprintf("getByLabel('%s')", i.LabelText)
	case i.Name != "":
		return fmt.Sprintf("locator('[name=\"%s\"]')", i.Name)
	}
	return ""
}

// locatorProvenance returns the rank label for the fallback chain when the
// chosen locator is NOT the strongest (testid). Empty string means the input
// has a strong locator and no provenance note is needed.
func locatorProvenance(i ast.FormInput) string {
	switch {
	case i.TestID != "":
		return ""
	case i.Aria != "":
		return ""
	case i.Placeholder != "":
		return "placeholder"
	case i.LabelText != "":
		return "label-for"
	case i.Name != "":
		return "name"
	}
	return ""
}

// firstSubmit returns the first submit-tagged anchor, otherwise the first
// button anchor. Inputs are NOT considered — they're for fills, not clicks.
// Single-element slice for use with {{with}} in templates.
func firstSubmit(anchors []ast.LocatorAnchor) []ast.LocatorAnchor {
	for _, a := range anchors {
		if a.Tag == "submit" {
			return []ast.LocatorAnchor{a}
		}
	}
	for _, a := range anchors {
		if a.Tag == "button" {
			return []ast.LocatorAnchor{a}
		}
	}
	return nil
}

// firstSameOriginLink returns the first ranked same-origin link.
// Single-element slice for {{with}}. Kept for backward-compat with the
// existing templates; new code should call rankedNavTargets directly.
func firstSameOriginLink(links []ast.LocatorAnchor) []ast.LocatorAnchor {
	ranked := rankedNavTargets(links, 1)
	if len(ranked) == 0 {
		return nil
	}
	return ranked[:1]
}

// navVocabulary is the set of substrings that signal a high-signal user
// action. Ordered roughly by intent strength. Matches are case-insensitive
// substrings of the link's visible text.
var navVocabulary = []string{
	"contact", "get started", "sign up", "signup", "sign in", "log in", "login",
	"book a demo", "request a demo", "talk to sales", "pricing",
	"case studies", "case study", "services", "products", "features",
	"learn more", "read more", "subscribe", "buy now", "get a quote",
}

// navAvoidPath are href substrings we deprioritise: legal/footer pages
// that are valid but uninteresting as primary nav targets.
var navAvoidPath = []string{
	"privacy", "terms", "cookie", "legal", "sitemap", "rss", "feed",
}

// rankedNavTargets returns up to n same-origin links ordered by user-action
// signal strength. Score: +3 for vocabulary text match, +1 for short href
// (likely a top-level page), -3 for legal/footer paths, -1 for href ending
// in a deep slug.
func rankedNavTargets(links []ast.LocatorAnchor, n int) []ast.LocatorAnchor {
	type scored struct {
		anchor ast.LocatorAnchor
		score  int
	}
	var all []scored
	seenHref := map[string]bool{}
	for _, l := range links {
		if !strings.HasPrefix(l.Aria, "/") || strings.HasPrefix(l.Aria, "//") {
			continue
		}
		if seenHref[l.Aria] {
			continue
		}
		seenHref[l.Aria] = true
		s := scored{anchor: l}
		lowerText := strings.ToLower(l.Text)
		for _, v := range navVocabulary {
			if strings.Contains(lowerText, v) {
				s.score += 3
				break
			}
		}
		lowerHref := strings.ToLower(l.Aria)
		for _, v := range navVocabulary {
			if strings.Contains(lowerHref, strings.ReplaceAll(v, " ", "-")) {
				s.score += 2
				break
			}
		}
		for _, v := range navAvoidPath {
			if strings.Contains(lowerHref, v) {
				s.score -= 3
			}
		}
		// Slightly prefer shorter (top-level) hrefs.
		if strings.Count(l.Aria, "/") <= 1 {
			s.score++
		}
		all = append(all, s)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	if len(all) > n {
		all = all[:n]
	}
	out := make([]ast.LocatorAnchor, 0, len(all))
	for _, s := range all {
		out = append(out, s.anchor)
	}
	return out
}

// linkHref returns the link's href (stored in Aria during extraction).
func linkHref(l ast.LocatorAnchor) string {
	return l.Aria
}

// intentFor classifies a Symbol into one of three flow shapes. The template
// switches on the returned string to emit a form-fill, nav-only, or
// minimal-smoke spec — instead of the previous fire-everything approach
// that produced fill+submit calls on pages with no login intent.
func intentFor(s ast.Symbol) string {
	hasSubmit := false
	for _, a := range s.Anchors {
		if a.Tag == "submit" {
			hasSubmit = true
			break
		}
	}
	hasRequired := false
	for _, i := range s.Inputs {
		if i.Required {
			hasRequired = true
			break
		}
	}
	if s.HasForm && hasRequired && hasSubmit {
		return "form"
	}
	for _, l := range s.Links {
		if l.Aria != "" {
			return "nav"
		}
	}
	return "content"
}

// hasRequiredInput is true when any FormInput in the slice is marked
// required. Used to gate the onSubmit validation scenario.
func hasRequiredInput(inputs []ast.FormInput) bool {
	for _, i := range inputs {
		if i.Required {
			return true
		}
	}
	return false
}

// firstRequiredInput returns a single-element slice with the first input
// flagged required. Designed for {{with firstRequiredInput .Inputs}} in
// templates. Returns nil when none exist.
func firstRequiredInput(inputs []ast.FormInput) []ast.FormInput {
	for _, i := range inputs {
		if i.Required {
			return []ast.FormInput{i}
		}
	}
	return nil
}

// firstEmailInput returns a single-element slice with the first input
// whose type is "email". Gates the "rejects malformed email" negative
// test family.
func firstEmailInput(inputs []ast.FormInput) []ast.FormInput {
	for _, i := range inputs {
		if i.Type == "email" {
			return []ast.FormInput{i}
		}
	}
	return nil
}

// firstOversizableInput returns the first textarea or text input — the
// fields where an oversized payload would meaningfully exercise input
// length handling. textarea wins over text when both exist.
func firstOversizableInput(inputs []ast.FormInput) []ast.FormInput {
	for _, i := range inputs {
		if i.Type == "textarea" {
			return []ast.FormInput{i}
		}
	}
	for _, i := range inputs {
		if i.Type == "text" || i.Type == "email" || i.Type == "url" || i.Type == "tel" {
			return []ast.FormInput{i}
		}
	}
	return nil
}

// paramRow is one row of a Scenario Outline `Examples:` table for the
// v0.37 parameterized form scenarios.
type paramRow struct {
	Variant string
	Value   string
}

// paramRowsFor returns up to 3 deterministic valid-value rows for a
// text-like input. The row table is intentionally short — Gherkin
// Examples blocks bloat .feature files fast, and three rows already
// surface 90% of the variation a typical input mishandles.
func paramRowsFor(i ast.FormInput) []paramRow {
	switch i.Type {
	case "email":
		return []paramRow{
			{"typical", "jane@example.com"},
			{"plus-alias", "jane+alias@example.com"},
			{"subdomain", "user@mail.example.co.uk"},
		}
	case "password":
		return []paramRow{
			{"min-len", "Pass1234"},
			{"with-symbols", "Pa$$w0rd!"},
			{"long-passphrase", "correct-horse-battery-staple-1"},
		}
	case "tel":
		return []paramRow{
			{"us-e164", "+15551234567"},
			{"uk-e164", "+442012345678"},
			{"br-mobile", "+5511987654321"},
		}
	case "url":
		return []paramRow{
			{"https", "https://example.com"},
			{"with-path", "https://example.com/docs/getting-started"},
			{"with-query", "https://example.com/search?q=test"},
		}
	case "number":
		return []paramRow{
			{"zero", "0"},
			{"typical", "42"},
			{"large", "1000000"},
		}
	case "text", "search", "textarea":
		return []paramRow{
			{"short", "sample"},
			{"with-spaces", "sample text with spaces"},
			{"unicode", "café-niño-ümlaut"},
		}
	}
	return nil
}

type renderData struct {
	Symbol          ast.Symbol
	Symbols         []ast.Symbol // populated for happy-flow; first == Symbol
	PageURL         string       // populated for happy-flow; "/" default
	JourneyKind     string       // convert | browse | explore | read; empty for non-probe
	ImportPath      string
	AppImportPath   string
	SupertestMethod string
	HappyArgs       string
	SnakeName       string
	Package         string
	// Catalogue is the aggregated suite-level data the catalogue + summary
	// templates render against. Nil for spec-shaped templates.
	Catalogue *plan.Catalogue
	// Form is the FormSpec the API-contract template renders against.
	Form *ast.FormSpec
	// v0.25: LLM-composed scenarios for the feature template.
	ExtraScenarios []plan.ExtraScenario
	LLMModel       string
	// v0.27: integration-test context (reviewqa.yml-derived).
	Integration *plan.IntegrationCtx
}

func buildData(it plan.Item, workDir string) renderData {
	d := renderData{Symbol: it.Symbol}
	d.Catalogue = it.Catalogue
	d.Form = it.Form
	d.ExtraScenarios = it.ExtraScenarios
	d.LLMModel = it.LLMModel
	d.Integration = it.Integration
	d.Symbols = it.Symbols
	if len(d.Symbols) == 0 {
		d.Symbols = []ast.Symbol{it.Symbol}
	}
	d.PageURL = it.PageURL
	if d.PageURL == "" {
		d.PageURL = "/"
	}
	d.JourneyKind = it.JourneyKind
	d.HappyArgs = happyArgs(it.Symbol)
	d.SnakeName = toSnake(it.Symbol.Name)
	d.SupertestMethod = strings.ToLower(it.Symbol.Method)
	switch it.Symbol.Language {
	case "ts":
		d.ImportPath = relativeImport(it.OutPath, it.Symbol.File)
		d.AppImportPath = relativeImport(it.OutPath, deriveAppEntry(workDir, it.Symbol.File))
	case "python":
		d.ImportPath = pythonModule(it.Symbol.File)
		d.AppImportPath = pythonModule(deriveAppEntry(workDir, it.Symbol.File))
	case "go":
		d.Package = goPackageFor(it.OutPath)
	case "java":
		d.Package = javaPackageFor(it.OutPath)
	}
	return d
}

func happyArgs(s ast.Symbol) string {
	parts := make([]string, 0, len(s.Params))
	for _, p := range s.Params {
		parts = append(parts, defaultForType(s.Language, p.Type))
	}
	return strings.Join(parts, ", ")
}

func defaultForType(lang, typ string) string {
	t := strings.ToLower(strings.TrimSpace(typ))
	switch lang {
	case "ts":
		return defaultForTS(t)
	case "python":
		return defaultForPython(t)
	case "go":
		return defaultForGo(t)
	case "java":
		return defaultForJava(t)
	}
	return "null"
}

func defaultForTS(t string) string {
	switch {
	case t == "" || strings.Contains(t, "any") || strings.Contains(t, "unknown"):
		return "undefined"
	case strings.Contains(t, "number") || strings.Contains(t, "int") || strings.Contains(t, "float"):
		return "0"
	case strings.Contains(t, "string"):
		return `''`
	case strings.Contains(t, "bool"):
		return "false"
	case strings.HasPrefix(t, "array<") || strings.HasSuffix(t, "[]"):
		return "[]"
	}
	return "undefined"
}

func defaultForPython(t string) string {
	switch {
	case strings.Contains(t, "int"):
		return "0"
	case strings.Contains(t, "float"):
		return "0.0"
	case strings.Contains(t, "str"):
		return `""`
	case strings.Contains(t, "bool"):
		return "False"
	case strings.Contains(t, "list"):
		return "[]"
	case strings.Contains(t, "dict"):
		return "{}"
	}
	return "None"
}

func defaultForGo(t string) string {
	switch t {
	case "string":
		return `""`
	case "int", "int32", "int64", "uint", "uint32", "uint64", "byte", "rune",
		"float32", "float64":
		return "0"
	case "bool":
		return "false"
	}
	return "nil"
}

func defaultForJava(t string) string {
	switch t {
	case "int", "long", "short", "byte":
		return "0"
	case "double", "float":
		return "0.0"
	case "boolean":
		return "false"
	case "string":
		return `""`
	}
	return "null"
}

func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func relativeImport(testFile, srcFile string) string {
	if srcFile == "" {
		return "../src"
	}
	rel, err := filepath.Rel(filepath.Dir(testFile), srcFile)
	if err != nil {
		return strings.TrimSuffix(srcFile, filepath.Ext(srcFile))
	}
	rel = strings.TrimSuffix(rel, filepath.Ext(rel))
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}

func pythonModule(srcFile string) string {
	rel := strings.TrimSuffix(filepath.ToSlash(srcFile), ".py")
	return strings.ReplaceAll(rel, "/", ".")
}

func deriveAppEntry(workDir, source string) string {
	for _, c := range []string{"src/app.ts", "src/app.js", "src/index.ts", "src/server.ts", "app/main.py", "main.py"} {
		if _, err := os.Stat(filepath.Join(workDir, c)); err == nil {
			return c
		}
	}
	return source
}

func goPackageFor(testPath string) string {
	parts := strings.Split(filepath.ToSlash(testPath), "/")
	if len(parts) < 2 {
		return "main"
	}
	return parts[len(parts)-2]
}

func javaPackageFor(testPath string) string {
	rel := strings.TrimPrefix(filepath.ToSlash(testPath), "src/test/java/")
	dir := path.Dir(rel)
	if dir == "." || dir == "" {
		return "tests"
	}
	return strings.ReplaceAll(dir, "/", ".")
}
