package serve

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

// Options configures a single serve session.
type Options struct {
	Workdir   string // root of the reviewqa-generated project to load
	Addr      string // listen address; default 127.0.0.1:8765
	NoBrowser bool   // skip the auto-open
	Logf      func(format string, args ...any)
}

// Run starts the server on Options.Addr and blocks until ctx is
// cancelled (Ctrl-C handled by the caller). Returns the first listen
// error, or nil on graceful shutdown.
func Run(ctx context.Context, opts Options) error {
	if opts.Addr == "" {
		opts.Addr = "127.0.0.1:8765"
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	workdir, err := filepath.Abs(opts.Workdir)
	if err != nil {
		return fmt.Errorf("serve: resolve workdir: %w", err)
	}
	if info, err := os.Stat(workdir); err != nil || !info.IsDir() {
		return fmt.Errorf("serve: workdir %q is not a directory", workdir)
	}
	handler := Handler(workdir)
	srv := &http.Server{
		Addr:              opts.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	url := "http://" + opts.Addr + "/"
	opts.Logf("reviewqa serve listening on %s (workdir %s)", url, workdir)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	if !opts.NoBrowser {
		go openBrowser(url, opts.Logf)
	}
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// Handler returns the mux for the given workdir. Exposed for tests via
// httptest so the JSON contract can be asserted without a live socket.
func Handler(workdir string) http.Handler {
	mux := http.NewServeMux()

	assets, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(fmt.Sprintf("serve: embed sub-fs: %v", err))
	}
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		serveStatic(w, "web/index.html", "text/html; charset=utf-8")
	}))

	mux.HandleFunc("/api/project", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, loadProject(workdir))
	})
	mux.HandleFunc("/api/feature", func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("path")
		path, ok := safeJoin(workdir, rel)
		if !ok {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		feat, err := ParseFeatureFile(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		raw, _ := os.ReadFile(path)
		feat.Path = rel
		writeJSON(w, map[string]any{
			"feature": feat,
			"gherkin": string(raw),
		})
	})
	mux.HandleFunc("/api/steps", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, loadSteps(workdir))
	})
	mux.HandleFunc("/api/doc", func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("path")
		path, ok := safeJoin(workdir, rel)
		if !ok {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		b, err := os.ReadFile(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]string{"path": rel, "body": string(b)})
	})

	mux.HandleFunc("/api/scenario", func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("feature")
		name := r.URL.Query().Get("name")
		path, ok := safeJoin(workdir, rel)
		if !ok {
			http.Error(w, "invalid feature path", http.StatusBadRequest)
			return
		}
		history := historyRootFor(workdir)
		switch r.Method {
		case http.MethodDelete:
			if name == "" {
				http.Error(w, "missing name", http.StatusBadRequest)
				return
			}
			n, err := DeleteScenario(path, name, history)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, map[string]any{"deleted": n})
		case http.MethodPatch:
			if name == "" {
				http.Error(w, "missing name", http.StatusBadRequest)
				return
			}
			body, err := readJSONBody(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			newName, err := ReplaceScenario(path, name, body.Gherkin, history)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, map[string]any{"name": newName})
		case http.MethodPost:
			body, err := readJSONBody(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			newName, err := AppendScenario(path, body.Gherkin, history)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, map[string]any{"name": newName})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/validate-scenario", func(w http.ResponseWriter, r *http.Request) {
		body, err := readJSONBody(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := validateScenarioBlock(body.Gherkin); err != nil {
			writeJSON(w, map[string]any{"valid": false, "error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"valid": true})
	})

	mux.HandleFunc("/api/probe-dom", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			URL  string `json:"url"`
			Base string `json:"base,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		target, err := resolveTarget(body.URL, body.Base)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		lm, err := FetchAndParseDOM(ctx, target)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, lm)
	})

	mux.HandleFunc("/api/locator-candidates", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			URL  string `json:"url"`
			Base string `json:"base,omitempty"`
			Kind string `json:"kind"`
			Hint string `json:"hint"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		target, err := resolveTarget(body.URL, body.Base)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		lm, err := FetchAndParseDOM(ctx, target)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, map[string]any{
			"url":        lm.URL,
			"candidates": RankLocators(lm, body.Kind, body.Hint),
		})
	})

	return localOnly(mux)
}

type scenarioRequest struct {
	Gherkin string `json:"gherkin"`
}

func readJSONBody(r *http.Request) (scenarioRequest, error) {
	var body scenarioRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return body, fmt.Errorf("body: %w", err)
	}
	return body, nil
}

// localOnly rejects requests that don't come from a loopback address.
// Belt-and-suspenders alongside binding to 127.0.0.1 — if a caller asks
// us to bind to 0.0.0.0 (we don't expose a flag for that yet, but) the
// API stays inert for non-loopback clients.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.RemoteAddr
		if i := strings.LastIndex(host, ":"); i >= 0 {
			host = host[:i]
		}
		host = strings.Trim(host, "[]")
		if host != "127.0.0.1" && host != "::1" && host != "localhost" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func serveStatic(w http.ResponseWriter, name, contentType string) {
	b, err := webFS.ReadFile(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(b)
}

// safeJoin resolves rel against workdir, returning the absolute path
// and a bool flagging whether the resolved path stays inside workdir.
// Defends against `../` escape attempts.
func safeJoin(workdir, rel string) (string, bool) {
	if rel == "" {
		return "", false
	}
	abs := filepath.Clean(filepath.Join(workdir, rel))
	wabs, _ := filepath.Abs(workdir)
	if !strings.HasPrefix(abs, wabs+string(filepath.Separator)) && abs != wabs {
		return "", false
	}
	return abs, true
}

// Project is the top-level shape returned by GET /api/project.
type Project struct {
	Name     string       `json:"name"`
	Workdir  string       `json:"workdir"`
	Features []FeatureRef `json:"features"`
	Docs     []DocRef     `json:"docs,omitempty"`
}

// FeatureRef is a shallow reference to one .feature file — name + tags
// + scenario count, NOT the full parse. The UI uses this for the
// sidebar; full parse loads lazily via /api/feature.
type FeatureRef struct {
	Path      string   `json:"path"`
	Name      string   `json:"name"`
	Tags      []string `json:"tags,omitempty"`
	Scenarios int      `json:"scenarios"`
}

// DocRef is a stakeholder doc (catalogue / summary / findings).
type DocRef struct {
	Path  string `json:"path"`
	Title string `json:"title"`
	Kind  string `json:"kind"` // catalogue | summary | findings | other
}

func loadProject(workdir string) Project {
	p := Project{
		Name:    filepath.Base(workdir),
		Workdir: workdir,
	}
	featuresDir := filepath.Join(workdir, "tests", "e2e", "features")
	if entries, err := os.ReadDir(featuresDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".feature") {
				continue
			}
			path := filepath.Join(featuresDir, e.Name())
			rel, _ := filepath.Rel(workdir, path)
			feat, err := ParseFeatureFile(path)
			if err != nil {
				continue
			}
			p.Features = append(p.Features, FeatureRef{
				Path:      filepath.ToSlash(rel),
				Name:      feat.Name,
				Tags:      feat.Tags,
				Scenarios: len(feat.Scenarios),
			})
		}
	}
	sort.Slice(p.Features, func(i, j int) bool { return p.Features[i].Path < p.Features[j].Path })

	docsDir := filepath.Join(workdir, "tests", "e2e", "docs")
	for _, d := range []struct{ name, kind, title string }{
		{"test-catalogue.md", "catalogue", "Test catalogue"},
		{"summary.html", "summary", "Stakeholder summary"},
		{"findings.md", "findings", "Bug-discovery ledger"},
	} {
		path := filepath.Join(docsDir, d.name)
		if _, err := os.Stat(path); err == nil {
			rel, _ := filepath.Rel(workdir, path)
			p.Docs = append(p.Docs, DocRef{Path: filepath.ToSlash(rel), Kind: d.kind, Title: d.title})
		}
	}
	return p
}

func loadSteps(workdir string) []StepPattern {
	path := filepath.Join(workdir, "tests", "e2e", "steps", "reviewqa.steps.ts")
	pats, err := ParseStepsFile(path)
	if err != nil {
		return nil
	}
	return pats
}

// openBrowser best-effort opens the URL in the user's default browser.
// Logs and moves on if the underlying command fails — the URL is
// printed to stderr regardless.
func openBrowser(url string, logf func(string, ...any)) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		logf("serve: open browser: %v (visit %s manually)", err, url)
	}
}
