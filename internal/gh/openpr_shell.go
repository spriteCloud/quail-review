package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/google/go-github/v66/github"
	"github.com/spriteCloud/quail/internal/log"
)

// openPRViaShell creates the bot PR by shelling out to `gh api` for
// every write call. Why: under GitHub Actions, quail's go-github writes
// to /git/trees consistently 403 with "Resource not accessible by
// integration" — even though the SAME GITHUB_TOKEN, from the SAME
// runner, succeeds on the SAME endpoint via curl and `gh api` (proven
// by a side-by-side diagnostic on the quail-e2e-demo repo). Cause is
// not yet root-caused in the go-github / oauth2 stack. Until it is,
// this shell path is the reliable fallback when running under Actions.
//
// Returns (url, true, nil) on success. (_, false, nil) means "not
// applicable, caller should try the go-github path." Any error is
// from the shell path itself.
func (c *Client) openPRViaShell(ctx context.Context, owner, repo string, opts PROpts) (string, bool, error) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		return "", false, nil
	}
	if _, err := exec.LookPath("gh"); err != nil {
		log.Warn("openPRViaShell: gh CLI not found on PATH; falling back to go-github")
		return "", false, nil
	}

	// 1) Resolve base branch SHA.
	baseSHARaw, err := ghAPI(ctx, c.cfg.GitHubToken, "GET",
		fmt.Sprintf("/repos/%s/%s/git/refs/heads/%s", owner, repo, opts.BaseBranch), nil)
	if err != nil {
		return "", true, fmt.Errorf("get base ref: %w", err)
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(baseSHARaw, &ref); err != nil {
		return "", true, fmt.Errorf("parse base ref: %w", err)
	}
	parent := ref.Object.SHA

	// 2) Build tree entries. Each entry inlines its content; `gh api`
	// then makes the same POST /git/trees call that the diagnostic
	// proved succeeds.
	type entry struct {
		Path    string `json:"path"`
		Mode    string `json:"mode"`
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	entries := make([]entry, 0, len(opts.Files))
	for path, content := range opts.Files {
		entries = append(entries, entry{
			Path:    path,
			Mode:    "100644",
			Type:    "blob",
			Content: string(content),
		})
	}

	// 3) CreateTree.
	treeReq := map[string]interface{}{"base_tree": parent, "tree": entries}
	treeResp, err := ghAPI(ctx, c.cfg.GitHubToken, "POST",
		fmt.Sprintf("/repos/%s/%s/git/trees", owner, repo), treeReq)
	if err != nil {
		return "", true, fmt.Errorf("create tree: %w", err)
	}
	var tree struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(treeResp, &tree); err != nil {
		return "", true, fmt.Errorf("parse tree: %w", err)
	}

	// 4) CreateCommit.
	commitReq := map[string]interface{}{
		"message": opts.Title,
		"tree":    tree.SHA,
		"parents": []string{parent},
	}
	commitResp, err := ghAPI(ctx, c.cfg.GitHubToken, "POST",
		fmt.Sprintf("/repos/%s/%s/git/commits", owner, repo), commitReq)
	if err != nil {
		return "", true, fmt.Errorf("create commit: %w", err)
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(commitResp, &commit); err != nil {
		return "", true, fmt.Errorf("parse commit: %w", err)
	}

	// 5) CreateRef / UpdateRef.
	branchRef := "refs/heads/" + opts.NewBranch
	createRefReq := map[string]interface{}{"ref": branchRef, "sha": commit.SHA}
	_, err = ghAPI(ctx, c.cfg.GitHubToken, "POST",
		fmt.Sprintf("/repos/%s/%s/git/refs", owner, repo), createRefReq)
	branchAction := "created"
	if err != nil {
		// Branch already exists → fast-forward via PATCH.
		if !strings.Contains(err.Error(), "Reference already exists") {
			return "", true, fmt.Errorf("create branch: %w", err)
		}
		updateRefReq := map[string]interface{}{"sha": commit.SHA, "force": true}
		if _, err := ghAPI(ctx, c.cfg.GitHubToken, "PATCH",
			fmt.Sprintf("/repos/%s/%s/git/refs/heads/%s", owner, repo, opts.NewBranch), updateRefReq); err != nil {
			return "", true, fmt.Errorf("update branch: %w", err)
		}
		branchAction = "updated"
	}
	log.Info(branchAction+" branch", "branch", opts.NewBranch)

	// 6) Idempotency: if a PR already exists for this head, patch it.
	listPath := fmt.Sprintf("/repos/%s/%s/pulls?head=%s:%s&state=open&per_page=1",
		owner, repo, owner, opts.NewBranch)
	listResp, err := ghAPI(ctx, c.cfg.GitHubToken, "GET", listPath, nil)
	if err == nil {
		var existing []struct {
			Number  int    `json:"number"`
			HTMLURL string `json:"html_url"`
		}
		if json.Unmarshal(listResp, &existing) == nil && len(existing) > 0 {
			patchReq := map[string]interface{}{"title": opts.Title, "body": opts.Body}
			patchPath := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, existing[0].Number)
			if _, err := ghAPI(ctx, c.cfg.GitHubToken, "PATCH", patchPath, patchReq); err == nil {
				log.Info("updated existing pr", "url", existing[0].HTMLURL, "branch", opts.NewBranch)
				return existing[0].HTMLURL, true, nil
			}
		}
	}

	// 7) Create the PR.
	prReq := map[string]interface{}{
		"title":                 opts.Title,
		"head":                  opts.NewBranch,
		"base":                  opts.BaseBranch,
		"body":                  opts.Body,
		"maintainer_can_modify": true,
	}
	prResp, err := ghAPI(ctx, c.cfg.GitHubToken, "POST",
		fmt.Sprintf("/repos/%s/%s/pulls", owner, repo), prReq)
	if err != nil {
		return "", true, fmt.Errorf("create pr: %w", err)
	}
	var pr struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(prResp, &pr); err != nil {
		return "", true, fmt.Errorf("parse pr: %w", err)
	}
	log.Info("opened pr", "url", pr.HTMLURL, "branch", opts.NewBranch)
	return pr.HTMLURL, true, nil
}

// ghAPI shells out to `gh api`. body=nil → no body. Returns response body bytes.
func ghAPI(ctx context.Context, token, method, path string, body interface{}) ([]byte, error) {
	args := []string{"api", "--method", method,
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: 2022-11-28",
		path}
	cmd := exec.CommandContext(ctx, "gh", args...)
	env := append(os.Environ(), "GH_TOKEN="+token, "GITHUB_TOKEN="+token)
	cmd.Env = env

	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		cmd.Args = append(cmd.Args, "--input", "-")
		cmd.Stdin = bytes.NewReader(raw)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh api %s %s: %s", method, path, strings.TrimSpace(stderr.String()))
	}
	// gh prints both success and error responses on stdout; gate on exit.
	_ = github.String // keep import alive for parity with sibling file
	return out, nil
}

// useShellOpenPR is the gate for the shell fallback. Currently: on
// when running under GitHub Actions. Can later be flipped to also
// trigger on a heuristic 403 retry, but right now CI is the only
// known-broken context.
func useShellOpenPR() bool { return os.Getenv("GITHUB_ACTIONS") == "true" }
