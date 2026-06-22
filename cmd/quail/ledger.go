package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/spriteCloud/quail-review/internal/ledger"
	rlog "github.com/spriteCloud/quail-review/internal/log"
)

// newLedgerCmd registers the `quail ledger ...` subcommand group.
// Today: `ledger update --report <path>` merges fresh findings into
// tests/e2e/docs/findings.md.
func newLedgerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ledger",
		Short: "Maintain the bug-discovery ledger (tests/e2e/docs/findings.md).",
	}
	cmd.AddCommand(newLedgerUpdateCmd(), newLedgerVerifyCmd())
	return cmd
}

// newLedgerVerifyCmd registers `quail ledger verify`. Reads a Playwright
// JSON report + the on-disk findings.md, surfaces the failures NOT
// already tracked as open in the ledger, and exits non-zero when any
// such regression exists.
//
// Wired into demo CI as a smoke-step post-processor: the suite stays
// red on regressions while accepting known SUT debt as tracked
// findings. "Fix don't hide" — the assertion still runs and still
// detects the violation; the green/red signal flips on regression
// vs. baseline.
//
// v0.97.2.
func newLedgerVerifyCmd() *cobra.Command {
	var report string
	var baseline string
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Exit non-zero when the Playwright report contains failures NOT tracked as open in findings.md.",
		Long: `Reads a Playwright JSON report and the findings.md ledger, computes
the regression set (failures whose (spec, test) key is NOT present
as an open ledger row), and exits non-zero when that set is
non-empty. Prints a compact report of new vs. baseline counts.

Use as the smoke gate in CI:

    npx playwright test --reporter=json || true   # capture report
    quail ledger verify --report=playwright-report.json   # red on regressions only

Baseline rows marked "resolved" are excluded — if they reoccur the
verifier surfaces them as regressions (the user explicitly closed
the finding, so reappearance is a real bug).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLedgerVerify(report, baseline)
		},
	}
	cmd.Flags().StringVar(&report, "report", "playwright-report.json", "Path to the Playwright JSON report")
	cmd.Flags().StringVar(&baseline, "baseline", "tests/e2e/docs/findings.md", "Path to the on-disk findings ledger")
	return cmd
}

func runLedgerVerify(reportPath, baselinePath string) error {
	r, err := ledger.LoadReport(reportPath)
	if err != nil {
		return fmt.Errorf("ledger verify: load report: %w", err)
	}
	if r == nil {
		rlog.Info("ledger verify: report not found; nothing to verify", "report", reportPath)
		return nil
	}
	today := time.Now().UTC().Format("2006-01-02")
	current := ledger.FindingsFromReport(r, today)
	existing, err := os.ReadFile(baselinePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ledger verify: read baseline: %w", err)
	}
	baseline := ledger.ParseLedger(existing)
	regressions := ledger.NewFindings(current, baseline)
	rlog.Info("ledger verify: summary",
		"current_failures", len(current),
		"baseline_open", countOpen(baseline),
		"regressions", len(regressions),
	)
	if len(regressions) == 0 {
		rlog.Info("ledger verify: PASS — all failures are tracked as open in findings.md")
		return nil
	}
	for _, f := range regressions {
		rlog.Warn("regression", "spec", f.Spec, "test", f.Test, "symptom", f.Symptom)
	}
	return fmt.Errorf("ledger verify: %d regression(s) not present as open in %s", len(regressions), baselinePath)
}

func countOpen(fs []ledger.Finding) int {
	n := 0
	for _, f := range fs {
		if f.Status == "" || f.Status == "open" {
			n++
		}
	}
	return n
}

func newLedgerUpdateCmd() *cobra.Command {
	var report string
	var out string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Merge fresh Playwright failures into the on-disk findings.md ledger.",
		Long: `Reads a Playwright JSON report (` + "`--reporter=json`" + `), surfaces failing
and timed-out tests, and merges them into tests/e2e/docs/findings.md.

Rows are deduped by (spec, test). Reoccurring findings have their
LastSeen bumped; the FirstSeen stamp survives. Hand-edited "resolved"
rows are preserved across runs even when the failure repeats.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLedgerUpdate(report, out)
		},
	}
	cmd.Flags().StringVar(&report, "report", "playwright-report.json", "Path to the Playwright JSON report")
	cmd.Flags().StringVar(&out, "out", "tests/e2e/docs/findings.md", "Path to the ledger markdown file")
	return cmd
}

func runLedgerUpdate(reportPath, ledgerPath string) error {
	r, err := ledger.LoadReport(reportPath)
	if err != nil {
		return fmt.Errorf("ledger update: load report: %w", err)
	}
	today := time.Now().UTC().Format("2006-01-02")
	fresh := ledger.FindingsFromReport(r, today)
	existing, err := os.ReadFile(ledgerPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("ledger update: read existing ledger: %w", err)
	}
	merged := ledger.Merge(existing, fresh)
	if err := os.MkdirAll(filepath.Dir(ledgerPath), 0o755); err != nil {
		return fmt.Errorf("ledger update: mkdir: %w", err)
	}
	if err := os.WriteFile(ledgerPath, merged, 0o644); err != nil {
		return fmt.Errorf("ledger update: write: %w", err)
	}
	rlog.Info("ledger updated", "path", ledgerPath, "new_findings", len(fresh))
	return nil
}
