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
// a live URL via a live, turn-by-turn agent loop: the engine navigates to
// the target, hands the model one action's worth of choice at a time (click,
// fill, navigate, read the page, record a finding), executes it against a
// real browser session, and feeds the observed result back before the model
// decides its next move. This requires an LLM (--llm) to do anything at all
// — there is no template-only fallback, since the whole point of this
// command is letting the model reason about what to try next rather than
// pre-declaring a batch of attacks up front.
//
// Two axes distinguish it from the other commands:
//
//  1. Ephemeral by default. Evidence (screenshots) and the findings record
//     exist only long enough for one run — written into an os.MkdirTemp
//     workdir and wiped on exit. The Gherkin-formatted report survives on
//     stdout so a human can read what was exercised. Pass --persist to keep
//     evidence and the findings file on disk (and carry triage state across
//     runs — see --findings).
//
//  2. Change-aware. On every run the CLI auto-detects the last change
//     (PR diff in CI via $GITHUB_EVENT_PATH, else `git diff HEAD~1..HEAD`
//     locally) and forwards *file paths only* to the model as a hint for
//     where to look first — never diff content, unless
//     $QUAIL_ALLOW_DIFF_TO_LLM=1.
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

		// LLM — required; explore has no deterministic-only mode.
		llmURL      string
		model       string
		llmTimeout  string
		llmProvider string

		// Timeboxed exploratory loop.
		timebox  string
		maxTurns int

		// HTML report path (auto when empty).
		htmlOut string

		// OpenAPI spec URL/path for --focus contract.
		openAPI string
	)

	cmd := &cobra.Command{
		Use:   "explore",
		Short: "Adversarial bug-hunting against a live URL",
		Long: `Hunt for real bugs against a live application via a live agent loop.

Requires --llm (or $QUAIL_LLM) — unlike the rest of quail, explore has no
deterministic-only mode. The engine navigates to --url, then repeatedly:
hands the model the current page's visible elements, lets it choose exactly
one action (navigate, click, fill, wait, read the page, or record a
confirmed finding), executes that action against a real browser session, and
feeds the result back before the next decision. Attack categories (boundary
inputs, injection probes, state corruption, race conditions, auth/access,
data edge cases, cross-feature state, interrupted flows, out-of-order
operations, role/session transitions, upstream dependency failures,
cumulative state, contract, functional) are a focus hint the model is told
to prioritise, not a fixed template it must exhaust.

Every finding the model records is validated (known category, known severity,
required expected/observed text) before it counts, and is capped to the
lowest severity tier unless it's either a security-class category (auth,
injection, upstream-dep, role-switch) or backed by a captured screenshot.

Ephemeral by default: evidence and the findings record are written to a temp
dir and wiped on exit. The Gherkin-formatted report is printed to stdout.
Pass --persist to keep them under --workdir, which also lets --findings
carry triage state (acknowledged/deferred/wontfix/...) across runs.

Change-aware by default: on every run the last change is auto-detected (PR
diff in CI, else 'git diff HEAD~1..HEAD' locally) and its file paths — never
diff content — are given to the model as a hint for where to look first.

Examples:
  # Ephemeral change-aware session (default):
  quail explore --url https://shop.example.com --llm http://localhost:11434/v1

  # Persist evidence + findings, carrying triage across runs:
  quail explore --url https://shop.example.com --llm http://localhost:11434/v1 --persist --workdir .

  # Focused auth surface only:
  quail explore --url https://shop.example.com --llm http://localhost:11434/v1 --focus auth,injection

  # CI: explicit PR number (auto-detected from $GITHUB_EVENT_PATH otherwise):
  quail explore --url https://review-42.preview.example.com --llm http://localhost:11434/v1 --pr 42`,

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
				llmProvider:  llmProvider,
				timebox:      timebox,
				maxTurns:     maxTurns,
				htmlOut:      htmlOut,
				openAPI:      openAPI,
			})
		},
	}

	f := cmd.Flags()

	f.StringVar(&targetURL, "url", "", "Target URL to probe (required)")
	_ = cmd.MarkFlagRequired("url")

	f.StringVar(&focus, "focus", "all",
		"Comma-separated attack categories the model is asked to prioritise. 'all' includes every category.\n"+
			"Valid values: boundary,injection,state-corrupt,race,auth,data-edge,\n"+
			"              cross-feature,flow-interrupt,sequence,role-switch,\n"+
			"              upstream-dep,cumulative,contract,functional")
	f.StringVar(&depth, "depth", "standard",
		"shallow | standard | deep. Only affects the fallback single-page crawl used when --llm is unset "+
			"(explore requires --llm to actually probe anything); accepted for forward compatibility.")

	// Output / persistence.
	f.BoolVar(&ephemeral, "ephemeral", true,
		"Run once and discard evidence/findings. Report streams to stdout as Gherkin. Default true.")
	f.BoolVar(&persist, "persist", false,
		"Persist evidence screenshots and the findings file under --workdir. Overrides --ephemeral.")
	f.StringVar(&findingsPath, "findings", "",
		"Path for the findings file when persisting (default: tests/e2e/docs/exploratory-findings.md). "+
			"Existing findings are loaded and merged, carrying triage status forward. Ignored when ephemeral.")
	f.StringVar(&workdir, "workdir", ".",
		"Working directory for evidence screenshots and the findings file. Ignored when ephemeral.")
	f.BoolVar(&dryRun, "dry-run", false,
		"Skip the session and explain why: agentic mode has no fixed plan to preview, since each\n"+
			"action depends on the live result of the previous one.")

	// Change-aware.
	f.IntVar(&pr, "pr", 0,
		"PR number for change context. Defaults to $GITHUB_EVENT_PATH; falls back to local `git diff HEAD~1..HEAD`.")

	// LLM (optional).
	f.StringVar(&llmProvider, "llm-provider", "",
		"openai | anthropic (default: inherits QUAIL_LLM_PROVIDER or openai). Selects the wire protocol:\n"+
			"'openai' is any OpenAI-compatible endpoint (Ollama, vLLM, OpenAI) via --llm/--model.\n"+
			"'anthropic' runs the agent loop's reasoning on a Claude model via the Anthropic API\n"+
			"(needs ANTHROPIC_API_KEY — a real Anthropic Console key; Claude Code sessions authenticated\n"+
			"via a Pro/Max subscription typically don't expose one to child processes). Purely opt-in —\n"+
			"omit this flag and nothing changes from today's OpenAI-compatible-only behaviour.")
	f.StringVar(&llmURL, "llm", "",
		"Endpoint for AI-assisted target selection and scenario composition. With --llm-provider openai\n"+
			"(default): an OpenAI-compatible URL, with or without trailing /v1 (normalised) — strictly\n"+
			"local/self-hosted, do not point at third-party endpoints. With --llm-provider anthropic:\n"+
			"optional, only needed to override the default Anthropic API endpoint (e.g. an enterprise proxy).")
	f.StringVar(&model, "model", "",
		"Model ID for the LLM endpoint (default: inherits QUAIL_MODEL or gpt-4o-mini; with\n"+
			"--llm-provider anthropic, a Claude model ID such as claude-sonnet-5)")
	f.StringVar(&llmTimeout, "llm-timeout", "",
		"Per-call LLM timeout, Go duration (default: inherits QUAIL_LLM_TIMEOUT or 60s)")
	f.StringVar(&timebox, "timebox", "",
		"Wall-clock ceiling on the exploratory session, Go duration (default: inherits QUAIL_EXPLORE_TIMEBOX or 60s). "+
			"The agent loop runs turn by turn until this expires, the model calls finish_session, or --max-turns is hit.")
	f.IntVar(&maxTurns, "max-turns", 0,
		"Cap on agent tool-call turns, independent of --timebox (default: inherits QUAIL_EXPLORE_MAX_TURNS or 40). "+
			"A safety ceiling against a model that returns instantly every time.")
	f.StringVar(&htmlOut, "html-out", "",
		"Path to write the branded HTML report. Empty (default): auto — persisted next to the ledger in --persist mode, "+
			"else under $TMPDIR with the file path echoed to stderr.")
	f.StringVar(&openAPI, "openapi", "",
		"OpenAPI 3.x spec URL or local file path. When set with --focus contract, the LLM proposes "+
			"per-endpoint contract scenarios (schema violations, missing required, status mismatches).")

	return cmd
}

type exploreOpts struct {
	targetURL, focus, depth, findingsPath, workdir string
	dryRun, ephemeral, persist                     bool
	pr                                              int
	llmURL, model, llmTimeout, llmProvider          string
	timebox                                         string
	maxTurns                                        int
	htmlOut                                         string
	openAPI                                         string
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
	if o.llmProvider == "" {
		o.llmProvider = envOr("QUAIL_LLM_PROVIDER", "openai")
	}
	if o.timebox == "" {
		o.timebox = envOr("QUAIL_EXPLORE_TIMEBOX", "60s")
	}
	if o.maxTurns <= 0 {
		if v := os.Getenv("QUAIL_EXPLORE_MAX_TURNS"); v != "" {
			fmt.Sscanf(v, "%d", &o.maxTurns)
		}
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

	llmCfg := exploreLLMConfigOrNil(o.llmURL, o.model, o.llmTimeout, o.llmProvider)
	cfg := core.ExploreConfig{
		TargetURL:      o.targetURL,
		Categories:     categories,
		Depth:          o.depth,
		FindingsPath:   o.findingsPath,
		WorkDir:        o.workdir,
		DryRun:         o.dryRun,
		Ephemeral:      ephemeral,
		Changes:        changes,
		LLM:            llmCfg,
		GuardrailsSpec: spec.ExploreGuardrails,
		Timebox:        parseTimebox(o.timebox),
		OpenAPISpec:    o.openAPI,
		MaxTurns:       o.maxTurns,
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
		// The full report (turn count, stop reason, transcript) only
		// ever lived in result.Report in memory — nothing persisted it
		// before, so a --persist run's diagnostic detail vanished the
		// moment the process exited. Save it alongside the findings
		// file so it survives.
		reportPath := filepath.Join(o.workdir, "report.md")
		if err := os.WriteFile(reportPath, []byte(result.Report), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: could not write %s: %v\n", reportPath, err)
			reportPath = ""
		}
		fmt.Printf("  evidence → %s\n  findings → %s\n", filepath.Join(result.SpecsDir, "explore-evidence"), result.FindingsPath)
		if reportPath != "" {
			fmt.Printf("  report   → %s\n", reportPath)
		}
	}

	// HTML report — always attempt to write, so consumers never need a
	// second post-processing step. Silent on error (falls back to the
	// Gherkin/markdown stdout everything else keys off of).
	if htmlPath := writeExploreHTML(result, o, ephemeral, llmCfg != nil); htmlPath != "" {
		fmt.Fprintf(os.Stderr, "  html     → %s\n", htmlPath)
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

// writeExploreHTML renders the report as HTML and drops it at the
// first-available destination. Silent-nil on any error — HTML is a nice-to-have
// beside the stdout report, not a hard dependency. Returns the on-disk path
// so the caller can announce it on stderr.
//
// Branches on which engine path produced result: the agent loop (agentic
// true — an LLM config was actually built, regardless of provider or
// whether --llm itself was set) has its findings/transcript as structured
// data already and renders via explorehtml.RenderAgentSession directly
// from that — re-parsing result.Report's markdown the way explorehtml.Render
// does for the deterministic-only path doesn't recognise the agent loop's
// report shape at all (confirmed the hard way: a real high-severity finding
// rendered as "No issues surfaced" before this branch existed).
func writeExploreHTML(result *core.ExploreResult, o exploreOpts, ephemeral, agentic bool) string {
	if result == nil {
		return ""
	}

	meta := explorehtml.Meta{
		TargetURL: o.targetURL,
		Generated: time.Now().UTC().Format(time.RFC3339),
	}

	var rendered string
	var err error
	if agentic {
		rendered, err = explorehtml.RenderAgentSession(result.Findings, explorehtml.AgentSummary{
			Turns:           result.Rounds,
			StopReason:      result.StopReason,
			SessionDuration: result.SessionDuration.Round(time.Second).String(),
			PagesVisited:    result.PagesVisited,
			Transcript:      result.Transcript,
		}, meta)
	} else {
		if strings.TrimSpace(result.Report) == "" {
			return ""
		}
		rendered, err = explorehtml.Render(result.Report, meta)
	}
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

// loadExploreDiff resolves the "last change" forwarded to the model as a
// where-to-look-first hint (file paths only, never diff content unless
// $QUAIL_ALLOW_DIFF_TO_LLM=1). Order: explicit --pr / $QUAIL_PR /
// $GITHUB_EVENT_PATH, then local `git diff HEAD~1..HEAD`. Nil is a valid
// result — the engine treats a missing diff as "no change context".
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
		"contract", "functional",
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

// exploreLLMConfigOrNil returns nil when there's nothing usable to run
// with. With no config, the core runner falls back to a single-page
// crawl and skips the agent loop entirely — explore has no meaningful
// adversarial behaviour without an LLM.
//
// Two providers, two different "nothing configured" conditions:
//   - openai (default): nil unless --llm/QUAIL_LLM gave an endpoint —
//     unchanged from before this function knew about providers.
//   - anthropic: nil unless ANTHROPIC_API_KEY is actually set (a real
//     Anthropic Console key — see the --llm-provider flag help for why
//     that isn't automatic just because this runs inside Claude Code).
//     --llm/endpoint is optional here, only used to override the
//     default Anthropic API URL for e.g. an enterprise proxy.
//
// Returns a plain *config.Config (explore's own core.LLMConfig type was
// folded into it) — core.newLLMClient still re-normalizes the OpenAI
// base URL and re-applies defaults defensively for that provider, so
// this doesn't need to duplicate that.
func exploreLLMConfigOrNil(endpoint, model, timeout, provider string) *config.Config {
	if strings.TrimSpace(provider) == "" {
		provider = "openai"
	}
	if provider == "anthropic" {
		key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
		if key == "" {
			return nil
		}
		return &config.Config{
			LLMProvider:      "anthropic",
			AnthropicAPIKey:  key,
			AnthropicBaseURL: endpoint, // optional override; empty = real Anthropic endpoint
			Model:            model,
			LLMTimeout:       parseTimebox(timeout),
		}
	}
	if endpoint == "" {
		return nil
	}
	return &config.Config{
		LLMProvider:   "openai",
		OpenAIBaseURL: endpoint,
		Model:         model,
		OpenAIAPIKey:  envOr("OPENAI_API_KEY", "ollama"),
		LLMTimeout:    parseTimebox(timeout),
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
