package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// fixtureProjectAt writes a minimal reviewqa-shaped tree under `root`
// and returns root. Used by switcher / sibling-discovery tests.
func fixtureProjectAt(t *testing.T, root string) string {
	t.Helper()
	mustWrite(t, filepath.Join(root, "tests", "e2e", "features", "demo.feature"), sampleFeature)
	mustWrite(t, filepath.Join(root, "tests", "e2e", "steps", "reviewqa.steps.ts"), sampleSteps)
	return root
}

func TestLooksLikeReviewqaProject(t *testing.T) {
	// A tmp dir with the features dir → yes.
	yes := t.TempDir()
	fixtureProjectAt(t, yes)
	if !looksLikeReviewqaProject(yes) {
		t.Errorf("fixture project should look like reviewqa")
	}
	// An empty dir → no.
	no := t.TempDir()
	if looksLikeReviewqaProject(no) {
		t.Errorf("empty dir should NOT look like reviewqa")
	}
}

func TestSiblingProjects_DiscoversReviewqaPeers(t *testing.T) {
	parent := t.TempDir()
	a := filepath.Join(parent, "project-a")
	b := filepath.Join(parent, "project-b")
	c := filepath.Join(parent, "project-c") // not a reviewqa project
	for _, p := range []string{a, b, c} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fixtureProjectAt(t, a)
	fixtureProjectAt(t, b)

	got := siblingProjects(a)
	if len(got) != 1 || got[0].Path != b {
		t.Errorf("siblings = %+v, want [project-b]", got)
	}
}

func TestProjectsEndpoint_ReturnsCurrent(t *testing.T) {
	useTempSettings(t)
	root := fixtureProject(t)
	workdir := filepath.Join(root, "tests", "e2e")
	srv := httptest.NewServer(Handler(workdir))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/projects")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var got ProjectsResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Current.Path != workdir {
		t.Errorf("current = %q, want %q", got.Current.Path, workdir)
	}
}

func TestSwitchProject_RejectsMissingDir(t *testing.T) {
	useTempSettings(t)
	root := fixtureProject(t)
	workdir := filepath.Join(root, "tests", "e2e")
	srv := httptest.NewServer(Handler(workdir))
	defer srv.Close()

	body := []byte(`{"path":"/does/not/exist-` + t.Name() + `"}`)
	resp, err := http.Post(srv.URL+"/api/switch-project", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d, want 400", resp.StatusCode)
	}
}

func TestSwitchProject_AcceptsValidDir(t *testing.T) {
	useTempSettings(t)
	root := fixtureProject(t)
	workdir := filepath.Join(root, "tests", "e2e")
	other := t.TempDir()
	srv := httptest.NewServer(Handler(workdir))
	defer srv.Close()

	body, _ := json.Marshal(map[string]string{"path": other})
	resp, err := http.Post(srv.URL+"/api/switch-project", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	// /api/project now returns the new workdir.
	pr, err := http.Get(srv.URL + "/api/project")
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Body.Close()
	var p Project
	_ = json.NewDecoder(pr.Body).Decode(&p)
	if p.Workdir != other {
		t.Errorf("project.Workdir = %q, want %q", p.Workdir, other)
	}
	// Recents persisted.
	s, _ := LoadSettings()
	if len(s.RecentProjects) == 0 || s.RecentProjects[0] != other {
		t.Errorf("recents not pushed: %v", s.RecentProjects)
	}
}

func TestPushRecentProject_DedupesAndTruncates(t *testing.T) {
	useTempSettings(t)
	for i := 0; i < 12; i++ {
		pushRecentProject(filepath.Join(t.TempDir(), "p"))
	}
	s, _ := LoadSettings()
	if len(s.RecentProjects) > 8 {
		t.Errorf("recents = %d, want <= 8", len(s.RecentProjects))
	}
}
