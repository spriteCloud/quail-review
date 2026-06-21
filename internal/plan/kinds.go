package plan

import (
	"strings"
)

// Kind constants used for taxonomy filtering and PR-body grouping.
// Keep these stable — they're the user-facing names exposed via the
// --kinds / --exclude-kinds CLI flags and QUAIL_KINDS env. The
// corresponding @kind:* tags inside the generated specs use the same
// strings.
//
// v0.99.
const (
	KindJourney       = "journey"
	KindA11y          = "a11y"
	KindPerf          = "perf"
	KindVisual        = "visual"
	KindSecurity      = "security"
	KindContract      = "contract"
	KindHealth        = "health"
	KindObservability = "observability"
	KindI18n          = "i18n"
	KindNetwork       = "network"
	KindStorage       = "storage"
	KindPrint         = "print"
	KindMobile        = "mobile"
	KindResponsive    = "responsive"
	KindTouch         = "touch"
	KindRace          = "race"
	KindFuzz          = "fuzz"
	KindWebhook       = "webhook"
	KindGraphQL       = "graphql"
	KindAuthExpiry    = "auth-expiry"
	KindHistoryDepth  = "history-depth"
	KindClipboard     = "clipboard"
	KindIframe        = "iframe"
	KindDateEdges     = "date-edges"
	KindFileUpload    = "file-upload"
	KindDeepLink     = "deeplink"
	KindHTTPChains    = "http-chains"
	KindAPI           = "api"
	KindIntegration   = "integration"
	KindGRPC          = "grpc"
	KindCompat        = "compat"
	KindUnit          = "unit"
	KindPWA           = "pwa"
	KindLocale        = "locale"
	KindJobs          = "jobs"

	// Kinds we never filter out — they're structural prerequisites,
	// not user-selectable test families.
	KindScaffold = "scaffold"
	KindDocs     = "docs"
	KindSentinel = "sentinel"
)

// templateKinds is the single source of truth for Template → Kind
// mapping. TestKindOf_CoversEveryTemplate (in plan_test.go) reflects
// over every Tmpl* constant and fails on any unmapped one so adding
// a new Template forces a kind decision.
var templateKinds = map[Template]string{
	// Journeys (user flows)
	TmplPlaywrightE2E:       KindJourney,
	TmplPlaywrightHappyFlow: KindJourney,
	TmplPlaywrightFeature:   KindJourney,
	TmplRNHappyFlow:         KindJourney,
	TmplFlutterHappyFlow:    KindJourney,

	// A11y family
	TmplPlaywrightA11y:          KindA11y,
	TmplPlaywrightA11yLandmarks: KindA11y,
	TmplPlaywrightA11yPrefs:     KindA11y,
	TmplPlaywrightKeyboardNav:   KindA11y,
	TmplPlaywrightZoom:          KindA11y,

	// Single-kind specs
	TmplPlaywrightPerf:              KindPerf,
	TmplPlaywrightVisual:            KindVisual,
	TmplPlaywrightVisualStates:      KindVisual,
	TmplPlaywrightSecurity:          KindSecurity,
	TmplPlaywrightContract:          KindContract,
	TmplPlaywrightHealth:            KindHealth,
	TmplPlaywrightObservability:     KindObservability,
	TmplPlaywrightI18n:              KindI18n,
	TmplPlaywrightNetworkResilience: KindNetwork,
	TmplPlaywrightHTTPChains:        KindHTTPChains,
	TmplPlaywrightStorage:           KindStorage,
	TmplPlaywrightPrint:             KindPrint,
	TmplPlaywrightMobile:            KindMobile,
	TmplPlaywrightResponsive:        KindResponsive,
	TmplPlaywrightTouch:             KindTouch,
	TmplPlaywrightRace:              KindRace,
	TmplPlaywrightFuzz:              KindFuzz,
	TmplPlaywrightWebhook:           KindWebhook,
	TmplPlaywrightWebhookStub:       KindWebhook,
	TmplPlaywrightGraphQL:           KindGraphQL,
	TmplPlaywrightGraphQLStub:       KindGraphQL,
	TmplPlaywrightAuthExpiry:        KindAuthExpiry,
	TmplPlaywrightHistoryDepth:      KindHistoryDepth,
	TmplPlaywrightClipboard:         KindClipboard,
	TmplPlaywrightIframe:            KindIframe,
	TmplPlaywrightDateEdges:         KindDateEdges,
	TmplPlaywrightFileUpload:        KindFileUpload,
	TmplPlaywrightDeepLink:          KindDeepLink,
	TmplPlaywrightPWA:               KindPWA,
	TmplPlaywrightLocaleSwitch:      KindLocale,
	TmplPlaywrightDragDrop:          KindTouch,

	// API + compat
	TmplPlaywrightAPI:                KindAPI,
	TmplPlaywrightIdempotency:        KindAPI,
	TmplPlaywrightPagination:         KindAPI,
	TmplPlaywrightContentNegotiation: KindAPI,
	TmplPlaywrightAuthHeaders:        KindAPI,
	TmplPlaywrightVersioning:         KindAPI,
	TmplOpenAPICompat:                KindCompat,
	TmplProtoCompat:                  KindCompat,
	TmplAsyncAPICompat:               KindCompat,

	// Integration stubs
	TmplPlaywrightIntegrationStub:      KindIntegration,
	TmplPlaywrightIntegrationDBStub:    KindIntegration,
	TmplPlaywrightIntegrationCacheStub: KindIntegration,
	TmplPlaywrightIntegrationObsStub:   KindIntegration,
	TmplPlaywrightIntegrationAuthStub:  KindIntegration,
	TmplIntegrationDB:                  KindIntegration,
	TmplIntegrationBroker:              KindIntegration,
	TmplIntegrationCache:               KindIntegration,
	TmplIntegrationStorage:             KindIntegration,
	TmplIntegrationSearch:              KindIntegration,
	TmplIntegrationAuth:                KindIntegration,
	TmplIntegrationContainers:          KindIntegration,
	TmplIntegrationCompose:             KindIntegration,

	// gRPC
	TmplGRPCUnary:        KindGRPC,
	TmplGRPCServerStream: KindGRPC,
	TmplGRPCClientStream: KindGRPC,
	TmplGRPCBidi:         KindGRPC,

	// Data-quality / schema-conformance templates.
	TmplDbtSchema:          KindContract,
	TmplPanderaConformance: KindContract,
	TmplGreatExpectations:  KindContract,

	// Unit / aspect / job templates — non-Playwright, kept as-is.
	TmplJestUnit:            KindUnit,
	TmplJestAPI:             KindAPI,
	TmplJestProperty:        KindUnit,
	TmplJestSerialization:   KindUnit,
	TmplJestValidatorPos:    KindUnit,
	TmplJestStore:           KindUnit,
	TmplJestConstructor:     KindUnit,
	TmplPytestUnit:          KindUnit,
	TmplPytestAPI:           KindAPI,
	TmplPytestProperty:      KindUnit,
	TmplPytestSerialization: KindUnit,
	TmplPytestValidatorPos:  KindUnit,
	TmplPytestConstructor:   KindUnit,
	TmplGoUnit:              KindUnit,
	TmplGoHTTPTest:          KindAPI,
	TmplJUnit5Unit:          KindUnit,
	TmplJUnit5RestAssured:   KindAPI,
	TmplScheduledJob:        KindJobs,
	TmplEventHandler:        KindJobs,
	TmplEmailTemplate:       KindJobs,

	// Project scaffolding — never filtered.
	TmplPlaywrightFixtures: KindScaffold,
	TmplPlaywrightConfig:   KindScaffold,
	TmplPlaywrightReadme:   KindScaffold,
	TmplPlaywrightPackage:  KindScaffold,
	TmplPlaywrightTsconfig: KindScaffold,
	TmplPlaywrightCIFile:   KindScaffold,
	TmplPlaywrightSteps:    KindScaffold,
	TmplPlaywrightStepsBDD: KindScaffold,
	TmplPlaywrightFindings: KindScaffold,
	TmplRaw:                KindScaffold,

	// Documentation
	TmplPlaywrightCatalogue: KindDocs,
	TmplPlaywrightSummary:   KindDocs,

	// Sentinels track findings; not user-filterable.
	TmplPlaywrightSentinel: KindSentinel,
}

// KindOf returns the user-facing kind for a Template. Unknown
// templates default to "unknown" so the caller still gets a usable
// label even if we add a new Template constant and forget to map it.
// The exhaustiveness test in plan_test.go is the real guard.
func KindOf(t Template) string {
	if k, ok := templateKinds[t]; ok {
		return k
	}
	return "unknown"
}

// FilterByKinds drops items whose KindOf is not in `allow`. When
// `allow` is empty / nil, no allow-list filter applies (everything
// passes the first gate). `deny` is applied after: an item whose
// KindOf is in deny is dropped regardless of allow.
//
// Items in the "always-keep" set (KindScaffold, KindDocs,
// KindSentinel) are never dropped by either list — they're
// structural prerequisites, not user-selectable test families.
//
// v0.99.
func FilterByKinds(items []Item, allow, deny []string) []Item {
	allowSet := setOf(allow)
	denySet := setOf(deny)
	if len(allowSet) == 0 && len(denySet) == 0 {
		return items
	}
	out := items[:0:0]
	for _, it := range items {
		k := KindOf(it.Template)
		if isAlwaysKeep(k) {
			out = append(out, it)
			continue
		}
		if len(allowSet) > 0 && !allowSet[k] {
			continue
		}
		if denySet[k] {
			continue
		}
		out = append(out, it)
	}
	return out
}

func isAlwaysKeep(k string) bool {
	return k == KindScaffold || k == KindDocs || k == KindSentinel
}

func setOf(parts []string) map[string]bool {
	m := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			m[strings.ToLower(p)] = true
		}
	}
	return m
}

// ParseKinds splits a comma-separated kind list into a normalised
// slice. Empty input returns nil so callers can distinguish "no
// filter" from "filter to nothing".
func ParseKinds(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, strings.ToLower(p))
		}
	}
	return out
}
