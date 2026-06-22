package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/plan"
)

// TestRunAll_EmitsFeatureFilesAndBDDSteps proves the v0.21 inversion:
// journeys ship as .feature files (not .spec.ts) and the suite includes
// the playwright-bdd step-definitions companion.
func TestRunAll_EmitsFeatureFilesAndBDDSteps(t *testing.T) {
	t.Setenv("QUAIL_PROBE_ALLOW_LOOPBACK", "1")
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<html><body><h1>Home</h1><a href="/contact">Contact</a></body></html>`))
	})
	mux.HandleFunc("/contact", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><h1>Contact</h1></body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	items, _ := RunAll(context.Background(), []string{srv.URL + "/"})

	gotFeatureFiles, gotSpecTs, gotStepsFile := 0, 0, false
	for _, it := range items {
		switch {
		case strings.HasSuffix(it.OutPath, ".feature"):
			gotFeatureFiles++
			if it.Template != plan.TmplPlaywrightFeature {
				t.Errorf("feature file %s should use TmplPlaywrightFeature; got %s", it.OutPath, it.Template)
			}
		case strings.HasPrefix(it.OutPath, "tests/e2e/") &&
			strings.HasSuffix(it.OutPath, ".spec.ts") &&
			!strings.Contains(it.OutPath, "/api/") &&
			!strings.HasSuffix(it.OutPath, "-fuzz.spec.ts") &&
			// v0.22: the quality-taxonomy companions land under named
			// subdirectories (a11y/, responsive/, perf/, security/,
			// health/, observability/, contract/, i18n/). Not happy-flow
			// journeys, so exclude them from this guard.
			!strings.Contains(it.OutPath, "/a11y/") &&
			!strings.Contains(it.OutPath, "/responsive/") &&
			!strings.Contains(it.OutPath, "/perf/") &&
			!strings.Contains(it.OutPath, "/security/") &&
			!strings.Contains(it.OutPath, "/health/") &&
			!strings.Contains(it.OutPath, "/observability/") &&
			!strings.Contains(it.OutPath, "/contract/") &&
			!strings.Contains(it.OutPath, "/i18n/") &&
			// v0.23: additional quality-companion subdirs.
			!strings.Contains(it.OutPath, "/visual/") &&
			!strings.Contains(it.OutPath, "/webhooks/") &&
			// v0.42: edge-case quality-companion subdirs.
			!strings.Contains(it.OutPath, "/network/") &&
			!strings.Contains(it.OutPath, "/storage/") &&
			!strings.Contains(it.OutPath, "/race/") &&
			!strings.Contains(it.OutPath, "/clipboard/") &&
			!strings.Contains(it.OutPath, "/print/") &&
			!strings.Contains(it.OutPath, "/http-chains/") &&
			// v0.43: taxonomy-restoration quality-companion subdirs.
			!strings.Contains(it.OutPath, "/mobile/") &&
			!strings.Contains(it.OutPath, "/integration/") &&
			// v0.44: additional gated edge quality-companion subdirs.
			!strings.Contains(it.OutPath, "/file-upload/") &&
			!strings.Contains(it.OutPath, "/iframe/") &&
			!strings.Contains(it.OutPath, "/date-edges/") &&
			!strings.Contains(it.OutPath, "/pwa/") &&
			!strings.Contains(it.OutPath, "/history-depth/") &&
			// v0.45: touch / dragdrop / auth-expiry quality-companion subdirs.
			!strings.Contains(it.OutPath, "/touch/") &&
			!strings.Contains(it.OutPath, "/dragdrop/") &&
			!strings.Contains(it.OutPath, "/auth-expiry/") &&
			// v0.49: graphql / webhook always-attempt stub subdirs.
			!strings.Contains(it.OutPath, "/graphql/") &&
			!strings.Contains(it.OutPath, "/webhook/") &&
			// v0.57: per-kind integration scaffold subdirs.
			!strings.Contains(it.OutPath, "/integration-db/") &&
			!strings.Contains(it.OutPath, "/integration-cache/") &&
			!strings.Contains(it.OutPath, "/integration-obs/") &&
			!strings.Contains(it.OutPath, "/integration-auth/"):
			gotSpecTs++
		case it.OutPath == "tests/e2e/steps/quail.steps.ts":
			gotStepsFile = true
		}
	}
	if gotFeatureFiles == 0 {
		t.Errorf("expected ≥1 .feature journey file; got 0")
	}
	if gotSpecTs > 0 {
		t.Errorf("v0.21 should NOT emit happy-flow .spec.ts files; got %d", gotSpecTs)
	}
	if !gotStepsFile {
		t.Errorf("expected tests/e2e/steps/quail.steps.ts companion to be emitted")
	}
}
