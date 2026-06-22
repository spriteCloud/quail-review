// Package gh wraps the GitHub API for the two operations we need:
// fetching a PR's unified diff, and opening a follow-up PR with a set of
// file edits on a new branch.
package gh

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/google/go-github/v66/github"
	"github.com/spriteCloud/quail-review/internal/config"
	"github.com/spriteCloud/quail-review/internal/log"
	"golang.org/x/oauth2"
)

type Client struct {
	api *github.Client
	cfg config.Config
}

func New(ctx context.Context, cfg config.Config) (*Client, error) {
	if cfg.GitHubToken == "" {
		return nil, errors.New("gh: missing GITHUB_TOKEN / QUAIL_GITHUB_TOKEN")
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.GitHubToken})
	tc := oauth2.NewClient(ctx, ts)
	api := github.NewClient(tc)
	// v0.95.1 — override go-github's default `go-github/vNN` User-Agent.
	// Under GitHub Actions, requests carrying that UA + the runtime
	// GITHUB_TOKEN get 403 "Resource not accessible by integration" on
	// `POST /git/trees` (and likely related write endpoints), even though
	// the same token from the same runner succeeds via curl on the same
	// endpoint. Sending a non-`go-github` UA bypasses whatever heuristic
	// gates the lib's default UA.
	api.UserAgent = "spritecloud-quail"
	return &Client{api: api, cfg: cfg}, nil
}

// FetchDiff returns the unified diff for the PR and the PR metadata.
func (c *Client) FetchDiff(ctx context.Context, pr int) (string, *github.PullRequest, error) {
	owner, repo, ok := c.cfg.SplitRepo()
	if !ok {
		return "", nil, fmt.Errorf("gh: invalid GITHUB_REPOSITORY %q", c.cfg.Repo)
	}
	prObj, _, err := c.api.PullRequests.Get(ctx, owner, repo, pr)
	if err != nil {
		return "", nil, err
	}
	raw, _, err := c.api.PullRequests.GetRaw(ctx, owner, repo, pr,
		github.RawOptions{Type: github.Diff})
	if err != nil {
		return "", nil, err
	}
	return raw, prObj, nil
}

// FileBlobs fetches the new and (when modified) old blob contents for the
// listed paths, at the PR's head and base SHAs respectively.
func (c *Client) FileBlobs(ctx context.Context, prObj *github.PullRequest, paths []string) (newBlobs map[string]string, oldBlobs map[string]string, err error) {
	owner, repo, ok := c.cfg.SplitRepo()
	if !ok {
		return nil, nil, fmt.Errorf("gh: invalid GITHUB_REPOSITORY %q", c.cfg.Repo)
	}
	newBlobs = map[string]string{}
	oldBlobs = map[string]string{}
	for _, p := range paths {
		if s, err := c.readBlob(ctx, owner, repo, p, prObj.GetHead().GetSHA()); err == nil {
			newBlobs[p] = s
		}
		if s, err := c.readBlob(ctx, owner, repo, p, prObj.GetBase().GetSHA()); err == nil {
			oldBlobs[p] = s
		}
	}
	return newBlobs, oldBlobs, nil
}

func (c *Client) readBlob(ctx context.Context, owner, repo, path, ref string) (string, error) {
	f, _, _, err := c.api.Repositories.GetContents(ctx, owner, repo, path,
		&github.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		return "", err
	}
	if f == nil || f.Content == nil {
		return "", fmt.Errorf("empty")
	}
	if enc := f.GetEncoding(); enc == "base64" {
		raw, err := base64.StdEncoding.DecodeString(*f.Content)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	return *f.Content, nil
}

// ReadFile returns the file content at the given ref. found=false means the
// file does not exist (HTTP 404); any other failure is returned as err.
func (c *Client) ReadFile(ctx context.Context, path, ref string) (content string, found bool, err error) {
	owner, repo, ok := c.cfg.SplitRepo()
	if !ok {
		return "", false, fmt.Errorf("gh: invalid GITHUB_REPOSITORY %q", c.cfg.Repo)
	}
	s, err := c.readBlob(ctx, owner, repo, path, ref)
	if err != nil {
		var ge *github.ErrorResponse
		if errors.As(err, &ge) && ge.Response != nil && ge.Response.StatusCode == http.StatusNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	return s, true, nil
}

type PROpts struct {
	BaseBranch string
	NewBranch  string
	Title      string
	Body       string
	Files      map[string][]byte // path -> content
}

// OpenPR commits Files on NewBranch (created from BaseBranch's HEAD) and opens
// a PR. Existing files are overwritten.
func (c *Client) OpenPR(ctx context.Context, opts PROpts) (string, error) {
	owner, repo, ok := c.cfg.SplitRepo()
	if !ok {
		return "", fmt.Errorf("gh: invalid GITHUB_REPOSITORY %q", c.cfg.Repo)
	}
	// v0.95.9 — under GitHub Actions, drop any .github/workflows/* file
	// from the bot-PR push. The default GITHUB_TOKEN and a `repo`-scope
	// PAT both lack the `workflow` permission and the remote rejects
	// the entire push if even one workflow file is present. Suppress
	// here, log once, and let the caller advertise it in the PR body.
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		var dropped []string
		for path := range opts.Files {
			if strings.HasPrefix(path, ".github/workflows/") {
				dropped = append(dropped, path)
			}
		}
		if len(dropped) > 0 {
			filtered := make(map[string][]byte, len(opts.Files)-len(dropped))
			for path, content := range opts.Files {
				if strings.HasPrefix(path, ".github/workflows/") {
					continue
				}
				filtered[path] = content
			}
			opts.Files = filtered
			log.Info("OpenPR: suppressed workflow files (need `workflow` PAT scope)",
				"dropped", len(dropped), "first", dropped[0])
		}
	}
	// Shell fallback for GitHub Actions runners (see openpr_shell.go).
	if c.useShellOpenPR() {
		if url, handled, err := c.openPRViaShell(ctx, owner, repo, opts); handled {
			if err != nil {
				return "", fmt.Errorf("open pr (shell): %w", err)
			}
			return url, nil
		}
	}
	baseRef, _, err := c.api.Git.GetRef(ctx, owner, repo, "refs/heads/"+opts.BaseBranch)
	if err != nil {
		return "", fmt.Errorf("get base ref: %w", err)
	}
	parent := baseRef.GetObject().GetSHA()

	// Build a tree of changes layered on top of the base commit.
	var entries []*github.TreeEntry
	mode := "100644"
	tType := "blob"
	for path, content := range opts.Files {
		s := string(content)
		entries = append(entries, &github.TreeEntry{
			Path:    github.String(path),
			Mode:    &mode,
			Type:    &tType,
			Content: &s,
		})
	}
	tree, _, err := c.api.Git.CreateTree(ctx, owner, repo, parent, entries)
	if err != nil {
		return "", fmt.Errorf("create tree: %w", err)
	}
	commit := &github.Commit{
		Message: github.String(opts.Title),
		Tree:    tree,
		Parents: []*github.Commit{{SHA: &parent}},
	}
	newCommit, _, err := c.api.Git.CreateCommit(ctx, owner, repo, commit, nil)
	if err != nil {
		return "", fmt.Errorf("create commit: %w", err)
	}
	branchRef := "refs/heads/" + opts.NewBranch
	_, _, err = c.api.Git.CreateRef(ctx, owner, repo, &github.Reference{
		Ref:    github.String(branchRef),
		Object: &github.GitObject{SHA: newCommit.SHA},
	})
	if err != nil && !isAlreadyExists(err) {
		return "", fmt.Errorf("create branch: %w", err)
	}
	if isAlreadyExists(err) {
		_, _, err = c.api.Git.UpdateRef(ctx, owner, repo, &github.Reference{
			Ref:    github.String(branchRef),
			Object: &github.GitObject{SHA: newCommit.SHA},
		}, true)
		if err != nil {
			return "", fmt.Errorf("update branch: %w", err)
		}
		log.Info("updated existing branch", "branch", opts.NewBranch)
	} else {
		log.Info("created branch", "branch", opts.NewBranch)
	}
	// Idempotency: if a PR for this head already exists (re-trigger of the
	// same source PR, synchronize event, retry after transient failure),
	// update its title/body in place instead of erroring.
	if url, ok, err := c.updateExistingPR(ctx, owner, repo, opts); err != nil {
		return "", fmt.Errorf("open pr: %w", err)
	} else if ok {
		return url, nil
	}
	pr, _, err := c.api.PullRequests.Create(ctx, owner, repo, &github.NewPullRequest{
		Title:               github.String(opts.Title),
		Head:                github.String(opts.NewBranch),
		Base:                github.String(opts.BaseBranch),
		Body:                github.String(opts.Body),
		MaintainerCanModify: github.Bool(true),
	})
	if err != nil {
		// Race: the PR was created between our precheck and our Create. Fall
		// back to the same edit path.
		if isPRAlreadyExists(err) {
			if url, ok, fbErr := c.updateExistingPR(ctx, owner, repo, opts); fbErr == nil && ok {
				return url, nil
			}
		}
		return "", fmt.Errorf("open pr: %w", err)
	}
	log.Info("opened pr", "url", pr.GetHTMLURL(), "branch", opts.NewBranch)
	return pr.GetHTMLURL(), nil
}

// updateExistingPR returns (url, true, nil) when a PR exists for this branch
// and was successfully patched with the new title and body. (_, false, nil)
// when no PR was found.
func (c *Client) updateExistingPR(ctx context.Context, owner, repo string, opts PROpts) (string, bool, error) {
	existing, _, err := c.api.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
		Head:        owner + ":" + opts.NewBranch,
		State:       "open",
		ListOptions: github.ListOptions{PerPage: 1},
	})
	if err != nil {
		return "", false, fmt.Errorf("list prs: %w", err)
	}
	if len(existing) == 0 {
		return "", false, nil
	}
	pr := existing[0]
	patched, _, err := c.api.PullRequests.Edit(ctx, owner, repo, pr.GetNumber(), &github.PullRequest{
		Title: github.String(opts.Title),
		Body:  github.String(opts.Body),
	})
	if err != nil {
		return "", false, fmt.Errorf("edit pr #%d: %w", pr.GetNumber(), err)
	}
	log.Info("updated existing pr", "url", patched.GetHTMLURL(), "branch", opts.NewBranch)
	return patched.GetHTMLURL(), true, nil
}

func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	var ge *github.ErrorResponse
	if errors.As(err, &ge) && ge.Response != nil {
		if ge.Response.StatusCode == http.StatusUnprocessableEntity &&
			strings.Contains(ge.Message, "Reference already exists") {
			return true
		}
	}
	return strings.Contains(err.Error(), "already exists")
}

func isPRAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	var ge *github.ErrorResponse
	if errors.As(err, &ge) && ge.Response != nil {
		if ge.Response.StatusCode == http.StatusUnprocessableEntity &&
			strings.Contains(err.Error(), "A pull request already exists") {
			return true
		}
	}
	return strings.Contains(err.Error(), "A pull request already exists")
}
