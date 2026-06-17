package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureProject sets up a minimal reviewqa-shaped project tree under
// t.TempDir() and returns its absolute path.
func fixtureProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "tests", "e2e", "features", "demo.feature"), sampleFeature)
	mustWrite(t, filepath.Join(root, "tests", "e2e", "steps", "reviewqa.steps.ts"), sampleSteps)
	mustWrite(t, filepath.Join(root, "tests", "e2e", "docs", "test-catalogue.md"), "# Catalogue\n\nDemo content.\n")
	mustWrite(t, filepath.Join(root, "tests", "e2e", "docs", "findings.md"), "# Findings\n\nNone yet.\n")
	return root
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHandler_ProjectEndpoint(t *testing.T) {
	root := fixtureProject(t)
	srv := httptest.NewServer(Handler(root))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/project")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: got %d", resp.StatusCode)
	}
	var p Project
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(p.Features) != 1 {
		t.Errorf("features: got %d want 1", len(p.Features))
	}
	if len(p.Features) > 0 && p.Features[0].Scenarios != 2 {
		t.Errorf("scenario count: got %d want 2", p.Features[0].Scenarios)
	}
	if len(p.Docs) != 2 {
		t.Errorf("docs: got %d want 2 (catalogue + findings)", len(p.Docs))
	}
}

func TestHandler_FeatureEndpoint(t *testing.T) {
	root := fixtureProject(t)
	srv := httptest.NewServer(Handler(root))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/feature?path=tests/e2e/features/demo.feature")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: got %d", resp.StatusCode)
	}
	var body struct {
		Feature Feature `json:"feature"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Feature.Name == "" {
		t.Errorf("feature name empty")
	}
}

func TestHandler_PathTraversalRejected(t *testing.T) {
	root := fixtureProject(t)
	srv := httptest.NewServer(Handler(root))
	defer srv.Close()

	// "../" escape should be rejected; the path is read-only but we
	// still don't want to confirm presence of files outside the
	// workdir.
	resp, err := http.Get(srv.URL + "/api/feature?path=../../etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
		t.Errorf("path traversal status: got %d want 400 or 404", resp.StatusCode)
	}
}

func TestHandler_StepsEndpoint(t *testing.T) {
	root := fixtureProject(t)
	srv := httptest.NewServer(Handler(root))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/steps")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var pats []StepPattern
	if err := json.NewDecoder(resp.Body).Decode(&pats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(pats) != 4 {
		t.Errorf("expected 4 step patterns, got %d", len(pats))
	}
}

func TestHandler_IndexServed(t *testing.T) {
	root := fixtureProject(t)
	srv := httptest.NewServer(Handler(root))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	if !strings.Contains(string(body[:n]), "reviewqa") {
		t.Errorf("index missing brand: got %q", body[:n])
	}
}
