package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
)

// ProjectListItem is one entry in the project switcher dropdown.
type ProjectListItem struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// ProjectsResponse is the GET /api/projects payload — the current
// workdir plus its sibling reviewqa projects (auto-discovered from
// the filesystem) and the recents list (persisted in Settings).
type ProjectsResponse struct {
	Current  ProjectListItem   `json:"current"`
	Siblings []ProjectListItem `json:"siblings"`
	Recents  []ProjectListItem `json:"recents"`
}

// handleProjects answers GET /api/projects.
func handleProjects(state *workdirState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		current := state.get()
		writeJSON(w, ProjectsResponse{
			Current:  itemFor(current),
			Siblings: siblingProjects(current),
			Recents:  recentProjects(current),
		})
	}
}

// handleSwitchProject answers POST /api/switch-project with body
// `{ path: "/abs/path" }`. Validates the path exists, looks like a
// reviewqa project (or at least a directory), and mutates the
// shared state so the next request sees the new workdir. Also
// pushes the path onto the recents list in Settings.
func handleSwitchProject(state *workdirState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		abs, err := filepath.Abs(body.Path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			http.Error(w, "path is not a directory", http.StatusBadRequest)
			return
		}
		state.set(abs)
		pushRecentProject(abs)
		writeJSON(w, map[string]any{"ok": true, "current": abs})
	}
}

// handleProbeWithState wraps the existing handleProbe(workdir) so
// the probe endpoint always operates against the *current* state,
// not a value captured at server start.
func handleProbeWithState(state *workdirState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleProbe(state.get())(w, r)
	}
}

// looksLikeReviewqaProject reports whether `dir` contains any of the
// signals that suggest a reviewqa-generated workdir. Same heuristic
// the sidebar uses to surface features / docs.
func looksLikeReviewqaProject(dir string) bool {
	checks := []string{
		filepath.Join(dir, "tests", "e2e", "features"),
		filepath.Join(dir, "tests", "e2e", "steps", "reviewqa.steps.ts"),
		filepath.Join(dir, "tests", "e2e", "docs", "summary.html"),
		filepath.Join(dir, "tests", "e2e", "docs", "test-catalogue.md"),
	}
	for _, p := range checks {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	// Also accept any dir with at least one .feature file directly
	// under features/. Glob is the cheapest way to ask.
	if matches, _ := filepath.Glob(filepath.Join(dir, "tests", "e2e", "features", "*.feature")); len(matches) > 0 {
		return true
	}
	return false
}

// siblingProjects scans the parent of `current` for immediate-
// sibling directories that look like reviewqa projects. Returns a
// sorted list (by Name). The current dir itself is excluded.
func siblingProjects(current string) []ProjectListItem {
	parent := filepath.Dir(current)
	if parent == "" || parent == current {
		return nil
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	var out []ProjectListItem
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		full := filepath.Join(parent, e.Name())
		if full == current {
			continue
		}
		if !looksLikeReviewqaProject(full) {
			continue
		}
		out = append(out, ProjectListItem{Name: filepath.Base(full), Path: full})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// recentProjects returns the user's RecentProjects list from
// Settings, excluding `current` (which has its own pill).
func recentProjects(current string) []ProjectListItem {
	s, err := LoadSettings()
	if err != nil {
		return nil
	}
	var out []ProjectListItem
	for _, p := range s.RecentProjects {
		if p == "" || p == current {
			continue
		}
		out = append(out, ProjectListItem{Name: filepath.Base(p), Path: p})
	}
	return out
}

// pushRecentProject moves `path` to the front of the recents list,
// dedupes, and truncates to 8 entries. Best-effort — settings save
// errors are swallowed (the switch already succeeded; recents are a
// nicety, not load-bearing).
func pushRecentProject(path string) {
	s, err := LoadSettings()
	if err != nil {
		s = Settings{}
	}
	out := make([]string, 0, len(s.RecentProjects)+1)
	out = append(out, path)
	for _, p := range s.RecentProjects {
		if p == path || p == "" {
			continue
		}
		out = append(out, p)
		if len(out) >= 8 {
			break
		}
	}
	s.RecentProjects = out
	_ = SaveSettings(s)
}

func itemFor(path string) ProjectListItem {
	return ProjectListItem{Name: filepath.Base(path), Path: path}
}

