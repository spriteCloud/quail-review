package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	_ "github.com/reviewqa/reviewqa/internal/ast/golang"
	_ "github.com/reviewqa/reviewqa/internal/ast/java"
	_ "github.com/reviewqa/reviewqa/internal/ast/proto"
	_ "github.com/reviewqa/reviewqa/internal/ast/python"
	_ "github.com/reviewqa/reviewqa/internal/ast/ts"

	"github.com/reviewqa/reviewqa/internal/compat"
	"github.com/reviewqa/reviewqa/internal/config"
	"github.com/reviewqa/reviewqa/internal/diff"
	"github.com/reviewqa/reviewqa/internal/gen"
	"github.com/reviewqa/reviewqa/internal/gh"
	"github.com/reviewqa/reviewqa/internal/heal"
	"github.com/reviewqa/reviewqa/internal/integration"
	"github.com/reviewqa/reviewqa/internal/ledger"
	"github.com/reviewqa/reviewqa/internal/llm"
	rlog "github.com/reviewqa/reviewqa/internal/log"
	"github.com/reviewqa/reviewqa/internal/merge"
	"github.com/reviewqa/reviewqa/internal/plan"
	"github.com/reviewqa/reviewqa/internal/probe"
	"github.com/reviewqa/reviewqa/internal/prompt"
)

// loadSentinelItems reads the bug-discovery ledger and emits one
// sentinel `test.fail()` spec per open finding. v0.40 closes the
// "Achilles bug-sentinel" gap from the plan.
func loadSentinelItems(workDir string) []plan.Item {
	body, err := os.ReadFile(filepath.Join(workDir, "tests/e2e/docs/findings.md"))
	if err != nil {
		return nil
	}
	findings := parseLedger(body)
	if len(findings) == 0 {
		return nil
	}
	items := ledger.EmitSentinels(findings)
	rlog.Info("ledger: emitting sentinel specs", "count", len(items))
	return items
}

// parseLedger is a tiny wrapper around ledger.parseLedger (which is
// package-private) — we re-implement the minimum here so cmd/reviewqa
// doesn't depend on internal symbols.
func parseLedger(body []byte) []ledger.Finding {
	var out []ledger.Finding
	for _, line := range strings.Split(string(body), "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "|") || !strings.HasSuffix(t, "|") {
			continue
		}
		if strings.HasPrefix(t, "|---") || strings.Contains(t, "| Spec |") || strings.Contains(t, "|---|") {
			continue
		}
		parts := strings.Split(strings.Trim(t, "|"), "|")
		if len(parts) != 7 {
			continue
		}
		out = append(out, ledger.Finding{
			Spec:      strings.Trim(strings.TrimSpace(parts[0]), "`"),
			Test:      strings.TrimSpace(parts[1]),
			Symptom:   strings.TrimSpace(parts[2]),
			FirstSeen: strings.TrimSpace(parts[3]),
			LastSeen:  strings.TrimSpace(parts[4]),
			Severity:  strings.TrimSpace(parts[5]),
			Status:    strings.TrimSpace(parts[6]),
		})
	}
	return out
}

// loadIntegrationItems reads reviewqa.yml from the work directory and
// returns integration-test plan.Items. Empty when the config is
// missing or declares no resources.
func loadIntegrationItems(workDir string) []plan.Item {
	cfg, err := integration.Load(workDir)
	if err != nil {
		rlog.Warn("integration: skipping reviewqa.yml", "err", err)
		return nil
	}
	if cfg.IsEmpty() {
		return nil
	}
	items := integration.EmitItems(cfg)
	rlog.Info("integration: emitting items from reviewqa.yml", "count", len(items))
	return items
}

// compareSchema is the plan.CompatComparator implementation. Classifies
// `path` by extension + content, delegates to the right compat.X
// function, returns ("openapi"|"proto"|"asyncapi", regressions, nil).
func compareSchema(path string, old, new_ []byte) (string, []plan.CompatRegression, error) {
	lowerPath := strings.ToLower(path)
	wrap := func(kind string, regs []compat.Regression, err error) (string, []plan.CompatRegression, error) {
		if err != nil {
			return "", nil, err
		}
		out := make([]plan.CompatRegression, 0, len(regs))
		for _, r := range regs {
			out = append(out, plan.CompatRegression{Kind: r.Kind, Detail: r.Detail})
		}
		return kind, out, nil
	}
	switch {
	case strings.HasSuffix(lowerPath, ".proto"):
		regs, err := compat.Proto(old, new_)
		return wrap("proto", regs, err)
	case strings.Contains(string(new_), "\"openapi\"") || strings.Contains(string(new_), "openapi:") ||
		strings.Contains(string(new_), "\"swagger\"") || strings.Contains(string(new_), "swagger:"):
		regs, err := compat.OpenAPI(old, new_)
		return wrap("openapi", regs, err)
	case strings.Contains(string(new_), "\"asyncapi\"") || strings.Contains(string(new_), "asyncapi:"):
		regs, err := compat.AsyncAPI(old, new_)
		return wrap("asyncapi", regs, err)
	}
	return "", nil, nil
}

var (
	version = "0.77"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	root := newRoot()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "reviewqa",
		Short:         "Generate tests for a PR and heal broken Playwright locators.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}
	root.AddCommand(newGenerateCmd(), newHealCmd(), newScanCmd(), newProbeCmd(), newPromptCmd(), newLedgerCmd(), newRunOnceCmd(), newServeCmd())
	return root
}

func newGenerateCmd() *cobra.Command {
	var pr int
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Scan a PR's diff, emit test scaffolds, and open a follow-up PR.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			if err := cfg.Validate(); err != nil {
				return err
			}
			if pr == 0 {
				pr = cfg.PRNumber
			}
			if pr == 0 {
				pr = readPRFromEvent()
			}
			// PR is OPTIONAL when target URLs are configured — generate then runs
			// in pure-probe mode against the URLs and commits against main.
			if pr == 0 && os.Getenv("REVIEWQA_TARGET_URLS") == "" {
				return fmt.Errorf("missing --pr; set $REVIEWQA_PR, run inside a pull_request event, or set REVIEWQA_TARGET_URLS")
			}
			cfg.PRNumber = pr
			cfg.DryRun = dryRun
			return runGenerate(cmd.Context(), cfg)
		},
	}
	cmd.Flags().IntVar(&pr, "pr", 0, "PR number")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print plan instead of opening a PR")
	return cmd
}

func newHealCmd() *cobra.Command {
	var pr int
	var report string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "heal",
		Short: "Repair broken Playwright locators (defaults to on-failure).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			if err := cfg.Validate(); err != nil {
				return err
			}
			if pr == 0 {
				pr = cfg.PRNumber
			}
			if pr == 0 {
				pr = readPRFromEvent()
			}
			cfg.PRNumber = pr
			cfg.DryRun = dryRun
			if report != "" {
				cfg.PlaywrightReport = report
			}
			return runHeal(cmd.Context(), cfg)
		},
	}
	cmd.Flags().IntVar(&pr, "pr", 0, "PR number")
	cmd.Flags().StringVar(&report, "report", "", "Path to Playwright JSON report (on-failure mode)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print edits instead of opening a PR")
	return cmd
}

func newProbeCmd() *cobra.Command {
	var urls []string
	var dryRun bool
	var local bool
	var coverage string
	var llm string
	var ignoreRobots bool
	cmd := &cobra.Command{
		Use:   "probe",
		Short: "Fetch live URL(s), generate a Playwright happy-flow per URL, open a PR.",
		Long: `Probe a live URL and generate a full Playwright + Gherkin suite.

LLM scenario composer (OPTIONAL):
  --llm <url>   Enable the scenario composer against an OpenAI-compatible
                endpoint (e.g. http://100.82.34.115:11434 for a local
                Ollama). Adds up to 3 extra @llm-composed Scenarios per
                journey. STRICTLY local-only — the DGX is on Netbird and
                unreachable from public CI; the generated .feature files
                still run anywhere because they're plain Gherkin.

  REVIEWQA_LLM env var is the equivalent of --llm.
  REVIEWQA_MODEL overrides the model id (default: qwen3-coder-next:latest
                 when --llm is set).
  REVIEWQA_LLM_TIMEOUT bounds each LLM call (default 60s — bump on
                 slower hardware; e.g. 120s for a local model that
                 takes longer to respond).
  REVIEWQA_HUMANIZE=0 skips the per-file humanization pass while
                 keeping the composer active. Useful when the
                 generator is many specs deep and per-file LLM
                 calls would saturate your wall-clock budget.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			if len(urls) == 0 {
				if env := os.Getenv("REVIEWQA_TARGET_URLS"); env != "" {
					urls = strings.Split(env, ",")
				}
			}
			if len(urls) == 0 {
				return fmt.Errorf("probe: no urls provided (pass --url or set REVIEWQA_TARGET_URLS)")
			}
			cfg.DryRun = dryRun
			applyLLMOverride(&cfg, llm)
			applyIgnoreRobots(ignoreRobots)
			return runProbe(cmd.Context(), cfg, urls, probe.ParseCoverage(coverage), local)
		},
	}
	cmd.Flags().StringSliceVar(&urls, "url", nil, "URL to probe (repeatable; may also be set via REVIEWQA_TARGET_URLS env)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print rendered spec(s) instead of opening a PR")
	cmd.Flags().BoolVar(&local, "local", false, "Write rendered files directly into the workdir; skip the gh PR-open path (no GITHUB_TOKEN needed). Used by reviewqa serve's HOME probe.")
	cmd.Flags().StringVar(&coverage, "coverage", coverageDefault(), "Coverage mode: breadth | standard | depth | max (env: REVIEWQA_COVERAGE)")
	cmd.Flags().StringVar(&llm, "llm", llmDefault(), "LLM scenario composer endpoint (e.g. http://100.82.34.115:11434). Local-only; never set in CI. (env: REVIEWQA_LLM)")
	cmd.Flags().BoolVar(&ignoreRobots, "ignore-robots", false, "Crawl pages disallowed by robots.txt. Default OFF — only enable for QA of sites you own.")
	return cmd
}

// applyIgnoreRobots forwards the CLI flag into the env var the probe
// layer consults at crawl time. Keeps the probe package decoupled from
// cobra. v0.41b.
func applyIgnoreRobots(ignore bool) {
	if ignore {
		os.Setenv("REVIEWQA_IGNORE_ROBOTS", "1")
	}
}

// applyLLMOverride enables the composer when --llm is provided. Sets
// OpenAIBaseURL + Model + API key on cfg so the existing llm.New uses
// the local endpoint with the qwen-coder-next default model.
func applyLLMOverride(cfg *config.Config, llmURL string) {
	llmURL = strings.TrimSpace(llmURL)
	if llmURL == "" {
		return
	}
	cfg.OpenAIBaseURL = strings.TrimRight(llmURL, "/") + "/v1"
	if cfg.Model == "" || cfg.Model == "gpt-4o-mini" {
		cfg.Model = "qwen3-coder-next:latest"
	}
	if cfg.OpenAIAPIKey == "" {
		// Ollama doesn't require a key but the existing llm client
		// gates on key presence; populate with a sentinel.
		cfg.OpenAIAPIKey = "ollama"
	}
	rlog.Info("llm composer enabled (local-only)", "endpoint", cfg.OpenAIBaseURL, "model", cfg.Model)
}

// llmDefault reads $REVIEWQA_LLM, defaulting to empty.
func llmDefault() string {
	return strings.TrimSpace(os.Getenv("REVIEWQA_LLM"))
}

func newPromptCmd() *cobra.Command {
	var urls []string
	var dryRun bool
	var evidence bool
	var coverage string
	var llm string
	var ignoreRobots bool
	cmd := &cobra.Command{
		Use:   "prompt [text]",
		Short: "Generate Playwright tests for a focused area expressed as a natural-language prompt.",
		Long: `Parse the prompt into a journey-kind filter, probe the target URL, and
generate Playwright specs only for the journeys that match the prompt.

When the prompt produces no recognised signal (e.g. all stop-words) the
probe runs unfiltered with a warning. The probe layer is the same one
the bare ` + "`probe`" + ` command uses; set REVIEWQA_BROWSER_PROBE=1 to
drive Chromium when the target site is JS-rendered.

With --evidence, the command writes the generated specs to disk, runs
npx playwright test against them, and bundles the resulting
playwright-report/ + test-results/ into tests/e2e/evidence-<timestamp>.zip
so reviewers see "we ran it and here's what happened" in one artifact.

Examples:
  reviewqa prompt "test the checkout flow" --url https://shop.example.com
  reviewqa prompt "verify the contact form rejects invalid emails" --url https://x.com --evidence
  reviewqa prompt "explore the docs section" --url https://docs.x.com`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromEnv()
			text := strings.Join(args, " ")
			if len(urls) == 0 {
				if env := os.Getenv("REVIEWQA_TARGET_URLS"); env != "" {
					urls = strings.Split(env, ",")
				}
			}
			if len(urls) == 0 {
				return fmt.Errorf("prompt: no urls provided (pass --url or set REVIEWQA_TARGET_URLS)")
			}
			cfg.DryRun = dryRun
			applyLLMOverride(&cfg, llm)
			applyIgnoreRobots(ignoreRobots)
			filter := prompt.Parse(text)
			rlog.Info("prompt parsed", "summary", filter.Describe())
			cov := probe.ParseCoverage(coverage)
			if evidence {
				return runPromptEvidence(cmd.Context(), cfg, urls, filter, cov)
			}
			return runProbeWithFilter(cmd.Context(), cfg, urls, filter, cov)
		},
	}
	cmd.Flags().StringSliceVar(&urls, "url", nil, "URL to probe (repeatable; may also be set via REVIEWQA_TARGET_URLS env)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print rendered spec(s) instead of opening a PR")
	cmd.Flags().BoolVar(&evidence, "evidence", false, "Write specs to disk, run them, and bundle playwright-report/+test-results/ into a ZIP")
	cmd.Flags().StringVar(&coverage, "coverage", coverageDefault(), "Coverage mode: breadth | standard | depth | max (env: REVIEWQA_COVERAGE)")
	cmd.Flags().StringVar(&llm, "llm", llmDefault(), "LLM scenario composer endpoint (local-only; never set in CI). (env: REVIEWQA_LLM)")
	cmd.Flags().BoolVar(&ignoreRobots, "ignore-robots", false, "Crawl pages disallowed by robots.txt. Default OFF — only enable for QA of sites you own.")
	return cmd
}

// coverageDefault reads $REVIEWQA_COVERAGE, falling back to "standard"
// when unset. Used as the cobra flag's Default so `--help` shows
// whatever the environment has chosen.
func coverageDefault() string {
	if v := strings.TrimSpace(os.Getenv("REVIEWQA_COVERAGE")); v != "" {
		return v
	}
	return string(probe.CoverageStandard)
}

func newScanCmd() *cobra.Command {
	var pr int
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Dry-run: print what generate/heal would do without opening a PR.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			if pr == 0 {
				pr = cfg.PRNumber
			}
			if pr == 0 {
				pr = readPRFromEvent()
			}
			cfg.PRNumber = pr
			cfg.DryRun = true
			return runGenerate(cmd.Context(), cfg)
		},
	}
	cmd.Flags().IntVar(&pr, "pr", 0, "PR number")
	return cmd
}

func runGenerate(ctx context.Context, cfg config.Config) error {
	client, err := gh.New(ctx, cfg)
	if err != nil && !cfg.DryRun {
		return err
	}
	files, prInfo, err := fetchPRFilesAndInfo(ctx, client, cfg.PRNumber)
	if err != nil {
		return err
	}
	layout := plan.Detect(cfg.WorkDir)
	items := plan.Build(files, layout)
	// v0.24: when the PR diff touches a schema file, append one
	// compatibility-test item per detected breaking-change set.
	items = append(items, plan.BuildCompat(files, compareSchema)...)
	// v0.27: when reviewqa.yml is present, emit integration items.
	items = append(items, loadIntegrationItems(cfg.WorkDir)...)
	// v0.40: every open finding in the bug-discovery ledger becomes a
	// sentinel test.fail() spec. Resolved findings are skipped.
	items = append(items, loadSentinelItems(cfg.WorkDir)...)
	if probeURLs := nonEmptyURLs(os.Getenv("REVIEWQA_TARGET_URLS")); len(probeURLs) > 0 {
		probeItems, probeErrs := probe.RunAll(ctx, probeURLs)
		for _, e := range probeErrs {
			rlog.Warn("probe url failed", "err", e)
		}
		items = append(items, probeItems...)
	}
	if len(items) == 0 {
		rlog.Info("no symbols in PR diff and no target URLs to probe; nothing to generate")
		writeStepSummary("reviewqa: no new symbols in PR diff and no target URLs configured.\n")
		return nil
	}
	rendered, err := gen.Render(items, cfg.WorkDir)
	if err != nil {
		return fmt.Errorf("render: %w", err)
	}
	llmClient := llm.New(cfg)
	for i := range rendered {
		rendered[i].Content = llmClient.Humanize(ctx, rendered[i].Symbol.Language, rendered[i].Symbol.Name, rendered[i].Content)
	}
	if cfg.DryRun || client == nil {
		printRendered(rendered)
		return nil
	}
	if prInfo == nil {
		return runGenerateStandalone(ctx, client, cfg, rendered, nonEmptyURLs(os.Getenv("REVIEWQA_TARGET_URLS")))
	}
	branch := fmt.Sprintf("%s/tests-pr-%d-%s", cfg.BranchPrefix, cfg.PRNumber, shortSHA(prInfo.HeadSHA))
	body := genPRBody(prInfo, rendered)
	files2 := applyExistingFileMerge(ctx, client, rendered, prInfo.HeadSHA)
	url, err := client.OpenPR(ctx, gh.PROpts{
		BaseBranch: prInfo.HeadBranch, NewBranch: branch,
		Title: fmt.Sprintf("reviewqa: tests for PR #%d", cfg.PRNumber),
		Body:  body, Files: files2,
	})
	if err != nil {
		return fmt.Errorf("open pr: %w", err)
	}
	rlog.Info("opened test PR", "url", url)
	writeStepSummary(generateSummary(prInfo, rendered, url))
	return nil
}

// runGenerateStandalone handles the no-PR path — only target URLs are
// configured, so the diff fetch was skipped. We open a PR against main with
// the probe-derived specs.
func runGenerateStandalone(ctx context.Context, client *gh.Client, cfg config.Config, rendered []gen.Rendered, urls []string) error {
	// Mirror probe-mode idempotency: derive the branch from the host of
	// the first probed URL when one is available, so subsequent runs
	// amend the same PR.
	branch := probeBranchName(cfg, urls)
	body := genPRBody(nil, rendered)
	files := map[string][]byte{}
	for _, r := range rendered {
		files[r.Path] = r.Content
	}
	url, err := client.OpenPR(ctx, gh.PROpts{
		BaseBranch: "main", NewBranch: branch,
		Title: "reviewqa: probe-generated Playwright tests",
		Body:  body, Files: files,
	})
	if err != nil {
		return fmt.Errorf("open pr: %w", err)
	}
	rlog.Info("opened probe PR (standalone)", "url", url)
	return nil
}

// probeBranchName returns a stable host-derived branch name when exactly
// one usable URL is available — so re-running the probe against the same
// site amends the previous companion PR instead of opening a new one.
// Falls back to a timestamp when the host can't be reliably extracted
// (zero URLs, multiple URLs, malformed input).
func probeBranchName(cfg config.Config, urls []string) string {
	if len(urls) == 1 {
		if u, err := url.Parse(strings.TrimSpace(urls[0])); err == nil && u.Host != "" {
			slug := strings.ToLower(u.Host)
			slug = strings.TrimPrefix(slug, "www.")
			slug = strings.ReplaceAll(slug, ".", "-")
			if slug != "" {
				return fmt.Sprintf("%s/probe-%s", cfg.BranchPrefix, slug)
			}
		}
	}
	return fmt.Sprintf("%s/probe-%s", cfg.BranchPrefix, time.Now().UTC().Format("20060102-150405"))
}

// siblingPath inserts "_reviewqa" before the test suffix so the generated
// file lands next to the existing one instead of overwriting it.
//
//	internal/diff/diff_test.go   -> internal/diff/diff_reviewqa_test.go
//	src/foo.test.ts              -> src/foo.reviewqa.test.ts
//	tests/test_users.py          -> tests/test_users_reviewqa.py
//	src/test/java/x/YTest.java   -> src/test/java/x/YReviewqaTest.java
func siblingPath(p string) string {
	dir, base := filepath.Split(p)
	switch {
	case strings.HasSuffix(base, "_test.go"):
		return dir + strings.TrimSuffix(base, "_test.go") + "_reviewqa_test.go"
	case strings.HasSuffix(base, ".test.ts"):
		return dir + strings.TrimSuffix(base, ".test.ts") + ".reviewqa.test.ts"
	case strings.HasSuffix(base, ".test.js"):
		return dir + strings.TrimSuffix(base, ".test.js") + ".reviewqa.test.js"
	case strings.HasSuffix(base, ".spec.ts"):
		return dir + strings.TrimSuffix(base, ".spec.ts") + ".reviewqa.spec.ts"
	case strings.HasSuffix(base, ".py"):
		return dir + strings.TrimSuffix(base, ".py") + "_reviewqa.py"
	case strings.HasSuffix(base, "Test.java"):
		return dir + strings.TrimSuffix(base, "Test.java") + "ReviewqaTest.java"
	}
	ext := filepath.Ext(base)
	return dir + strings.TrimSuffix(base, ext) + "_reviewqa" + ext
}

// runProbeWithFilter is the prompt-driven variant: same probe pipeline
// as runProbe but with a journey-filter applied before generation.
func runProbeWithFilter(ctx context.Context, cfg config.Config, urls []string, f prompt.Filter, c probe.CoverageMode) error {
	items, errs := probe.RunAllWithCoverage(ctx, urls, f, c)
	return finishProbe(ctx, cfg, urls, items, errs, false)
}

// runProbe fetches each URL, renders a Playwright happy-flow per URL,
// and either prints them (dry-run), writes them locally (local), or
// opens a PR with the new specs.
func runProbe(ctx context.Context, cfg config.Config, urls []string, c probe.CoverageMode, local bool) error {
	items, errs := probe.RunAllWithCoverage(ctx, urls, nil, c)
	return finishProbe(ctx, cfg, urls, items, errs, local)
}

// finishProbe shares the post-probe pipeline (render, humanize, dry-run
// vs local-write vs PR-open) between runProbe and runProbeWithFilter
// so neither path drifts from the other.
//
// `local` (set by --local on the probe CLI, used by reviewqa serve's
// HOME probe form) writes rendered files into cfg.WorkDir directly
// and skips the gh.New/OpenPR path entirely — no GITHUB_TOKEN
// required.
func finishProbe(ctx context.Context, cfg config.Config, urls []string, items []plan.Item, errs []error, local bool) error {
	for _, e := range errs {
		rlog.Warn("probe url failed", "err", e)
	}
	if len(items) == 0 {
		rlog.Info("probe: no items produced")
		return nil
	}
	// v0.25: LLM scenario composer — strictly opt-in via --llm.
	// Mutates feature items in-place to attach ExtraScenarios.
	items = composeScenarios(ctx, cfg, items)
	rendered, err := gen.Render(items, cfg.WorkDir)
	if err != nil {
		return fmt.Errorf("probe render: %w", err)
	}
	llmClient := llm.New(cfg)
	for i := range rendered {
		rendered[i].Content = llmClient.Humanize(ctx, rendered[i].Symbol.Language, rendered[i].Symbol.Name, rendered[i].Content)
	}
	if cfg.DryRun {
		printRendered(rendered)
		return nil
	}
	if local {
		return writeRenderedLocal(cfg.WorkDir, rendered)
	}
	client, err := gh.New(ctx, cfg)
	if err != nil {
		return err
	}
	prInfo := &prSummary{HeadBranch: defaultBaseBranch(cfg)}
	files := applyExistingFileMerge(ctx, client, rendered, "HEAD")
	// Idempotent companion-PR branch name: one branch per probed host,
	// not one per timestamp. OpenPR's existing same-branch-update path
	// then amends the prior probe PR instead of opening a new one each
	// run. Falls back to a timestamped name when no usable host can be
	// extracted (multiple URLs, malformed input, etc).
	branch := probeBranchName(cfg, urls)
	url, err := client.OpenPR(ctx, gh.PROpts{
		BaseBranch: prInfo.HeadBranch, NewBranch: branch,
		Title: "reviewqa: probe-generated Playwright tests",
		Body:  probePRBody(urls, rendered),
		Files: files,
	})
	if err != nil {
		return fmt.Errorf("probe open pr: %w", err)
	}
	rlog.Info("opened probe PR", "url", url)
	return nil
}

func defaultBaseBranch(cfg config.Config) string {
	if cfg.PRNumber != 0 {
		// Mirror the generate path — the heal/generate commands carry PR-derived
		// HeadBranch; for stand-alone probe runs default to main.
	}
	return "main"
}

func probePRBody(urls []string, rs []gen.Rendered) string {
	var b strings.Builder
	b.WriteString("Generated by reviewqa probe.\n\nTarget URL(s):\n")
	for _, u := range urls {
		fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(u))
	}
	b.WriteString("\nFiles:\n")
	for _, r := range rs {
		fmt.Fprintf(&b, "- `%s` — covers `%s`\n", r.Path, r.Symbol.Name)
	}
	b.WriteString("\nDeterministic scaffolds against live URLs. Review and extend with edge cases.\n")
	appendQualityNotes(&b, rs)
	return b.String()
}

// nonEmptyURLs splits a comma-separated list and trims/filters empties.
func nonEmptyURLs(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for _, u := range strings.Split(raw, ",") {
		if t := strings.TrimSpace(u); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// applyExistingFileMerge folds rendered scaffolds into the existing tree:
// append-where-possible, sibling-when-merge-unsupported, fresh otherwise.
// Mutates rendered[i].Content/Path when the file already exists.
//
// IfMissingOnly items short-circuit the merge: when the file already
// exists, they're dropped from the output. Used by the bug-discovery
// ledger so accumulated rows aren't clobbered by a re-probe.
func applyExistingFileMerge(ctx context.Context, client *gh.Client, rendered []gen.Rendered, headSHA string) map[string][]byte {
	out := map[string][]byte{}
	for i, r := range rendered {
		existing, found, err := client.ReadFile(ctx, r.Path, headSHA)
		if err != nil {
			rlog.Warn("could not check existing test file; will write fresh", "path", r.Path, "err", err)
		}
		if !found {
			out[r.Path] = r.Content
			continue
		}
		if r.IfMissingOnly {
			rlog.Info("skipping if-missing-only file (already present)", "path", r.Path)
			continue
		}
		mergeOneOrSibling(&rendered[i], r, []byte(existing), out)
	}
	return out
}

func mergeOneOrSibling(slot *gen.Rendered, r gen.Rendered, existing []byte, out map[string][]byte) {
	if merged, ok := merge.Append(r.Symbol.Language, existing, r.Content); ok {
		rlog.Info("appending to existing test file", "path", r.Path, "symbol", r.Symbol.Name)
		slot.Content = merged
		out[r.Path] = merged
		return
	}
	alt := siblingPath(r.Path)
	rlog.Info("existing test file present; writing to sibling", "from", r.Path, "to", alt)
	slot.Path = alt
	out[alt] = r.Content
}

func runHeal(ctx context.Context, cfg config.Config) error {
	if cfg.HealMode == config.HealOff {
		rlog.Info("heal mode = off; skipping")
		return nil
	}
	llmClient := llm.New(cfg)
	client, err := gh.New(ctx, cfg)
	if err != nil && !cfg.DryRun {
		return err
	}
	files, prInfo, err := fetchPRFilesAndInfo(ctx, client, cfg.PRNumber)
	if err != nil {
		return err
	}
	report, err := loadReportIfNeeded(cfg)
	if err != nil {
		return err
	}
	edits, err := heal.Run(ctx, cfg, files, report, llmClient)
	if err != nil {
		return err
	}
	if handled := emitOrSkipHealOutput(edits, cfg, client, prInfo); handled {
		return nil
	}
	return openHealPR(ctx, client, cfg, files, prInfo, edits)
}

// emitOrSkipHealOutput handles the three terminal cases for runHeal: no
// edits to apply (logs + writes summary), or the dry-run / missing-PR path
// (prints edits to stdout). Returns true when the caller should return nil
// without opening a PR.
func emitOrSkipHealOutput(edits []heal.Edit, cfg config.Config, client *gh.Client, prInfo *prSummary) bool {
	if len(edits) == 0 {
		rlog.Info("no locator edits to apply")
		writeStepSummary("reviewqa: no locator edits to apply.\n")
		return true
	}
	if cfg.DryRun || client == nil || prInfo == nil {
		printEdits(edits)
		return true
	}
	return false
}

// fetchPRFilesAndInfo pulls the PR's diff + blobs through the GitHub client.
// Returns (nil, nil, nil) when no client/PR is available — caller is
// expected to handle that as "skip heal" or "dry-run".
func fetchPRFilesAndInfo(ctx context.Context, client *gh.Client, prNum int) ([]diff.File, *prSummary, error) {
	if client == nil || prNum == 0 {
		return nil, nil, nil
	}
	raw, prObj, err := client.FetchDiff(ctx, prNum)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch diff: %w", err)
	}
	files := diff.Parse(raw)
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	newBlobs, oldBlobs, _ := client.FileBlobs(ctx, prObj, paths)
	for i := range files {
		if v, ok := newBlobs[files[i].Path]; ok {
			files[i].NewBlob = v
		}
		if v, ok := oldBlobs[files[i].Path]; ok {
			files[i].OldBlob = v
		}
	}
	return files, &prSummary{
		Number: prObj.GetNumber(), Title: prObj.GetTitle(),
		HeadBranch: prObj.GetHead().GetRef(),
		HeadSHA:    prObj.GetHead().GetSHA(),
		URL:        prObj.GetHTMLURL(),
	}, nil
}

// loadReportIfNeeded loads the Playwright JSON report when heal mode is
// `on-failure`. Returns (nil, nil) otherwise.
func loadReportIfNeeded(cfg config.Config) (*heal.PlaywrightReport, error) {
	if cfg.HealMode != config.HealOnFailure {
		return nil, nil
	}
	path := cfg.PlaywrightReport
	if path == "" {
		path = filepath.Join(cfg.WorkDir, "playwright-report.json")
	}
	report, err := heal.LoadReport(path)
	if err != nil {
		return nil, fmt.Errorf("load report (%s): %w; pass --report or set REVIEWQA_HEAL_MODE=proactive", path, err)
	}
	return report, nil
}

func openHealPR(ctx context.Context, client *gh.Client, cfg config.Config, files []diff.File, prInfo *prSummary, edits []heal.Edit) error {
	indexed := map[string]string{}
	for _, f := range files {
		if f.NewBlob != "" {
			indexed[f.Path] = f.NewBlob
		}
	}
	patched := applyEdits(cfg.WorkDir, indexed, edits)
	branch := fmt.Sprintf("%s/heal-pr-%d-%s", cfg.BranchPrefix, cfg.PRNumber, shortSHA(prInfo.HeadSHA))
	url, err := client.OpenPR(ctx, gh.PROpts{
		BaseBranch: prInfo.HeadBranch, NewBranch: branch,
		Title: fmt.Sprintf("reviewqa: heal Playwright locators for PR #%d", cfg.PRNumber),
		Body:  healPRBody(prInfo, edits),
		Files: patched,
	})
	if err != nil {
		return err
	}
	rlog.Info("opened heal PR", "url", url)
	writeStepSummary(healSummary(prInfo, edits, url))
	return nil
}

type prSummary struct {
	Number     int
	Title      string
	HeadBranch string
	HeadSHA    string
	URL        string
}

func printRendered(rs []gen.Rendered) {
	for _, r := range rs {
		fmt.Println("---", r.Path, "---")
		fmt.Println(string(r.Content))
	}
}

// writeRenderedLocal writes each rendered file into workDir directly,
// creating parent directories as needed. Used by the probe `--local`
// path (and `reviewqa serve` HOME probe) so neither needs a
// GitHub token / the gh PR-open path.
func writeRenderedLocal(workDir string, rs []gen.Rendered) error {
	if workDir == "" {
		workDir = "."
	}
	written := 0
	for _, r := range rs {
		dest := filepath.Join(workDir, r.Path)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(dest), err)
		}
		if err := os.WriteFile(dest, r.Content, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dest, err)
		}
		written++
	}
	rlog.Info("probe: wrote local files", "count", written, "workdir", workDir)
	return nil
}

func printEdits(es []heal.Edit) {
	for _, e := range es {
		fmt.Printf("%s:%d\n  - %s\n  + %s\n  reason: %s\n\n", e.File, e.Line, e.Before, e.After, e.Reason)
	}
}

func applyEdits(workDir string, indexed map[string]string, edits []heal.Edit) map[string][]byte {
	out := map[string][]byte{}
	bucket := map[string][]heal.Edit{}
	for _, e := range edits {
		bucket[e.File] = append(bucket[e.File], e)
	}
	for path, es := range bucket {
		content, ok := indexed[path]
		if !ok {
			b, err := os.ReadFile(filepath.Join(workDir, path))
			if err != nil {
				continue
			}
			content = string(b)
		}
		lines := strings.Split(content, "\n")
		for _, e := range es {
			if e.Line-1 < 0 || e.Line-1 >= len(lines) {
				continue
			}
			lines[e.Line-1] = e.After
		}
		out[path] = []byte(strings.Join(lines, "\n"))
	}
	return out
}

func genPRBody(pr *prSummary, rs []gen.Rendered) string {
	var b strings.Builder
	if pr != nil {
		fmt.Fprintf(&b, "Generated by reviewqa for #%d.\n\n", pr.Number)
	}
	b.WriteString("Files:\n")
	for _, r := range rs {
		fmt.Fprintf(&b, "- `%s` — covers `%s` (%s)\n", r.Path, r.Symbol.Name, r.Symbol.Language)
	}
	b.WriteString("\nEach scaffold contains one or more deterministic happy-path scenarios per component or symbol. Review and extend with edge cases.\n")
	appendQualityNotes(&b, rs)
	return b.String()
}

// appendQualityNotes summarises weak/missing locators across the rendered
// specs into a `## Quality notes` section. Surfaces what the customer should
// improve on their app to get more stable tests. Emits nothing when every
// spec has zero weak locators.
func appendQualityNotes(b *strings.Builder, rs []gen.Rendered) {
	type entry struct {
		path string
		note string
	}
	var all []entry
	for _, r := range rs {
		for _, n := range r.QualityNotes {
			all = append(all, entry{path: r.Path, note: n})
		}
	}
	if len(all) == 0 {
		return
	}
	b.WriteString("\n## Quality notes\n\n")
	b.WriteString("Weak / missing locators reviewqa fell back to. Add `data-testid` to these elements for stable tests:\n\n")
	for _, e := range all {
		fmt.Fprintf(b, "- `%s` — %s\n", e.path, e.note)
	}
}

func healPRBody(pr *prSummary, es []heal.Edit) string {
	var b strings.Builder
	if pr != nil {
		fmt.Fprintf(&b, "Generated by reviewqa for #%d.\n\n", pr.Number)
	}
	b.WriteString("Locator edits:\n")
	for _, e := range es {
		fmt.Fprintf(&b, "- `%s:%d` — %s\n", e.File, e.Line, e.Reason)
	}
	return b.String()
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	if sha == "" {
		return strconv.FormatInt(time.Now().Unix(), 10)
	}
	return sha
}

// writeStepSummary appends markdown to $GITHUB_STEP_SUMMARY when present.
// No-op outside GitHub Actions.
func writeStepSummary(md string) {
	p := os.Getenv("GITHUB_STEP_SUMMARY")
	if p == "" {
		return
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(md)
}

func generateSummary(pr *prSummary, rs []gen.Rendered, url string) string {
	var b strings.Builder
	b.WriteString("### reviewqa — generated tests\n\n")
	if pr != nil {
		fmt.Fprintf(&b, "Source PR: [#%d](%s)\n\n", pr.Number, pr.URL)
	}
	fmt.Fprintf(&b, "Follow-up PR: %s\n\n", url)
	b.WriteString("| Test file | Covers | Language |\n")
	b.WriteString("|---|---|---|\n")
	for _, r := range rs {
		fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", r.Path, r.Symbol.Name, r.Symbol.Language)
	}
	b.WriteString("\n")
	return b.String()
}

func healSummary(pr *prSummary, es []heal.Edit, url string) string {
	var b strings.Builder
	b.WriteString("### reviewqa — locator heal\n\n")
	if pr != nil {
		fmt.Fprintf(&b, "Source PR: [#%d](%s)\n\n", pr.Number, pr.URL)
	}
	fmt.Fprintf(&b, "Heal PR: %s\n\n", url)
	b.WriteString("| File | Line | Reason |\n")
	b.WriteString("|---|---|---|\n")
	for _, e := range es {
		fmt.Fprintf(&b, "| `%s` | %d | %s |\n", e.File, e.Line, e.Reason)
	}
	b.WriteString("\n")
	return b.String()
}

// readPRFromEvent extracts the PR number from $GITHUB_EVENT_PATH if present.
func readPRFromEvent() int {
	p := os.Getenv("GITHUB_EVENT_PATH")
	if p == "" {
		return 0
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	var event struct {
		PullRequest struct {
			Number int `json:"number"`
		} `json:"pull_request"`
		Number int `json:"number"`
	}
	if err := json.Unmarshal(b, &event); err != nil {
		return 0
	}
	if event.PullRequest.Number != 0 {
		return event.PullRequest.Number
	}
	return event.Number
}
