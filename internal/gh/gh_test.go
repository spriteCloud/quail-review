package gh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-github/v66/github"
	"github.com/spriteCloud/quail-core/config"
)

func TestIsAlreadyExists(t *testing.T) {
	if isAlreadyExists(nil) {
		t.Error("nil should not be 'already exists'")
	}
	if !isAlreadyExists(errors.New("git refs already exists: bla")) {
		t.Error("string-match err should be 'already exists'")
	}
	if isAlreadyExists(errors.New("totally unrelated")) {
		t.Error("unrelated err should not be 'already exists'")
	}
	resp := &github.ErrorResponse{
		Response: &http.Response{StatusCode: http.StatusUnprocessableEntity},
		Message:  "Reference already exists",
	}
	if !isAlreadyExists(resp) {
		t.Error("422 + Reference already exists should match")
	}
}

func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := config.Config{
		GitHubToken: "test-token",
		Repo:        "acme/widget",
	}
	c, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	u, _ := url.Parse(srv.URL + "/")
	c.api.BaseURL = u
	return c, srv
}

func TestFetchDiff(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget/pulls/7", func(w http.ResponseWriter, r *http.Request) {
		if accept := r.Header.Get("Accept"); strings.Contains(accept, "diff") {
			w.Header().Set("Content-Type", "application/vnd.github.diff")
			fmt.Fprint(w, "diff --git a/x.go b/x.go\n+added\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 7,
			"title":  "feat: thing",
			"head":   map[string]any{"ref": "feature", "sha": "abcdef1234567890"},
			"base":   map[string]any{"ref": "main", "sha": "0000000000000000"},
		})
	})
	c, _ := newTestClient(t, mux)
	raw, pr, err := c.FetchDiff(context.Background(), 7)
	if err != nil {
		t.Fatalf("FetchDiff: %v", err)
	}
	if !strings.Contains(raw, "+added") {
		t.Errorf("raw diff missing payload: %q", raw)
	}
	if pr.GetNumber() != 7 || pr.GetHead().GetRef() != "feature" {
		t.Errorf("pr meta wrong: %+v", pr)
	}
}

type apiCall struct{ method, path string }

func TestOpenPRHappyPath(t *testing.T) {
	var calls []apiCall
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, apiCall{r.Method, r.URL.Path})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ref":    "refs/heads/main",
			"object": map[string]any{"sha": "basesha"},
		})
	})
	mux.HandleFunc("/repos/acme/widget/git/trees", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, apiCall{r.Method, r.URL.Path})
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": "treesha"})
	})
	mux.HandleFunc("/repos/acme/widget/git/commits", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, apiCall{r.Method, r.URL.Path})
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": "commitsha"})
	})
	mux.HandleFunc("/repos/acme/widget/git/refs", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, apiCall{r.Method, r.URL.Path})
		_ = json.NewEncoder(w).Encode(map[string]any{"ref": "refs/heads/x"})
	})
	mux.HandleFunc("/repos/acme/widget/pulls", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, apiCall{r.Method, r.URL.Path})
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]any{}) // no existing PR for this head
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"html_url": "https://github.com/acme/widget/pull/123",
		})
	})
	c, _ := newTestClient(t, mux)
	url, err := c.OpenPR(context.Background(), PROpts{
		BaseBranch: "main", NewBranch: "x", Title: "t", Body: "b",
		Files: map[string][]byte{"a/b.txt": []byte("hi")},
	})
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}
	if url != "https://github.com/acme/widget/pull/123" {
		t.Errorf("url = %q", url)
	}
	wantMethods := []string{"GET", "POST", "POST", "POST", "GET", "POST"}
	if len(calls) != len(wantMethods) {
		t.Fatalf("call count: got %d, want %d (%+v)", len(calls), len(wantMethods), calls)
	}
	for i, m := range wantMethods {
		if calls[i].method != m {
			t.Errorf("call %d: method %s, want %s (path %s)", i, calls[i].method, m, calls[i].path)
		}
	}
}

func TestOpenPRBranchAlreadyExists(t *testing.T) {
	var sawUpdate bool
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ref":    "refs/heads/main",
			"object": map[string]any{"sha": "basesha"},
		})
	})
	mux.HandleFunc("/repos/acme/widget/git/trees", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": "treesha"})
	})
	mux.HandleFunc("/repos/acme/widget/git/commits", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": "commitsha"})
	})
	mux.HandleFunc("/repos/acme/widget/git/refs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "Reference already exists"})
	})
	mux.HandleFunc("/repos/acme/widget/git/refs/heads/x", func(w http.ResponseWriter, r *http.Request) {
		sawUpdate = true
		_ = json.NewEncoder(w).Encode(map[string]any{"ref": "refs/heads/x"})
	})
	mux.HandleFunc("/repos/acme/widget/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"html_url": "https://github.com/acme/widget/pull/456",
		})
	})
	c, _ := newTestClient(t, mux)
	url, err := c.OpenPR(context.Background(), PROpts{
		BaseBranch: "main", NewBranch: "x", Title: "t", Body: "b",
		Files: map[string][]byte{"a.txt": []byte("hi")},
	})
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}
	if !sawUpdate {
		t.Error("expected UpdateRef call after branch exists")
	}
	if url == "" {
		t.Error("empty url")
	}
}

func TestOpenPRPrechecksList_UpdatesExistingPR(t *testing.T) {
	// Re-trigger path: a PR for this branch already exists. OpenPR should
	// find it via List, PATCH the title/body, and return its URL — without
	// ever calling Create.
	var sawCreate, sawEdit bool
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ref": "refs/heads/main", "object": map[string]any{"sha": "basesha"},
		})
	})
	mux.HandleFunc("/repos/acme/widget/git/trees", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": "treesha"})
	})
	mux.HandleFunc("/repos/acme/widget/git/commits", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": "commitsha"})
	})
	mux.HandleFunc("/repos/acme/widget/git/refs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "Reference already exists"})
	})
	mux.HandleFunc("/repos/acme/widget/git/refs/heads/x", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ref": "refs/heads/x"})
	})
	mux.HandleFunc("/repos/acme/widget/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"number": 77, "html_url": "https://github.com/acme/widget/pull/77"},
			})
			return
		}
		sawCreate = true
		w.WriteHeader(http.StatusInternalServerError)
	})
	mux.HandleFunc("/repos/acme/widget/pulls/77", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			sawEdit = true
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["title"] != "updated-title" || body["body"] != "updated-body" {
				t.Errorf("PATCH body = %#v, want title=updated-title body=updated-body", body)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 77, "html_url": "https://github.com/acme/widget/pull/77",
		})
	})
	c, _ := newTestClient(t, mux)
	url, err := c.OpenPR(context.Background(), PROpts{
		BaseBranch: "main", NewBranch: "x", Title: "updated-title", Body: "updated-body",
		Files: map[string][]byte{"a.txt": []byte("hi")},
	})
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}
	if sawCreate {
		t.Error("Create should NOT have been called when a PR already exists")
	}
	if !sawEdit {
		t.Error("Edit (PATCH) was not called")
	}
	if url != "https://github.com/acme/widget/pull/77" {
		t.Errorf("url = %q, want existing PR url", url)
	}
}

func TestOpenPRRaceFalls_BackToEdit(t *testing.T) {
	// Create returns 422 "A pull request already exists" (race between List
	// and Create). OpenPR should fall back to List → Edit.
	listCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/widget/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ref": "refs/heads/main", "object": map[string]any{"sha": "basesha"},
		})
	})
	mux.HandleFunc("/repos/acme/widget/git/trees", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": "treesha"})
	})
	mux.HandleFunc("/repos/acme/widget/git/commits", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": "commitsha"})
	})
	mux.HandleFunc("/repos/acme/widget/git/refs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ref": "refs/heads/x"})
	})
	mux.HandleFunc("/repos/acme/widget/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			listCalls++
			if listCalls == 1 {
				// First precheck: no existing PR.
				_ = json.NewEncoder(w).Encode([]any{})
				return
			}
			// Race fallback: now the PR exists.
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"number": 99, "html_url": "https://github.com/acme/widget/pull/99"},
			})
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": "Validation Failed",
			"errors": []any{map[string]any{
				"resource": "PullRequest", "code": "custom",
				"message": "A pull request already exists for acme:x.",
			}},
		})
	})
	mux.HandleFunc("/repos/acme/widget/pulls/99", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 99, "html_url": "https://github.com/acme/widget/pull/99",
		})
	})
	c, _ := newTestClient(t, mux)
	url, err := c.OpenPR(context.Background(), PROpts{
		BaseBranch: "main", NewBranch: "x", Title: "t", Body: "b",
		Files: map[string][]byte{"a.txt": []byte("hi")},
	})
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}
	if url != "https://github.com/acme/widget/pull/99" {
		t.Errorf("url = %q", url)
	}
	if listCalls != 2 {
		t.Errorf("expected 2 List calls (precheck + race fallback), got %d", listCalls)
	}
}

func TestIsPRAlreadyExists(t *testing.T) {
	if isPRAlreadyExists(nil) {
		t.Error("nil should not be 'pr already exists'")
	}
	if !isPRAlreadyExists(errors.New("422 A pull request already exists for owner:branch")) {
		t.Error("string-match should detect")
	}
	resp := &github.ErrorResponse{
		Response: &http.Response{StatusCode: http.StatusUnprocessableEntity},
		Errors:   []github.Error{{Resource: "PullRequest", Code: "custom", Message: "A pull request already exists for x:y"}},
	}
	if !isPRAlreadyExists(resp) {
		t.Error("422 + 'A pull request already exists' should match")
	}
}
