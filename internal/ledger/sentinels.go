package ledger

import (
	"strings"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/plan"
)

// EmitSentinels converts the "open" rows of a ledger into one
// test.fail() spec each, under tests/e2e/sentinels/. Each spec
// reproduces the recorded symptom and stays red until the bug is
// fixed — at which point Playwright flips it to "unexpected pass"
// and the consumer removes the sentinel.
//
// "resolved" findings are skipped; v0.30's ledger preserved
// resolved status across runs so consumers can hide a finding
// permanently without losing the row.
func EmitSentinels(findings []Finding) []plan.Item {
	var items []plan.Item
	for _, f := range findings {
		if strings.EqualFold(f.Status, "resolved") {
			continue
		}
		stem := sentinelStem(f)
		sym := ast.Symbol{
			Name:          sanitizeName(f.Test),
			Kind:          ast.KindFunction,
			File:          f.Spec,
			Language:      "ts",
			PageTitle:     f.Symptom,
			FrameworkHint: f.Severity,
		}
		items = append(items, plan.Item{
			Symbol:   sym,
			Symbols:  []ast.Symbol{sym},
			PageURL:  f.FirstSeen,
			Template: plan.TmplPlaywrightSentinel,
			OutPath:  "tests/e2e/sentinels/" + stem + ".sentinel.spec.ts",
		})
	}
	return items
}

// sentinelStem builds a filesystem-safe slug for a sentinel file
// derived from the spec + test title. Collapses non-alphanumeric
// characters to dashes.
func sentinelStem(f Finding) string {
	src := strings.ToLower(f.Spec + "-" + f.Test)
	var b strings.Builder
	prevDash := false
	for _, c := range src {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "sentinel"
	}
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

// sanitizeName strips characters that would break Gherkin / JS
// identifier expectations downstream.
func sanitizeName(s string) string {
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, `"`, "")
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
