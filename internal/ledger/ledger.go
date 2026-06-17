// Package ledger maintains a markdown-based bug ledger across probe
// runs. Each fuzz / negative / journey failure surfaced by Playwright
// becomes a row in tests/e2e/docs/findings.md, deduped by (spec, test
// title). The on-disk file is human-editable; ledger.Merge round-trips
// it.
//
// Severity is derived from the spec filename — convert/contact/auth
// specs are `high`, fuzz/exercise/standard journeys are `medium`,
// explore/read are `low`. Mirrors the @priority:<level> mapping at
// the gen layer so a stakeholder reading the ledger sees consistent
// weighting.
package ledger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LedgerHeader is the literal first lines of findings.md. Used to
// recognise an existing ledger and to seed an empty one.
const LedgerHeader = `# Bug discovery ledger

Persists fuzz/negative/journey failures across runs. Each row is one
finding, deduped by (spec, test). Update with:

` + "```bash" + `
reviewqa ledger update --report playwright-report.json
` + "```" + `

Severity follows the @priority mapping: critical journeys → high,
standard → medium, nice-to-have → low.

| Spec | Test | Symptom | First seen | Last seen | Severity | Status |
|---|---|---|---|---|---|---|
`

// Finding is one row of the ledger. Stable across runs.
type Finding struct {
	Spec      string // tests/e2e/.../x.spec.ts
	Test      string // test('@journey:...') name verbatim
	Symptom   string // first line of the Playwright error message
	FirstSeen string // YYYY-MM-DD
	LastSeen  string // YYYY-MM-DD
	Severity  string // high | medium | low
	Status    string // open | resolved
}

func (f Finding) key() string {
	return f.Spec + "\x00" + f.Test
}

// Report is the subset of Playwright's JSON report shape we care about.
// Defined permissively so a partial report still parses.
type Report struct {
	Suites []Suite `json:"suites"`
}

type Suite struct {
	Title  string  `json:"title"`
	File   string  `json:"file"`
	Specs  []Spec  `json:"specs"`
	Suites []Suite `json:"suites"`
}

type Spec struct {
	Title string     `json:"title"`
	File  string     `json:"file"`
	Tests []TestCase `json:"tests"`
}

type TestCase struct {
	Title   string       `json:"title"`
	Results []TestResult `json:"results"`
}

type TestResult struct {
	Status   string `json:"status"`
	Error    *Error `json:"error,omitempty"`
	ErrorMsg string `json:"errorMessage,omitempty"`
}

type Error struct {
	Message string `json:"message"`
	Stack   string `json:"stack"`
}

// LoadReport reads + parses a Playwright JSON report. Returns (nil, nil)
// when the path doesn't exist — callers treat that as "no findings to
// merge", not an error.
func LoadReport(path string) (*Report, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var r Report
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("parse report %s: %w", path, err)
	}
	return &r, nil
}

// FindingsFromReport walks the report recursively, surfaces only failing
// tests, and returns one Finding per (spec, test). Severity is derived
// from the spec filename via SeverityForSpec; FirstSeen/LastSeen both
// set to `today`.
func FindingsFromReport(r *Report, today string) []Finding {
	var out []Finding
	if r == nil {
		return out
	}
	for _, s := range r.Suites {
		out = append(out, findingsFromSuite(s, "", today)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Spec != out[j].Spec {
			return out[i].Spec < out[j].Spec
		}
		return out[i].Test < out[j].Test
	})
	return out
}

func findingsFromSuite(s Suite, inheritedFile, today string) []Finding {
	var out []Finding
	file := s.File
	if file == "" {
		file = inheritedFile
	}
	for _, spec := range s.Specs {
		specFile := spec.File
		if specFile == "" {
			specFile = file
		}
		for _, tc := range spec.Tests {
			for _, r := range tc.Results {
				if !strings.EqualFold(r.Status, "failed") &&
					!strings.EqualFold(r.Status, "timedOut") &&
					!strings.EqualFold(r.Status, "interrupted") {
					continue
				}
				symptom := symptomFrom(r)
				if symptom == "" {
					symptom = "(no error message recorded)"
				}
				title := tc.Title
				if title == "" {
					title = spec.Title
				}
				out = append(out, Finding{
					Spec:      specFile,
					Test:      title,
					Symptom:   symptom,
					FirstSeen: today,
					LastSeen:  today,
					Severity:  SeverityForSpec(specFile),
					Status:    "open",
				})
			}
		}
	}
	for _, child := range s.Suites {
		out = append(out, findingsFromSuite(child, file, today)...)
	}
	return out
}

// symptomFrom extracts a single-line symptom from a Playwright result.
// Prefers `error.message`, falls back to `errorMessage`. ANSI escapes
// and surrounding whitespace are stripped so the ledger row stays
// readable in a plain-text viewer.
func symptomFrom(r TestResult) string {
	raw := ""
	if r.Error != nil && r.Error.Message != "" {
		raw = r.Error.Message
	} else if r.ErrorMsg != "" {
		raw = r.ErrorMsg
	}
	if raw == "" {
		return ""
	}
	raw = stripANSI(raw)
	if i := strings.IndexByte(raw, '\n'); i != -1 {
		raw = raw[:i]
	}
	raw = strings.TrimSpace(raw)
	// Markdown table cells choke on raw pipes — escape them.
	raw = strings.ReplaceAll(raw, "|", `\|`)
	if len(raw) > 220 {
		raw = raw[:217] + "..."
	}
	return raw
}

func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		if r == 0x1b {
			in = true
			continue
		}
		if in {
			if r == 'm' || r == 'K' || r == 'J' || r == 'H' {
				in = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// SeverityForSpec maps a spec filename to a ledger severity. Mirrors
// the @priority:<level> mapping used by the generator templates.
func SeverityForSpec(specFile string) string {
	base := strings.ToLower(filepath.Base(specFile))
	switch {
	case strings.Contains(base, "-convert.") ||
		strings.Contains(base, "-contact") ||
		strings.Contains(base, "-authenticate"):
		return "high"
	case strings.Contains(base, "-explore") || strings.Contains(base, "-read"):
		return "low"
	}
	return "medium"
}

// Merge folds fresh findings into the on-disk ledger. Returns the
// merged ledger as a byte slice ready to write to disk. Existing rows
// are matched by (Spec, Test); when matched, LastSeen is updated.
// Findings not present in the incoming batch are kept with their prior
// LastSeen (and Status preserved — a hand-edited "resolved" survives).
func Merge(existing []byte, fresh []Finding) []byte {
	prior := parseLedger(existing)
	priorByKey := map[string]Finding{}
	for _, p := range prior {
		priorByKey[p.key()] = p
	}
	for _, f := range fresh {
		if p, ok := priorByKey[f.key()]; ok {
			p.LastSeen = f.LastSeen
			// New runs override severity from the latest spec mapping;
			// the symptom is refreshed when the message changes.
			p.Severity = f.Severity
			p.Symptom = f.Symptom
			if p.Status == "" {
				p.Status = "open"
			}
			priorByKey[p.key()] = p
			continue
		}
		priorByKey[f.key()] = f
	}
	rows := make([]Finding, 0, len(priorByKey))
	for _, f := range priorByKey {
		rows = append(rows, f)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Spec != rows[j].Spec {
			return rows[i].Spec < rows[j].Spec
		}
		return rows[i].Test < rows[j].Test
	})
	var b strings.Builder
	b.WriteString(LedgerHeader)
	for _, r := range rows {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s | %s |\n",
			r.Spec, r.Test, r.Symptom, r.FirstSeen, r.LastSeen, r.Severity, r.Status)
	}
	return []byte(b.String())
}

// parseLedger extracts table rows from an existing findings.md. Returns
// nil on a missing / empty ledger. Header lines are skipped; only
// data rows (` ` table rows with 7 cells) are kept.
func parseLedger(existing []byte) []Finding {
	if len(existing) == 0 {
		return nil
	}
	lines := strings.Split(string(existing), "\n")
	var out []Finding
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
			continue
		}
		// Header / separator rows.
		if strings.HasPrefix(trimmed, "|---") || strings.Contains(trimmed, "| Spec |") || strings.Contains(trimmed, "|---|") {
			continue
		}
		cells := splitMarkdownRow(trimmed)
		if len(cells) != 7 {
			continue
		}
		spec := strings.Trim(cells[0], " `")
		out = append(out, Finding{
			Spec:      spec,
			Test:      cells[1],
			Symptom:   cells[2],
			FirstSeen: cells[3],
			LastSeen:  cells[4],
			Severity:  cells[5],
			Status:    cells[6],
		})
	}
	return out
}

// splitMarkdownRow splits a `| a | b | c |` row into its trimmed cells.
// Treats `\|` as a literal pipe (escaped during emission).
func splitMarkdownRow(row string) []string {
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	// Replace escaped pipes with a sentinel before splitting.
	const sentinel = "\x00"
	row = strings.ReplaceAll(row, `\|`, sentinel)
	raw := strings.Split(row, "|")
	out := make([]string, 0, len(raw))
	for _, c := range raw {
		out = append(out, strings.ReplaceAll(strings.TrimSpace(c), sentinel, "|"))
	}
	return out
}
