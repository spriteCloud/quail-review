package main

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spriteCloud/quail-review/internal/config"
	"github.com/spriteCloud/quail-review/internal/gen"
	rlog "github.com/spriteCloud/quail-review/internal/log"
	"github.com/spriteCloud/quail-review/internal/probe"
	"github.com/spriteCloud/quail-review/internal/prompt"
)

// runPromptEvidence is the prompt-driven evidence-pack flow.
//
//  1. Probe the URL with the prompt filter applied.
//  2. Write the generated files into cfg.WorkDir (no PR; no GitHub call).
//  3. Run `npx playwright test --reporter=html,list` against the
//     newly-written specs.
//  4. ZIP the consumer's playwright-report/ + test-results/ into
//     tests/e2e/evidence-<timestamp>.zip and print the path.
//
// Honors a missing-`npx` / missing-Playwright environment by logging the
// reason and writing the specs without running the tests — the consumer
// can still inspect what was generated.
func runPromptEvidence(ctx context.Context, cfg config.Config, urls []string, filter prompt.Filter, coverage probe.CoverageMode) error {
	items, errs := probe.RunAllWithCoverage(ctx, urls, filter, coverage)
	for _, e := range errs {
		rlog.Warn("probe url failed", "err", e)
	}
	if len(items) == 0 {
		return fmt.Errorf("prompt --evidence: probe yielded no items (filter: %s)", filter.Describe())
	}
	items = applyKindFilter(items)
	rendered, err := gen.Render(items, cfg.WorkDir)
	if err != nil {
		return fmt.Errorf("prompt --evidence: render: %w", err)
	}
	specSlugs := writeRendered(cfg.WorkDir, rendered)
	if len(specSlugs) == 0 {
		return fmt.Errorf("prompt --evidence: no spec files produced — nothing to run")
	}
	rlog.Info("prompt --evidence: wrote spec files", "count", len(specSlugs))

	grep := strings.Join(specSlugs, "|")
	cmdName, cmdArgs := playwrightInvocation(grep)
	if cmdName == "" {
		rlog.Warn("prompt --evidence: skipping run — neither npx nor a hoisted playwright binary is on PATH; specs were written to disk")
		return nil
	}
	rlog.Info("prompt --evidence: running playwright", "cmd", cmdName, "args", cmdArgs)
	runCmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
	runCmd.Dir = cfg.WorkDir
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	// Pin the JSON reporter output so the ledger merge can find it.
	runCmd.Env = append(os.Environ(),
		"PLAYWRIGHT_JSON_OUTPUT_NAME=playwright-report.json",
	)
	if runErr := runCmd.Run(); runErr != nil {
		// Failed tests are EXPECTED for an evidence pack (the whole point
		// is to capture what happened, pass or fail). Log + carry on.
		rlog.Warn("prompt --evidence: playwright exited non-zero", "err", runErr)
	}

	// Update the bug-discovery ledger from the just-emitted JSON report
	// so the evidence ZIP includes findings.md with today's pass.
	reportJSON := filepath.Join(cfg.WorkDir, "playwright-report.json")
	ledgerPath := filepath.Join(cfg.WorkDir, "tests", "e2e", "docs", "findings.md")
	if updErr := runLedgerUpdate(reportJSON, ledgerPath); updErr != nil {
		rlog.Warn("prompt --evidence: ledger merge skipped", "err", updErr)
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	zipPath := filepath.Join(cfg.WorkDir, "tests", "e2e", "evidence-"+stamp+".zip")
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		return fmt.Errorf("prompt --evidence: mkdir for zip: %w", err)
	}
	if err := bundleEvidenceZip(zipPath, cfg.WorkDir); err != nil {
		return fmt.Errorf("prompt --evidence: bundle zip: %w", err)
	}
	rlog.Info("prompt --evidence: bundle ready", "path", zipPath)
	fmt.Println(zipPath)
	return nil
}

// writeRendered writes each Rendered file into workDir. Returns the
// list of test-name regexes (one per .spec.ts) suitable for joining
// into a --grep pattern.
func writeRendered(workDir string, rs []gen.Rendered) []string {
	var slugs []string
	for _, r := range rs {
		full := filepath.Join(workDir, r.Path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			rlog.Warn("prompt --evidence: mkdir", "path", filepath.Dir(full), "err", err)
			continue
		}
		if err := os.WriteFile(full, r.Content, 0o644); err != nil {
			rlog.Warn("prompt --evidence: write", "path", full, "err", err)
			continue
		}
		if strings.HasSuffix(r.Path, ".spec.ts") {
			base := strings.TrimSuffix(filepath.Base(r.Path), ".spec.ts")
			slugs = append(slugs, base)
		}
	}
	return slugs
}

// playwrightInvocation returns the command+args to drive playwright. Prefers
// a hoisted ./node_modules/.bin/playwright (no network), falls back to
// `npx playwright`. Returns ("", nil) when neither is available.
//
// The reporter list includes JSON because the bug-discovery ledger
// (runLedgerUpdate) reads it post-run. PLAYWRIGHT_JSON_OUTPUT_NAME is
// set by the caller so the JSON lands at a predictable path.
func playwrightInvocation(grep string) (string, []string) {
	args := []string{"test", "--reporter=html,list,json"}
	if grep != "" {
		args = append(args, "--grep", grep)
	}
	if path, err := exec.LookPath("./node_modules/.bin/playwright"); err == nil {
		return path, args
	}
	if path, err := exec.LookPath("npx"); err == nil {
		return path, append([]string{"playwright"}, args...)
	}
	return "", nil
}

// bundleEvidenceZip writes a zip containing the consumer's
// playwright-report/ and test-results/ directories. Missing directories
// are silently skipped (a never-ran scenario or a test pass with no
// artifacts is still a valid evidence bundle).
func bundleEvidenceZip(zipPath, workDir string) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()
	z := zip.NewWriter(f)
	defer z.Close()
	for _, sub := range []string{"playwright-report", "test-results"} {
		root := filepath.Join(workDir, sub)
		if _, err := os.Stat(root); err != nil {
			continue
		}
		walkErr := filepath.Walk(root, func(p string, info os.FileInfo, werr error) error {
			if werr != nil {
				return werr
			}
			if info.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(workDir, p)
			w, cerr := z.Create(filepath.ToSlash(rel))
			if cerr != nil {
				return cerr
			}
			src, oerr := os.Open(p)
			if oerr != nil {
				return oerr
			}
			defer src.Close()
			_, copyErr := io.Copy(w, src)
			return copyErr
		})
		if walkErr != nil {
			return walkErr
		}
	}
	return nil
}
