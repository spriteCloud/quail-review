package plan

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestKindOf_CoversEveryTemplate parses plan.go and asserts every
// declared Tmpl* constant has a templateKinds entry. Adding a new
// Template without mapping it to a kind fails the test loudly so the
// taxonomy filter (--kinds) doesn't silently drop the new family.
//
// v0.99.
func TestKindOf_CoversEveryTemplate(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "plan.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse plan.go: %v", err)
	}
	var missing []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Tmpl") {
					continue
				}
				// Look up via the runtime-visible constant value: name
				// resolves to a Template string. We can't reflect into
				// an unexported package map from a test in the same
				// package directly via interface{}; instead we look up
				// by the value string the source assigns.
				tmpl := tmplFromConstName(name.Name)
				if tmpl == "" {
					t.Errorf("could not resolve constant %s to a Template value", name.Name)
					continue
				}
				if _, ok := templateKinds[tmpl]; !ok {
					missing = append(missing, name.Name)
				}
			}
		}
	}
	if len(missing) > 0 {
		t.Errorf("templateKinds missing entries for: %v", missing)
	}
}

// tmplFromConstName looks up the runtime value of a Template constant
// by its identifier. Listed explicitly so adding a new Tmpl* constant
// also requires touching this list — which forces the kind decision
// when the new constant is added.
func tmplFromConstName(name string) Template {
	m := map[string]Template{
		"TmplJestUnit":                       TmplJestUnit,
		"TmplJestAPI":                        TmplJestAPI,
		"TmplPlaywrightE2E":                  TmplPlaywrightE2E,
		"TmplPlaywrightHappyFlow":            TmplPlaywrightHappyFlow,
		"TmplPlaywrightFixtures":             TmplPlaywrightFixtures,
		"TmplPlaywrightConfig":               TmplPlaywrightConfig,
		"TmplPlaywrightReadme":               TmplPlaywrightReadme,
		"TmplPlaywrightPackage":              TmplPlaywrightPackage,
		"TmplPlaywrightTsconfig":             TmplPlaywrightTsconfig,
		"TmplPlaywrightCIFile":               TmplPlaywrightCIFile,
		"TmplPlaywrightFuzz":                 TmplPlaywrightFuzz,
		"TmplPlaywrightFeature":              TmplPlaywrightFeature,
		"TmplPlaywrightSteps":                TmplPlaywrightSteps,
		"TmplPlaywrightCatalogue":            TmplPlaywrightCatalogue,
		"TmplPlaywrightSummary":              TmplPlaywrightSummary,
		"TmplPlaywrightAPI":                  TmplPlaywrightAPI,
		"TmplPlaywrightFindings":             TmplPlaywrightFindings,
		"TmplPlaywrightStepsBDD":             TmplPlaywrightStepsBDD,
		"TmplPlaywrightA11y":                 TmplPlaywrightA11y,
		"TmplPlaywrightResponsive":           TmplPlaywrightResponsive,
		"TmplPlaywrightPerf":                 TmplPlaywrightPerf,
		"TmplPlaywrightSecurity":             TmplPlaywrightSecurity,
		"TmplPlaywrightHealth":               TmplPlaywrightHealth,
		"TmplPlaywrightContract":             TmplPlaywrightContract,
		"TmplPlaywrightObservability":        TmplPlaywrightObservability,
		"TmplPlaywrightI18n":                 TmplPlaywrightI18n,
		"TmplJestProperty":                   TmplJestProperty,
		"TmplJestSerialization":              TmplJestSerialization,
		"TmplJestValidatorPos":               TmplJestValidatorPos,
		"TmplPytestProperty":                 TmplPytestProperty,
		"TmplPytestSerialization":            TmplPytestSerialization,
		"TmplPytestValidatorPos":             TmplPytestValidatorPos,
		"TmplPlaywrightVisual":               TmplPlaywrightVisual,
		"TmplPlaywrightGraphQL":              TmplPlaywrightGraphQL,
		"TmplPlaywrightWebhook":              TmplPlaywrightWebhook,
		"TmplGRPCUnary":                      TmplGRPCUnary,
		"TmplGRPCServerStream":               TmplGRPCServerStream,
		"TmplGRPCClientStream":               TmplGRPCClientStream,
		"TmplGRPCBidi":                       TmplGRPCBidi,
		"TmplPlaywrightIdempotency":          TmplPlaywrightIdempotency,
		"TmplPlaywrightPagination":           TmplPlaywrightPagination,
		"TmplPlaywrightContentNegotiation":   TmplPlaywrightContentNegotiation,
		"TmplPlaywrightAuthHeaders":          TmplPlaywrightAuthHeaders,
		"TmplPlaywrightVersioning":           TmplPlaywrightVersioning,
		"TmplOpenAPICompat":                  TmplOpenAPICompat,
		"TmplProtoCompat":                    TmplProtoCompat,
		"TmplAsyncAPICompat":                 TmplAsyncAPICompat,
		"TmplJestStore":                      TmplJestStore,
		"TmplJestConstructor":                TmplJestConstructor,
		"TmplPytestConstructor":              TmplPytestConstructor,
		"TmplScheduledJob":                   TmplScheduledJob,
		"TmplEventHandler":                   TmplEventHandler,
		"TmplEmailTemplate":                  TmplEmailTemplate,
		"TmplIntegrationDB":                  TmplIntegrationDB,
		"TmplIntegrationBroker":              TmplIntegrationBroker,
		"TmplIntegrationCache":               TmplIntegrationCache,
		"TmplIntegrationStorage":             TmplIntegrationStorage,
		"TmplIntegrationSearch":              TmplIntegrationSearch,
		"TmplIntegrationAuth":                TmplIntegrationAuth,
		"TmplIntegrationContainers":          TmplIntegrationContainers,
		"TmplIntegrationCompose":             TmplIntegrationCompose,
		"TmplPlaywrightMobile":               TmplPlaywrightMobile,
		"TmplPlaywrightDeepLink":             TmplPlaywrightDeepLink,
		"TmplRNHappyFlow":                    TmplRNHappyFlow,
		"TmplFlutterHappyFlow":               TmplFlutterHappyFlow,
		"TmplPlaywrightVisualStates":         TmplPlaywrightVisualStates,
		"TmplPlaywrightKeyboardNav":          TmplPlaywrightKeyboardNav,
		"TmplPlaywrightA11yLandmarks":        TmplPlaywrightA11yLandmarks,
		"TmplPlaywrightSentinel":             TmplPlaywrightSentinel,
		"TmplPlaywrightNetworkResilience":    TmplPlaywrightNetworkResilience,
		"TmplPlaywrightRace":                 TmplPlaywrightRace,
		"TmplPlaywrightStorage":              TmplPlaywrightStorage,
		"TmplPlaywrightZoom":                 TmplPlaywrightZoom,
		"TmplPlaywrightA11yPrefs":            TmplPlaywrightA11yPrefs,
		"TmplPlaywrightPrint":                TmplPlaywrightPrint,
		"TmplPlaywrightClipboard":            TmplPlaywrightClipboard,
		"TmplPlaywrightHTTPChains":           TmplPlaywrightHTTPChains,
		"TmplPlaywrightIntegrationStub":      TmplPlaywrightIntegrationStub,
		"TmplPlaywrightFileUpload":           TmplPlaywrightFileUpload,
		"TmplPlaywrightIframe":               TmplPlaywrightIframe,
		"TmplPlaywrightDateEdges":            TmplPlaywrightDateEdges,
		"TmplPlaywrightPWA":                  TmplPlaywrightPWA,
		"TmplPlaywrightHistoryDepth":         TmplPlaywrightHistoryDepth,
		"TmplPlaywrightTouch":                TmplPlaywrightTouch,
		"TmplPlaywrightDragDrop":             TmplPlaywrightDragDrop,
		"TmplPlaywrightAuthExpiry":           TmplPlaywrightAuthExpiry,
		"TmplPlaywrightLocaleSwitch":         TmplPlaywrightLocaleSwitch,
		"TmplPlaywrightGraphQLStub":          TmplPlaywrightGraphQLStub,
		"TmplPlaywrightWebhookStub":          TmplPlaywrightWebhookStub,
		"TmplPlaywrightIntegrationDBStub":    TmplPlaywrightIntegrationDBStub,
		"TmplPlaywrightIntegrationCacheStub": TmplPlaywrightIntegrationCacheStub,
		"TmplPlaywrightIntegrationObsStub":   TmplPlaywrightIntegrationObsStub,
		"TmplPlaywrightIntegrationAuthStub":  TmplPlaywrightIntegrationAuthStub,
		"TmplRaw":                            TmplRaw,
		"TmplPytestUnit":                     TmplPytestUnit,
		"TmplPytestAPI":                      TmplPytestAPI,
		"TmplGoUnit":                         TmplGoUnit,
		"TmplGoHTTPTest":                     TmplGoHTTPTest,
		"TmplJUnit5Unit":                     TmplJUnit5Unit,
		"TmplJUnit5RestAssured":              TmplJUnit5RestAssured,
		"TmplDbtSchema":                      TmplDbtSchema,
		"TmplPanderaConformance":             TmplPanderaConformance,
		"TmplGreatExpectations":              TmplGreatExpectations,
	}
	return m[name]
}

func TestFilterByKinds_AllowList(t *testing.T) {
	items := []Item{
		{Template: TmplPlaywrightA11y},
		{Template: TmplPlaywrightPerf},
		{Template: TmplPlaywrightVisual},
	}
	got := FilterByKinds(items, []string{"a11y", "perf"}, nil)
	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d: %+v", len(got), got)
	}
	for _, it := range got {
		k := KindOf(it.Template)
		if k != KindA11y && k != KindPerf {
			t.Errorf("unexpected kind %q passed allow-list", k)
		}
	}
}

func TestFilterByKinds_DenyList(t *testing.T) {
	items := []Item{
		{Template: TmplPlaywrightA11y},
		{Template: TmplPlaywrightVisual},
		{Template: TmplPlaywrightTouch},
	}
	got := FilterByKinds(items, nil, []string{"visual", "touch"})
	if len(got) != 1 || KindOf(got[0].Template) != KindA11y {
		t.Errorf("expected only a11y to survive deny; got %+v", got)
	}
}

func TestFilterByKinds_NeverDropsScaffoldOrDocs(t *testing.T) {
	items := []Item{
		{Template: TmplPlaywrightConfig},    // scaffold
		{Template: TmplPlaywrightCatalogue}, // docs
		{Template: TmplPlaywrightSentinel},  // sentinel
		{Template: TmplPlaywrightA11y},      // a11y
		{Template: TmplPlaywrightVisual},    // visual
	}
	// Allow-list says perf only — but scaffold/docs/sentinel must
	// pass anyway.
	got := FilterByKinds(items, []string{"perf"}, []string{"a11y", "visual"})
	if len(got) != 3 {
		t.Fatalf("expected 3 always-keep items, got %d: %+v", len(got), got)
	}
	for _, it := range got {
		k := KindOf(it.Template)
		if !isAlwaysKeep(k) {
			t.Errorf("non-always-keep kind %q survived filters", k)
		}
	}
}

func TestFilterByKinds_EmptyAllowAndDenyPassesAll(t *testing.T) {
	items := []Item{
		{Template: TmplPlaywrightA11y},
		{Template: TmplPlaywrightVisual},
	}
	got := FilterByKinds(items, nil, nil)
	if len(got) != len(items) {
		t.Errorf("empty filters should pass everything; got %d/%d", len(got), len(items))
	}
}

func TestParseKinds(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{"a11y", []string{"a11y"}},
		{"a11y,perf,visual", []string{"a11y", "perf", "visual"}},
		{" a11y , Perf ,  ", []string{"a11y", "perf"}},
	}
	for _, tc := range cases {
		got := ParseKinds(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("ParseKinds(%q) length = %d, want %d (%v vs %v)", tc.in, len(got), len(tc.want), got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ParseKinds(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}
