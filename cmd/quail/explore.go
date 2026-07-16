package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	core "github.com/spriteCloud/quail-core"
	"github.com/spriteCloud/quail-core/config"
	"github.com/spriteCloud/quail-core/diff"
	"github.com/spriteCloud/quail-core/gh"
	rlog "github.com/spriteCloud/quail-core/log"
	"github.com/spriteCloud/quail-core/report/explorehtml"
	"github.com/spriteCloud/quail-review/internal/spec"
)

// newExploreCmd implements `quail explore` — adversarial bug-hunting against
// a live URL. Unlike `probe` (which builds comprehensive coverage), explore
// hunts for real bugs through targeted adversarial interaction.
//
// Two axes distinguish it from the other commands:
//
//  1. Ephemeral by default. The generated Playwright specs and Gherkin
//     features exist only long enough to run once — they're written into
//     an os.MkdirTemp workdir and wiped on exit. The Gherkin-formatted
//     report survives on stdout so a human can read what was exercised.
//     Pass --persist to keep today's on-disk layout.
//
//  2. Change-aware. On every run the CLI auto-detects the last change
//     (PR diff in CI via $GITHUB_EVENT_PATH, else `git diff HEAD~1..HEAD`
//     locally) and forwards *file paths only* to the LLM as prioritisation
//     hints for the attack-plan operation. The deterministic probing
//     layer still runs across every discovered element — the diff only
//     steers where the LLM points its extra attention.
func newExploreCmd() *cobra.Command {
	var (
		targetURL    string
		focus        string
		depth        string
		findingsPath string
		workdir      string
		dryRun       bool

		// New: ephemeral + change-aware axes.
		ephemeral bool
		persist   bool
		pr        int

		// LLM (all optional; deterministic layer always runs).
		llmURL     string
		model      string
		llmTimeout string

		// Timeboxed exploratory loop.
		timebox string

		// HTML report path (auto when empty).
		htmlOut string
	)

	cmd := &cobra.Command{
		Use:   "explore",
		Short: "Adversarial bug-hunting against a live URL",
		Long: `Probe a live application for real bugs through targeted adversarial interaction.

Unlike 'probe' (which builds comprehensive test coverage), 'explore' applies
12 attack categories — boundary inputs, injection probes, state corruption,
race conditions, auth/access, data edge cases, cross-feature state, interrupted
flows, out-of-order operations, role/session transitions, upstream dependency
failures, and cumulative state — to every discovered interactive element.

Ephemeral by default: the generated Playwright specs and .feature files are
written to a temp dir, executed once, and wiped on exit. The Gherkin-formatted
report is printed to stdout. Pass --persist to keep files under --workdir.

Change-aware by default: on every run the last change is auto-detected (PR
diff in CI, else 'git diff HEAD~1..HEAD' locally) and its file paths are
forwarded to the LLM to prioritise attack-plan targets. The deterministic
layer probes the whole app regardless.

Deterministic-first: attack templates run without an LLM. Pass --llm to
activate the AI layer, which proposes additional targets and composes Gherkin
scenarios for confirmed anomalies. All LLM output is validated against the
embedded guardrails spec (internal/spec/explore_guardrails.md) before use.

Examples:
  # Ephemeral change-aware probe (default):
  quail explore --url https://shop.example.com

  # Persist specs + ledger to disk (today's behaviour):
  quail explore --url https://shop.example.com --persist --workdir .

  # Focused auth surface only, no LLM:
  quail explore --url https://shop.example.com --focus auth,injection

  # CI: explicit PR number (auto-detected from $GITHUB_EVENT_PATH otherwise):
  quail explore --url https://review-42.preview.example.com --pr 42`,

		RunE: func(cmd *cobra.Command, _ []string) error {
			return runExplore(cmd.Context(), exploreOpts{
				targetURL:    targetURL,
				focus:        focus,
				depth:        depth,
				findingsPath: findingsPath,
				workdir:      workdir,
				dryRun:       dryRun,
				ephemeral:    ephemeral,
				persist:      persist,
				pr:           pr,
				llmURL:       llmURL,
				model:        model,
				llmTimeout:   llmTimeout,
				timebox:      timebox,
				htmlOut:      htmlOut,
			})
		},
	}

	f := cmd.Flags()

	f.StringVar(&targetURL, "url", "", "Target URL to probe (required)")
	_ = cmd.MarkFlagRequired("url")

	f.StringVar(&focus, "focus", "all",
		"Comma-separated attack categories to run. 'all' runs every category.\n"+
			"Valid values: boundary,injection,state-corrupt,race,auth,data-edge,\n"+
			"              cross-feature,flow-interrupt,sequence,role-switch,\n"+
			"              upstream-dep,cumulative")
	f.StringVar(&depth, "depth", "standard",
		"Probe depth: shallow (30 probes/page) | standard (60) | deep (120)")

	// Output / persistence.
	f.BoolVar(&ephemeral, "ephemeral", true,
		"Run once and discard generated specs/features. Report streams to stdout as Gherkin. Default true.")
	f.BoolVar(&persist, "persist", false,
		"Persist generated specs, features, and findings under --workdir. Overrides --ephemeral.")
	f.StringVar(&findingsPath, "findings", "",
		"Path for the findings ledger when persisting (default: tests/e2e/docs/exploratory-findings.md). Ignored when ephemeral.")
	f.StringVar(&workdir, "workdir", ".",
		"Working directory for generated specs and docs. Ignored when ephemeral.")
	f.BoolVar(&dryRun, "dry-run", false,
		"Print the attack plan without executing probes. Independent of --ephemeral.")

	// Change-aware.
	f.IntVar(&pr, "pr", 0,
		"PR number for change context. Defaults to $GITHUB_EVENT_PATH; falls back to local `git diff HEAD~1..HEAD`.")

	// LLM (optional).
	f.StringVar(&llmURL, "llm", "",
		"OpenAI-compatible endpoint for AI-assisted target selection and scenario composition.\n"+
			"Accepts the URL with or without trailing /v1 (normalised).\n"+
			"Strictly local/self-hosted — do not point at third-party endpoints.")
	f.StringVar(&model, "model", "",
		"Model ID for the LLM endpoint (default: inherits QUAIL_MODEL or gpt-4o-mini)")
	f.StringVar(&llmTimeout, "llm-timeout", "",
		"Per-call LLM timeout, Go duration (default: inherits QUAIL_LLM_TIMEOUT or 60s)")
	f.StringVar(&timebox, "timebox", "",
		"Wall-clock ceiling on the exploratory session, Go duration (default: inherits QUAIL_EXPLORE_TIMEBOX or 60s). "+
			"The engine calls the LLM in a loop until this expires or two consecutive rounds produce nothing new.")
	f.StringVar(&htmlOut, "html-out", "",
		"Path to write the branded HTML report. Empty (default): auto — persisted next to the ledger in --persist mode, "+
			"else under $TMPDIR with the file path echoed to stderr.")

	return cmd
}

type exploreOpts struct {
	targetURL, focus, depth, findingsPath, workdir string
	dryRun, ephemeral, persist                     bool
	pr                                             int
	llmURL, model, llmTimeout                      string
	timebox                                        string
	htmlOut                                        string
}

func runExplore(ctx context.Context, o exploreOpts) error {
	// Environment variable overrides (flags win when set explicitly).
	if o.targetURL == "" {
		o.targetURL = os.Getenv("QUAIL_TARGET_URL")
	}
	if o.llmURL == "" {
		o.llmURL = os.Getenv("QUAIL_LLM")
	}
	if o.model == "" {
		o.model = envOr("QUAIL_MODEL", "gpt-4o-mini")
	}
	if o.llmTimeout == "" {
		o.llmTimeout = envOr("QUAIL_LLM_TIMEOUT", "60s")
	}
	if o.timebox == "" {
		o.timebox = envOr("QUAIL_EXPLORE_TIMEBOX", "60s")
	}
	if o.pr == 0 {
		if v := os.Getenv("QUAIL_PR"); v != "" {
			// deliberate silent fallback to 0 on parse failure — matches
			// how newGenerateCmd handles $QUAIL_PR.
			fmt.Sscanf(v, "%d", &o.pr)
		}
	}

	switch o.depth {
	case "shallow", "standard", "deep":
	default:
		return fmt.Errorf("--depth must be one of: shallow | standard | deep (got %q)", o.depth)
	}

	categories, err := parseExploreCategories(o.focus)
	if err != nil {
		return err
	}

	// --persist wins over the ephemeral default so users can opt back in.
	ephemeral := o.ephemeral && !o.persist

	// Ephemeral workdir: MkdirTemp + defer wipe. Engine writes here as
	// always; the CLI owns the cleanup policy.
	// ponytail: temp dir is per-run; if we ever need cross-run continuation
	// the fix is a stable dir under $XDG_STATE_HOME, not surfacing a flag.
	if ephemeral {
		tmp, err := os.MkdirTemp("", "quail-explore-*")
		if err != nil {
			return fmt.Errorf("ephemeral workdir: %w", err)
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		o.workdir = tmp
		o.findingsPath = "" // engine defaults inside the tmp dir; not surfaced.
	} else if o.findingsPath == "" {
		o.findingsPath = envOr("QUAIL_FINDINGS", "tests/e2e/docs/exploratory-findings.md")
	}

	// Change context — PR first, local git diff as fallback.
	changes := loadExploreDiff(ctx, o.pr)

	cfg := core.ExploreConfig{
		TargetURL:      o.targetURL,
		Categories:     categories,
		Depth:          o.depth,
		FindingsPath:   o.findingsPath,
		WorkDir:        o.workdir,
		DryRun:         o.dryRun,
		Ephemeral:      ephemeral,
		Changes:        changes,
		LLM:            exploreLLMConfigOrNil(o.llmURL, o.model, o.llmTimeout),
		GuardrailsSpec: spec.ExploreGuardrails,
		Timebox:        parseTimebox(o.timebox),
	}

	runner, err := core.NewExplorer(cfg)
	if err != nil {
		return fmt.Errorf("explore init: %w", err)
	}

	result, err := runner.Run(ctx)
	if err != nil {
		return fmt.Errorf("explore run: %w", err)
	}

	// Summary line — mirroring the style of `probe` and `generate`.
	fmt.Printf(
		"quail explore: %d pages probed · %d anomalies detected · %d confirmed findings\n",
		result.PagesProbed, result.AnomaliesDetected, result.FindingsConfirmed,
	)
	if ephemeral {
		if strings.TrimSpace(result.Report) != "" {
			fmt.Println()
			fmt.Println(result.Report)
		}
	} else {
		fmt.Printf("  specs   → %s\n  ledger  → %s\n", result.SpecsDir, result.FindingsPath)
	}

	// HTML report — always attempt to write, so consumers never need a
	// second post-processing step. Silent on error (falls back to the
	// Gherkin stdout everything else keys off of).
	if htmlPath := writeExploreHTML(result.Report, o, ephemeral); htmlPath != "" {
		fmt.Fprintf(os.Stderr, "  report  → %s\n", htmlPath)
	}

	if result.FindingsConfirmed > 0 {
		critHigh := result.BySeverity["critical"] + result.BySeverity["high"]
		if critHigh > 0 {
			target := result.FindingsPath
			if ephemeral {
				target = "the report above"
			}
			fmt.Printf("  ⚠  %d critical/high severity finding(s) — review %s\n", critHigh, target)
		}
	}

	return nil
}

// writeExploreHTML renders the Gherkin report as HTML and drops it at the
// first-available destination. Silent-nil on any error — HTML is a nice-to-have
// beside the stdout Gherkin, not a hard dependency. Returns the on-disk path
// so the caller can announce it on stderr.
func writeExploreHTML(gherkin string, o exploreOpts, ephemeral bool) string {
	if strings.TrimSpace(gherkin) == "" {
		return ""
	}
	rendered, err := explorehtml.Render(gherkin, explorehtml.Meta{
		TargetURL: o.targetURL,
		Generated: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return ""
	}
	// Destination precedence:
	//   1. --html-out (explicit)
	//   2. --persist: <workdir>/report.html next to the ledger
	//   3. ephemeral: $TMPDIR/quail-explore-<host>-<epoch>.html
	dst := o.htmlOut
	switch {
	case dst != "":
		// use as-is
	case !ephemeral && o.workdir != "":
		dst = filepath.Join(o.workdir, "report.html")
	default:
		host := "run"
		if u, err := url.Parse(o.targetURL); err == nil && u.Host != "" {
			host = strings.NewReplacer(":", "_", "/", "_").Replace(u.Host)
		}
		dst = filepath.Join(os.TempDir(),
			fmt.Sprintf("quail-explore-%s-%d.html", host, time.Now().Unix()))
	}
	if err := os.WriteFile(dst, []byte(rendered), 0o644); err != nil {
		return ""
	}
	return dst
}

// loadExploreDiff resolves the "last change" that steers the LLM attack-plan
// prioritisation. Order: explicit --pr / $QUAIL_PR / $GITHUB_EVENT_PATH,
// then local `git diff HEAD~1..HEAD`. Nil is a valid result — the engine
// treats a missing diff as "no change context, probe everything".
func loadExploreDiff(ctx context.Context, prNum int) []diff.File {
	if prNum == 0 {
		prNum = readPRFromEvent()
	}
	if prNum != 0 {
		cfg := config.FromEnv()
		client, err := gh.New(ctx, cfg)
		if err == nil && client != nil {
			files, _, err := fetchPRFilesAndInfo(ctx, client, prNum)
			if err == nil && len(files) > 0 {
				return files
			}
			if err != nil {
				rlog.Warn("explore: PR diff fetch failed; falling back to local git diff", "err", err)
			}
		}
	}
	return readLocalDiff()
}

// readLocalDiff runs `git diff HEAD~1..HEAD` in the current directory and
// parses the result. Silent-nil on any failure (no repo, shallow clone,
// first commit) — matches readPRFromEvent's forgiving style.
func readLocalDiff() []diff.File {
	out, err := exec.Command("git", "diff", "--unified=0", "HEAD~1..HEAD").Output()
	if err != nil {
		return nil
	}
	return diff.Parse(string(out))
}

// parseExploreCategories validates and expands the --focus flag value.
// "all" expands to every registered category.
func parseExploreCategories(focus string) ([]string, error) {
	all := []string{
		"boundary", "injection", "state-corrupt", "race",
		"auth", "data-edge", "cross-feature", "flow-interrupt",
		"sequence", "role-switch", "upstream-dep", "cumulative",
	}

	if strings.EqualFold(focus, "all") {
		return all, nil
	}

	valid := make(map[string]struct{}, len(all))
	for _, c := range all {
		valid[c] = struct{}{}
	}

	requested := strings.Split(focus, ",")
	result := make([]string, 0, len(requested))
	var unknown []string

	for _, c := range requested {
		c = strings.TrimSpace(strings.ToLower(c))
		if c == "" {
			continue
		}
		if _, ok := valid[c]; !ok {
			unknown = append(unknown, c)
			continue
		}
		result = append(result, c)
	}

	if len(unknown) > 0 {
		return nil, fmt.Errorf(
			"unknown attack categories: %s\nValid categories: %s",
			strings.Join(unknown, ", "),
			strings.Join(all, ", "),
		)
	}
	if len(result) == 0 {
		return nil, errors.New("--focus produced no valid categories")
	}

	return result, nil
}

// exploreLLMConfigOrNil returns nil when no LLM endpoint is configured,
// signalling deterministic-only mode to the core runner. Pulls the API
// key from OPENAI_API_KEY so ollama's "ollama" sentinel and real OpenAI
// keys both work without another flag.
func exploreLLMConfigOrNil(endpoint, model, timeout string) *core.LLMConfig {
	if endpoint == "" {
		return nil
	}
	return &core.LLMConfig{
		Endpoint:          endpoint,
		Model:             model,
		APIKey:            envOr("OPENAI_API_KEY", "ollama"),
		Timeout:           timeout,
		EnforceGuardrails: true,
	}
}

// parseTimebox turns the --timebox flag string into a duration; falls
// back to 60s on parse failure so a typo can't accidentally uncap the
// loop.
func parseTimebox(s string) time.Duration {
	if d, err := time.ParseDuration(strings.TrimSpace(s)); err == nil && d > 0 {
		return d
	}
	return 60 * time.Second
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
