package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/spriteCloud/quail-review/internal/ledger"
	rlog "github.com/spriteCloud/quail-core/log"
)

// newRunOnceCmd registers `quail run-once`. Closes the v0.43d gap:
// the sentinel layer (v0.40) ships templates but the ledger is empty
// until the suite has actually run. This subcommand runs the
// just-generated tests/e2e/ suite via Playwright, harvests failures
// into the ledger, and prepares the next `quail generate` to emit
// sentinel specs.
//
// Strictly local-only — meant for the operator's machine where the
// generated suite + Playwright are installed. Not wired into CI; CI
// can use `ledger update --report=...` after its own playwright run.
func newRunOnceCmd() *cobra.Command {
	var workDir string
	var record bool
	var reportPath string
	var grep string
	cmd := &cobra.Command{
		Use:   "run-once",
		Short: "Run the generated Playwright suite once and (optionally) record failures into the ledger.",
		Long: `Runs ` + "`npx playwright test --reporter=json`" + ` against the e2e suite
under --workdir/tests/e2e (default: ./tests/e2e). With --record, parses
the report and merges any failures into tests/e2e/docs/findings.md so
the next ` + "`quail generate`" + ` emits a sentinel spec per failing
test under tests/e2e/sentinels/.

Use this once after the first ` + "`quail probe`" + ` to bootstrap the
sentinel layer with real findings — without it, the ledger stays
empty and the sentinel template never gates on anything to emit.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOnce(workDir, record, reportPath, grep)
		},
	}
	cmd.Flags().StringVar(&workDir, "workdir", ".", "Project root containing tests/e2e/ (default cwd)")
	cmd.Flags().BoolVar(&record, "record", false, "Merge failures into tests/e2e/docs/findings.md after the run")
	cmd.Flags().StringVar(&reportPath, "report", "playwright-report.json", "Path to write the Playwright JSON report")
	cmd.Flags().StringVar(&grep, "grep", "", "Optional Playwright --grep pattern to scope the run")
	return cmd
}

func runOnce(workDir string, record bool, reportPath, grep string) error {
	e2eDir := filepath.Join(workDir, "tests", "e2e")
	if _, err := os.Stat(e2eDir); err != nil {
		return fmt.Errorf("run-once: no tests/e2e/ under %s — run `quail probe` first", workDir)
	}
	args := []string{"playwright", "test", "--reporter=json"}
	if grep != "" {
		args = append(args, "--grep", grep)
	}
	rlog.Info("run-once: starting Playwright", "workdir", workDir, "grep", grep)
	cmd := exec.Command("npx", args...)
	cmd.Dir = workDir
	cmd.Env = os.Environ()
	out, runErr := cmd.Output()
	// Playwright exits non-zero when tests fail — that's expected here.
	// We still want the JSON report on disk so the ledger can ingest it.
	if writeErr := os.WriteFile(reportPath, out, 0o644); writeErr != nil {
		return fmt.Errorf("run-once: write report: %w", writeErr)
	}
	rlog.Info("run-once: report written", "path", reportPath, "size_bytes", len(out))
	if !record {
		if runErr != nil {
			rlog.Info("run-once: tests reported failures (expected for sentinel discovery)")
		}
		return nil
	}
	return mergeReportIntoLedger(workDir, reportPath)
}

func mergeReportIntoLedger(workDir, reportPath string) error {
	report, err := ledger.LoadReport(reportPath)
	if err != nil {
		return fmt.Errorf("run-once: load report: %w", err)
	}
	today := time.Now().UTC().Format("2006-01-02")
	fresh := ledger.FindingsFromReport(report, today)
	ledgerPath := filepath.Join(workDir, "tests", "e2e", "docs", "findings.md")
	existing, err := os.ReadFile(ledgerPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("run-once: read ledger: %w", err)
	}
	merged := ledger.Merge(existing, fresh)
	if err := os.MkdirAll(filepath.Dir(ledgerPath), 0o755); err != nil {
		return fmt.Errorf("run-once: mkdir: %w", err)
	}
	if err := os.WriteFile(ledgerPath, merged, 0o644); err != nil {
		return fmt.Errorf("run-once: write ledger: %w", err)
	}
	rlog.Info("run-once: ledger updated", "path", ledgerPath, "new_findings", len(fresh))
	rlog.Info("run-once: re-run `quail generate` (or probe) to emit sentinels for each finding")
	return nil
}
