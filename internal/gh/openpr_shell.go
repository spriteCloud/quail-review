package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/go-github/v66/github"
	"github.com/spriteCloud/quail-core/log"
)

// openPRViaShell creates the bot PR via plain git CLI + `gh pr create`,
// completely bypassing the REST git data API.
//
// Why: under GitHub Actions on this org/repo, POST /git/trees
// consistently returns 404 "Not Found" regardless of the body shape
// (inline content or SHA refs), regardless of the token type
// (GITHUB_TOKEN, OAuth user token, classic PAT), and regardless of
// the request size — yet the same token from the same runner happily
// accepts blob creation, branch creation, and PR creation. A
// side-by-side curl diagnostic from the same workflow returns 201 on
// a tiny tree but quail's request returns 404. GitHub-side
// root cause is unknown. Git CLI uses the regular git smart-HTTP
// transport (`git push https://x-access-token:TOKEN@github.com/...`),
// which has no documented restrictions and is the path actions/checkout
// itself uses to fetch.
//
// Returns (url, true, nil) on success. (_, false, nil) means "not
// applicable, caller should try the go-github path."
//
// v0.95.8.
func (c *Client) openPRViaShell(ctx context.Context, owner, repo string, opts PROpts) (string, bool, error) {
	for _, bin := range []string{"git", "gh"} {
		if _, err := exec.LookPath(bin); err != nil {
			log.Warn("openPRViaShell: required binary missing; falling back to go-github", "bin", bin)
			return "", false, nil
		}
	}

	token := c.cfg.GitHubToken
	tmp, err := os.MkdirTemp("", "quail-pr-*")
	if err != nil {
		return "", true, fmt.Errorf("mkdtemp: %w", err)
	}
	defer os.RemoveAll(tmp)

	cloneURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", token, owner, repo)
	if err := runCmd(ctx, "", "git", "clone", "--depth=1", "--branch", opts.BaseBranch, cloneURL, tmp); err != nil {
		return "", true, fmt.Errorf("clone %s: %w", opts.BaseBranch, err)
	}

	if err := runCmd(ctx, tmp, "git", "config", "user.email", "quail-bot@users.noreply.github.com"); err != nil {
		return "", true, fmt.Errorf("git config email: %w", err)
	}
	if err := runCmd(ctx, tmp, "git", "config", "user.name", "quail-bot"); err != nil {
		return "", true, fmt.Errorf("git config name: %w", err)
	}

	// Branch off base. Force-create so a stale local clone (won't happen
	// here since /tmp is fresh, but defensive) can't trip us.
	if err := runCmd(ctx, tmp, "git", "switch", "-C", opts.NewBranch); err != nil {
		return "", true, fmt.Errorf("switch -C %s: %w", opts.NewBranch, err)
	}

	for path, content := range opts.Files {
		dest := filepath.Join(tmp, path)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", true, fmt.Errorf("mkdir %s: %w", filepath.Dir(dest), err)
		}
		if err := os.WriteFile(dest, content, 0o644); err != nil {
			return "", true, fmt.Errorf("write %s: %w", dest, err)
		}
	}

	if err := runCmd(ctx, tmp, "git", "add", "-A"); err != nil {
		return "", true, fmt.Errorf("git add: %w", err)
	}
	// `git commit` exits 1 when nothing changed; treat as success (a
	// re-run of the same probe on the same head produces no diff).
	if err := runCmd(ctx, tmp, "git", "commit", "-m", opts.Title); err != nil {
		log.Info("openPRViaShell: nothing to commit; skipping PR open", "reason", err.Error())
		return "", true, nil
	}

	if err := runCmd(ctx, tmp, "git", "push", "--force", "origin", opts.NewBranch); err != nil {
		return "", true, fmt.Errorf("push %s: %w", opts.NewBranch, err)
	}
	log.Info("openPRViaShell: pushed branch", "branch", opts.NewBranch)

	// Idempotency: if a PR for this head already exists, edit it.
	listResp, err := ghAPI(ctx, token, "GET",
		fmt.Sprintf("/repos/%s/%s/pulls?head=%s:%s&state=open&per_page=1",
			owner, repo, owner, opts.NewBranch), nil)
	if err == nil {
		var existing []struct {
			Number  int    `json:"number"`
			HTMLURL string `json:"html_url"`
		}
		if json.Unmarshal(listResp, &existing) == nil && len(existing) > 0 {
			patchReq := map[string]any{"title": opts.Title, "body": opts.Body}
			patchPath := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, existing[0].Number)
			if _, err := ghAPI(ctx, token, "PATCH", patchPath, patchReq); err == nil {
				log.Info("openPRViaShell: updated existing pr", "url", existing[0].HTMLURL)
				return existing[0].HTMLURL, true, nil
			}
		}
	}

	// Open new PR via gh CLI (works — proven, that's how everyone does it).
	createCmd := exec.CommandContext(ctx, "gh", "pr", "create",
		"--repo", owner+"/"+repo,
		"--base", opts.BaseBranch,
		"--head", opts.NewBranch,
		"--title", opts.Title,
		"--body", opts.Body)
	createCmd.Dir = tmp
	createCmd.Env = append(os.Environ(), "GH_TOKEN="+token, "GITHUB_TOKEN="+token)
	var stderr bytes.Buffer
	createCmd.Stderr = &stderr
	out, err := createCmd.Output()
	if err != nil {
		return "", true, fmt.Errorf("gh pr create: %s", strings.TrimSpace(stderr.String()))
	}
	url := strings.TrimSpace(string(out))
	log.Info("openPRViaShell: opened pr", "url", url, "branch", opts.NewBranch)
	return url, true, nil
}

// runCmd runs a command and returns a wrapped error on non-zero exit
// with stderr attached.
func runCmd(ctx context.Context, dir string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// ghAPI shells out to `curl` against api.github.com. Retained for the
// remaining PR-list / PR-patch calls that DO succeed via curl. The
// failing tree/commit/ref API path is no longer used — see the git
// CLI path above.
func ghAPI(ctx context.Context, token, method, path string, body any) ([]byte, error) {
	url := "https://api.github.com" + path
	args := []string{
		"-sS", "-w", "\n__HTTP_CODE__%{http_code}",
		"-X", method,
		"-H", "Authorization: Bearer " + token,
		"-H", "Accept: application/vnd.github+json",
		url,
	}
	cmd := exec.CommandContext(ctx, "curl", args...)
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		cmd.Args = append(cmd.Args, "--data-binary", "@-")
		cmd.Stdin = bytes.NewReader(raw)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("curl %s %s: %s", method, path, msg)
	}
	idx := bytes.LastIndex(out, []byte("__HTTP_CODE__"))
	if idx < 0 {
		return nil, fmt.Errorf("curl %s %s: missing http code marker; body=%s", method, path, out)
	}
	bodyBytes := bytes.TrimRight(out[:idx], "\n")
	codeStr := strings.TrimSpace(string(out[idx+len("__HTTP_CODE__"):]))
	if !strings.HasPrefix(codeStr, "2") {
		return nil, fmt.Errorf("curl %s %s: HTTP %s: %s", method, path, codeStr, bytes.TrimSpace(bodyBytes))
	}
	_ = github.String // keep import alive for sibling-file parity
	return bodyBytes, nil
}

// useShellOpenPR is the gate for the shell fallback. On when running
// under GitHub Actions AND go-github's client is pointed at the real
// api.github.com.
func (c *Client) useShellOpenPR() bool {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		return false
	}
	if c.api == nil || c.api.BaseURL == nil {
		return false
	}
	return c.api.BaseURL.Host == "api.github.com"
}
