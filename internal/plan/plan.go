// Package plan decides what to generate per discovered symbol.
package plan

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/diff"
)

type Template string

const (
	TmplJestUnit            Template = "jest_unit"
	TmplJestAPI             Template = "jest_api"
	TmplPlaywrightE2E       Template = "pw_e2e"
	TmplPlaywrightHappyFlow Template = "pw_happyflow"
	TmplPlaywrightFixtures  Template = "pw_fixtures"
	TmplPlaywrightConfig    Template = "pw_config"
	TmplPlaywrightReadme    Template = "pw_readme"
	TmplPlaywrightPackage   Template = "pw_package"
	TmplPlaywrightTsconfig  Template = "pw_tsconfig"
	TmplPlaywrightCIFile    Template = "pw_ci_workflow"
	TmplPlaywrightFuzz      Template = "pw_fuzz"
	TmplPlaywrightFeature   Template = "pw_feature"
	TmplPlaywrightSteps     Template = "pw_steps"
	TmplPlaywrightCatalogue Template = "pw_test_catalogue"
	TmplPlaywrightSummary   Template = "pw_work_summary"
	TmplPlaywrightAPI       Template = "pw_api"
	TmplPlaywrightFindings  Template = "pw_findings"
	TmplPlaywrightStepsBDD  Template = "pw_steps_bdd"
	TmplPlaywrightA11y      Template = "pw_a11y"
	TmplPlaywrightResponsive Template = "pw_responsive"
	TmplPlaywrightPerf      Template = "pw_perf"
	TmplPlaywrightSecurity  Template = "pw_security"
	TmplPlaywrightHealth    Template = "pw_health"
	TmplPlaywrightContract  Template = "pw_contract"
	TmplPlaywrightObservability Template = "pw_observability"
	TmplPlaywrightI18n      Template = "pw_i18n"
	TmplJestProperty        Template = "jest_property"
	TmplJestSerialization   Template = "jest_serialization"
	TmplJestValidatorPos    Template = "jest_validator_positive"
	TmplPytestProperty      Template = "pytest_property"
	TmplPytestSerialization Template = "pytest_serialization"
	TmplPytestValidatorPos  Template = "pytest_validator_positive"
	TmplPlaywrightVisual    Template = "pw_visual"
	TmplPlaywrightGraphQL   Template = "pw_graphql"
	TmplPlaywrightWebhook   Template = "pw_webhook"
	TmplGRPCUnary           Template = "grpc_unary"
	TmplGRPCServerStream    Template = "grpc_server_stream"
	TmplGRPCClientStream    Template = "grpc_client_stream"
	TmplGRPCBidi            Template = "grpc_bidi"
	TmplPlaywrightIdempotency       Template = "pw_idempotency"
	TmplPlaywrightPagination        Template = "pw_pagination"
	TmplPlaywrightContentNegotiation Template = "pw_content_negotiation"
	TmplPlaywrightAuthHeaders       Template = "pw_auth_headers"
	TmplPlaywrightVersioning        Template = "pw_versioning"
	TmplOpenAPICompat               Template = "openapi_compat"
	TmplProtoCompat                 Template = "proto_compat"
	TmplAsyncAPICompat              Template = "asyncapi_compat"
	TmplJestStore                   Template = "jest_store"
	TmplJestConstructor             Template = "jest_constructor"
	TmplPytestConstructor           Template = "pytest_constructor"
	TmplScheduledJob                Template = "scheduled_job"
	TmplEventHandler                Template = "event_handler"
	TmplEmailTemplate               Template = "email_template"
	TmplIntegrationDB               Template = "integration_db"
	TmplIntegrationBroker           Template = "integration_broker"
	TmplIntegrationCache            Template = "integration_cache"
	TmplIntegrationStorage          Template = "integration_storage"
	TmplIntegrationSearch           Template = "integration_search"
	TmplIntegrationAuth             Template = "integration_auth"
	TmplIntegrationContainers       Template = "integration_containers"
	TmplIntegrationCompose          Template = "integration_compose"
	TmplPlaywrightMobile            Template = "pw_mobile"
	TmplPlaywrightDeepLink          Template = "pw_deeplink"
	TmplRNHappyFlow                 Template = "rn_happyflow"
	TmplFlutterHappyFlow            Template = "flutter_happyflow"
	TmplDbtSchema                   Template = "dbt_schema"
	TmplPanderaConformance          Template = "pandera_conformance"
	TmplGreatExpectations           Template = "great_expectations"
	TmplPlaywrightVisualStates      Template = "pw_visual_states"
	TmplPlaywrightKeyboardNav       Template = "pw_keyboard_nav"
	TmplPlaywrightA11yLandmarks     Template = "pw_a11y_landmarks"
	TmplPlaywrightSentinel          Template = "pw_sentinel"
	// v0.42 — edge-case templates: deterministic coverage of the
	// common failure modes the happy-path suite misses.
	TmplPlaywrightNetworkResilience Template = "pw_network_resilience"
	TmplPlaywrightRace              Template = "pw_race"
	TmplPlaywrightStorage           Template = "pw_storage"
	TmplPlaywrightZoom              Template = "pw_zoom"
	TmplPlaywrightA11yPrefs         Template = "pw_a11y_prefs"
	TmplPlaywrightPrint             Template = "pw_print"
	TmplPlaywrightClipboard         Template = "pw_clipboard"
	TmplPlaywrightHTTPChains        Template = "pw_http_chains"
	// v0.43 — integration-layer scaffold emitted per origin when an
	// API endpoint is detected. Test.skip()s until the consumer wires
	// a real backing resource via reviewqa.yml.
	TmplPlaywrightIntegrationStub Template = "pw_integration_api_stub"
	// v0.44 — gated edge templates: emitted when the probe sees the
	// matching signal on the page.
	TmplPlaywrightFileUpload     Template = "pw_file_upload"
	TmplPlaywrightIframe         Template = "pw_iframe"
	TmplPlaywrightDateEdges      Template = "pw_date_edges"
	TmplPlaywrightPWA            Template = "pw_pwa"
	TmplPlaywrightHistoryDepth   Template = "pw_history_depth"
	// v0.45 — the last 4 gated edge templates from the original v0.42
	// scope. Each gates on a probe signal so the test runs against
	// real surface.
	TmplPlaywrightTouch          Template = "pw_touch"
	TmplPlaywrightDragDrop       Template = "pw_dragdrop"
	TmplPlaywrightAuthExpiry     Template = "pw_auth_expiry"
	TmplPlaywrightLocaleSwitch   Template = "pw_locale_switch"
	// v0.49 — always-attempt stubs for the Contract / Non-functional
	// layers. Emit per origin regardless of endpoint discovery; skip
	// gracefully when the candidate path 404s.
	TmplPlaywrightGraphQLStub    Template = "pw_graphql_stub"
	TmplPlaywrightWebhookStub    Template = "pw_webhook_stub"
	TmplRaw                 Template = "raw" // sentinel: emit Item.RawContent verbatim
	TmplPytestUnit          Template = "pytest_unit"
	TmplPytestAPI           Template = "pytest_api"
	TmplGoUnit              Template = "gotest_unit"
	TmplGoHTTPTest          Template = "gotest_httptest"
	TmplJUnit5Unit          Template = "junit5_unit"
	TmplJUnit5RestAssured   Template = "junit5_restassured"
)

type Item struct {
	Symbol   ast.Symbol
	Template Template
	OutPath  string
	// Symbols carries multiple symbols when an Item represents a
	// page-scoped happy-flow (TmplPlaywrightHappyFlow). For all other
	// templates, Symbols is empty and Symbol is authoritative.
	Symbols []ast.Symbol
	// PageURL is the relative URL the happy-flow visits, e.g. "/", "/home".
	PageURL string
	// JourneyKind names the user goal this spec exercises (convert,
	// browse, explore, read). Empty for non-journey items.
	JourneyKind string
	// Catalogue carries the aggregated suite-level data the catalogue +
	// summary templates render. Populated only for those two templates.
	Catalogue *Catalogue
	// RawContent, when non-nil, is written to OutPath verbatim — the
	// gen.Render template path is bypassed. Used by the browser-mode DOM
	// snapshot pipeline (Template = TmplRaw).
	RawContent []byte
	// Form, when non-nil, drives the API-contract spec template. The
	// resolved absolute URL of the form's action sits in PageURL.
	Form *ast.FormSpec
	// IfMissingOnly tells the PR-write layer to skip this item when the
	// target file already exists on disk / in the repo. Used by
	// hand-editable companion files (findings.md ledger) so prior rows
	// survive subsequent probe runs.
	IfMissingOnly bool
	// ExtraScenarios carries LLM-composed Gherkin scenarios that
	// pw_feature.tmpl renders below the deterministic ones. Each item
	// also carries the model id used so the rendered file announces
	// its provenance. Empty in the default (LLM-off) path.
	ExtraScenarios []ExtraScenario
	// LLMModel is the model identifier embedded as a comment above the
	// composed scenarios block. Empty when ExtraScenarios is empty.
	LLMModel string
	// Integration carries reviewqa.yml-derived data for the v0.27
	// integration-test family. Populated only for integration Items.
	Integration *IntegrationCtx
}

// ExtraScenario mirrors composer.ExtraScenario in the plan layer so
// the gen package doesn't have to import composer (which would
// create a layering cycle since composer needs to import plan in some
// future architectures). One step = one Gherkin step line.
type ExtraScenario struct {
	Name  string
	Tags  []string
	Steps []ExtraScenarioStep
}

// ExtraScenarioStep is one step of an ExtraScenario.
type ExtraScenarioStep struct {
	Keyword string
	Text    string
}

// IntegrationCtx carries the integration-test data the v0.27
// templates render against. Populated for one of (Database / Broker /
// Cache / Storage / Search / Auth) depending on which template the
// Item drives.
type IntegrationCtx struct {
	Database *IntegrationDB
	Broker   *IntegrationBroker
	Cache    *IntegrationCache
	Storage  *IntegrationStorage
	Search   *IntegrationSearch
	Auth     *IntegrationAuth
	// Containers holds the aggregated set rendered into the shared
	// _containers.ts file.
	Containers *IntegrationContainers
}

type IntegrationDB struct {
	Name, Driver, Image, Migrations string
}
type IntegrationBroker struct {
	Kind, Image string
	Topics      []string
}
type IntegrationCache struct{ Kind, Image string }
type IntegrationStorage struct{ Kind, Bucket string }
type IntegrationSearch struct{ Kind string }
type IntegrationAuth struct{ Provider, Issuer string }
type IntegrationContainers struct {
	Databases []IntegrationDB
	Brokers   []IntegrationBroker
	Caches    []IntegrationCache
	Storage   []IntegrationStorage
	Search    []IntegrationSearch
	Auth      []IntegrationAuth
}

// Catalogue is the suite-level data passed to the test-catalogue and
// work-summary templates. Captures what was crawled, what journeys were
// identified, and where each spec landed — without re-reading the
// mindmap from the template.
type Catalogue struct {
	Origin       string             // probed origin (scheme://host)
	GeneratedAt  string             // RFC3339 timestamp; empty when unknown
	Pages        []CataloguePage    // every page the crawler visited
	Journeys     []CatalogueJourney // every journey identified
	Fuzz         []CatalogueFuzz    // per-page fuzz specs
	CoverageMode string             // breadth | standard | depth
}

type CataloguePage struct {
	URL   string
	Title string
	Tags  []string
}

type CatalogueJourney struct {
	Kind     string
	Priority string
	OutPath  string // generated spec path (relative)
	Steps    []CatalogueStep
}

type CatalogueStep struct {
	URL        string
	Title      string
	EnteredVia string // empty for the first step (direct goto)
}

type CatalogueFuzz struct {
	PageURL string
	OutPath string
}

type Layout struct {
	// JS/TS
	HasJestDir   bool
	HasTestsDir  bool
	HasUnderTest bool // *.test.ts siblings
	UsesVitest   bool
	// Python
	HasPyTestsDir bool
	// Java
	HasMavenLayout bool // src/test/java
	// Generic
	WorkDir string
}

func Detect(workDir string) Layout {
	l := Layout{WorkDir: workDir}
	if has(workDir, "__tests__") {
		l.HasJestDir = true
	}
	if has(workDir, "tests") {
		l.HasTestsDir = true
		l.HasPyTestsDir = true
	}
	if hasMatch(workDir, "src", ".test.") {
		l.HasUnderTest = true
	}
	if has(workDir, filepath.Join("src", "test", "java")) {
		l.HasMavenLayout = true
	}
	// crude pkg detection
	if b, err := os.ReadFile(filepath.Join(workDir, "package.json")); err == nil {
		s := string(b)
		if strings.Contains(s, `"vitest"`) {
			l.UsesVitest = true
		}
	}
	return l
}

func has(root, sub string) bool {
	_, err := os.Stat(filepath.Join(root, sub))
	return err == nil
}

func hasMatch(root, dir, contains string) bool {
	matches, _ := filepath.Glob(filepath.Join(root, dir, "*"+contains+"*"))
	if len(matches) > 0 {
		return true
	}
	matches, _ = filepath.Glob(filepath.Join(root, dir, "**", "*"+contains+"*"))
	return len(matches) > 0
}

func Build(files []diff.File, layout Layout) []Item {
	var items []Item
	for _, f := range files {
		if f.Status == "removed" {
			continue
		}
		ex := ast.ForFile(f.Path)
		if ex == nil {
			continue
		}
		content := readNew(layout.WorkDir, f.Path, f.NewBlob)
		if len(content) == 0 {
			continue
		}
		syms, _ := ex.Extract(f.Path, content)
		for _, s := range syms {
			if !diff.Intersects(f.Added, s.Line, s.EndLine) {
				continue
			}
			it := Item{Symbol: s}
			it.Template = pickTemplate(s, layout)
			it.OutPath = testPathFor(s, it.Template, layout)
			items = append(items, it)
			// v0.24 fan-out: a single Symbol can spawn extra
			// per-aspect tests when the extractor stamped one of
			// the diff-mode signals.
			items = append(items, fanOutAspects(s, layout)...)
		}
	}
	if os.Getenv("REVIEWQA_E2E_STYLE") != "per-component" {
		items = groupByPage(items, files, layout)
	}
	return items
}

// fanOutAspects emits the diff-mode aspect items for a Symbol —
// property, validator, scheduled job / event handler / email, plus
// the v0.26 additions: serialization round-trip for DTOs, constructor
// tests for classes, store tests for Redux/Pinia/Zustand/Vuex stores.
// Each becomes a sibling test file under tests/<aspect>/<stem>.<aspect>.test.<ext>.
func fanOutAspects(s ast.Symbol, l Layout) []Item {
	var out []Item
	stem := stemOf(s.File)
	if s.IsPure && (s.Kind == ast.KindFunction || s.Kind == ast.KindMethod) {
		out = append(out, aspectItem(s, TmplJestProperty,
			"tests/property/"+stem+"-"+strings.ToLower(s.Name)+".property.test.ts"))
	}
	if s.IsValidator {
		out = append(out, aspectItem(s, TmplJestValidatorPos,
			"tests/validator/"+stem+"-"+strings.ToLower(s.Name)+".validator.test.ts"))
	}
	if s.IsDTO {
		out = append(out, aspectItem(s, TmplJestSerialization,
			"tests/serialization/"+stem+"-"+strings.ToLower(s.Name)+".serialization.test.ts"))
	}
	if s.StoreKind != "" && len(s.StoreActions) > 0 {
		out = append(out, aspectItem(s, TmplJestStore,
			"tests/store/"+stem+"-"+strings.ToLower(s.Name)+".store.test.ts"))
	}
	// Class with a constructor → constructor test. Detected via
	// FrameworkHint="class" stamped by the v0.26 extractor.
	if s.FrameworkHint == "class" && len(s.Params) > 0 {
		out = append(out, aspectItem(s, TmplJestConstructor,
			"tests/constructor/"+stem+"-"+strings.ToLower(s.Name)+".constructor.test.ts"))
	}
	switch s.JobKind {
	case "cron":
		out = append(out, aspectItem(s, TmplScheduledJob,
			"tests/jobs/"+stem+"-"+strings.ToLower(s.Name)+".cron.test.ts"))
	case "event":
		out = append(out, aspectItem(s, TmplEventHandler,
			"tests/events/"+stem+"-"+strings.ToLower(s.Name)+".event.test.ts"))
	case "email":
		out = append(out, aspectItem(s, TmplEmailTemplate,
			"tests/email/"+stem+"-"+strings.ToLower(s.Name)+".email.test.ts"))
	}
	return out
}

func aspectItem(s ast.Symbol, t Template, path string) Item {
	return Item{Symbol: s, Template: t, OutPath: path}
}

// BuildCompat scans the PR diff for changed schema files (.json /
// .yaml / .yml / .proto) and emits one compatibility-test item per
// detected breaking change set. Caller must supply oldBytes via the
// File.OldBlob field; new bytes come from File.NewBlob.
//
// The actual diff comparison is delegated to internal/compat. Items
// produced by BuildCompat carry the regression list packed into
// Symbol.Anchors (Tag=Kind, Name=Detail) so the compat template can
// render the rows verbatim.
func BuildCompat(files []diff.File, compare CompatComparator) []Item {
	var items []Item
	for _, f := range files {
		if f.Status == "removed" {
			continue
		}
		old := []byte(f.OldBlob)
		new_ := []byte(f.NewBlob)
		if len(old) == 0 || len(new_) == 0 {
			continue
		}
		kind, regs, err := compare(f.Path, old, new_)
		if err != nil || len(regs) == 0 {
			continue
		}
		var tmpl Template
		switch kind {
		case "openapi":
			tmpl = TmplOpenAPICompat
		case "proto":
			tmpl = TmplProtoCompat
		case "asyncapi":
			tmpl = TmplAsyncAPICompat
		default:
			continue
		}
		anchors := make([]ast.LocatorAnchor, 0, len(regs))
		for _, r := range regs {
			anchors = append(anchors, ast.LocatorAnchor{Tag: r.Kind, Name: r.Detail})
		}
		base := f.Path
		if i := strings.LastIndexByte(base, '/'); i != -1 {
			base = base[i+1:]
		}
		sym := ast.Symbol{
			Name:    "Compat" + camelize(base),
			Kind:    ast.KindFunction,
			File:    f.Path,
			Language: "ts",
			Anchors: anchors,
		}
		items = append(items, Item{
			Symbol:   sym,
			Symbols:  []ast.Symbol{sym},
			Template: tmpl,
			OutPath:  "tests/contract/" + sanitizeFilename(base) + ".compat.test.ts",
		})
	}
	return items
}

// CompatComparator classifies a schema file and returns a list of
// regressions (kind + detail). Concrete implementations live in
// cmd/reviewqa so internal/plan stays free of the openapi/proto/
// asyncapi dependencies — this keeps the package graph thin.
type CompatComparator func(path string, old, new_ []byte) (kind string, regressions []CompatRegression, err error)

// CompatRegression is the package-public mirror of compat.Regression.
type CompatRegression struct {
	Kind   string
	Detail string
}

func camelize(s string) string {
	out := []byte{}
	upper := true
	for _, c := range []byte(s) {
		if c == '.' || c == '-' || c == '_' || c == '/' {
			upper = true
			continue
		}
		if upper {
			if c >= 'a' && c <= 'z' {
				c = c - 'a' + 'A'
			}
			upper = false
		}
		out = append(out, c)
	}
	return string(out)
}

func sanitizeFilename(s string) string {
	out := []byte{}
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			out = append(out, c)
		case c == '-', c == '_':
			out = append(out, c)
		default:
			out = append(out, '-')
		}
	}
	collapsed := []byte{}
	prevDash := false
	for _, c := range out {
		if c == '-' {
			if prevDash {
				continue
			}
			prevDash = true
		} else {
			prevDash = false
		}
		collapsed = append(collapsed, c)
	}
	r := string(collapsed)
	r = strings.Trim(r, "-")
	if r == "" {
		return "schema"
	}
	return r
}

func readNew(workDir, rel, fallback string) []byte {
	if fallback != "" {
		return []byte(fallback)
	}
	b, err := os.ReadFile(filepath.Join(workDir, rel))
	if err != nil {
		return nil
	}
	return b
}

func pickTemplate(s ast.Symbol, l Layout) Template {
	// .proto symbols carry their streaming shape via FrameworkHint —
	// route directly to the matching gRPC template before the
	// language switch kicks in.
	switch s.FrameworkHint {
	case "grpc-unary":
		return TmplGRPCUnary
	case "grpc-server-stream":
		return TmplGRPCServerStream
	case "grpc-client-stream":
		return TmplGRPCClientStream
	case "grpc-bidi":
		return TmplGRPCBidi
	}
	switch s.Language {
	case "ts":
		switch s.Kind {
		case ast.KindRoute:
			return TmplJestAPI
		case ast.KindComponent:
			return TmplPlaywrightE2E
		default:
			return TmplJestUnit
		}
	case "python":
		if s.Kind == ast.KindRoute {
			return TmplPytestAPI
		}
		return TmplPytestUnit
	case "go":
		if s.Kind == ast.KindRoute {
			return TmplGoHTTPTest
		}
		return TmplGoUnit
	case "java":
		if s.Kind == ast.KindRoute {
			return TmplJUnit5RestAssured
		}
		return TmplJUnit5Unit
	}
	return TmplJestUnit
}

func testPathFor(s ast.Symbol, t Template, l Layout) string {
	// gRPC-shaped symbols land under tests/grpc/ regardless of source
	// language (the templates emit TypeScript clients).
	switch t {
	case TmplGRPCUnary, TmplGRPCServerStream, TmplGRPCClientStream, TmplGRPCBidi:
		return filepath.ToSlash(filepath.Join("tests", "grpc",
			strings.ToLower(s.Receiver)+"."+strings.ToLower(s.Name)+".test.ts"))
	}
	dir, base := filepath.Split(s.File)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	switch s.Language {
	case "ts":
		return testPathForTS(dir, stem, t, l)
	case "python":
		return filepath.Join("tests", "test_"+stem+".py")
	case "go":
		return filepath.Join(dir, stem+"_test.go")
	case "java":
		return testPathForJava(dir, stem, s.File, l)
	}
	return filepath.Join("tests", stem+".test")
}

func testPathForTS(dir, stem string, t Template, l Layout) string {
	if t == TmplPlaywrightE2E || t == TmplPlaywrightHappyFlow {
		return filepath.Join("tests", "e2e", stem+".spec.ts")
	}
	if l.HasJestDir {
		return filepath.Join(dir, "__tests__", stem+".test.ts")
	}
	if l.HasUnderTest {
		return filepath.Join(dir, stem+".test.ts")
	}
	return filepath.Join("tests", stem+".test.ts")
}

func testPathForJava(dir, stem, file string, l Layout) string {
	if l.HasMavenLayout {
		rel := strings.TrimPrefix(file, "src/main/java/")
		return filepath.Join("src", "test", "java", strings.TrimSuffix(rel, ".java")+"Test.java")
	}
	return filepath.Join(dir, stem+"Test.java")
}
