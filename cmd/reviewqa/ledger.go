package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/reviewqa/reviewqa/internal/ledger"
	rlog "github.com/reviewqa/reviewqa/internal/log"
)

// newLedgerCmd registers the `reviewqa ledger ...` subcommand group.
// Today: `ledger update --report <path>` merges fresh findings into
// tests/e2e/docs/findings.md.
func newLedgerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ledger",
		Short: "Maintain the bug-discovery ledger (tests/e2e/docs/findings.md).",
	}
	cmd.AddCommand(newLedgerUpdateCmd())
	return cmd
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
