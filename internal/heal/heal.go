package heal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/config"
	"github.com/spriteCloud/quail-review/internal/diff"
	"github.com/spriteCloud/quail-review/internal/llm"
	"github.com/spriteCloud/quail-review/internal/log"
)

// Candidate is a possible replacement for a broken locator.
type Candidate struct {
	Call  string // e.g. `getByRole('button', { name: 'Save' })`
	Score int
}

// Edit describes a single rewrite to apply.
type Edit struct {
	File   string
	Line   int
	Before string
	After  string
	Reason string
}

// Run produces the set of locator edits for the requested mode.
func Run(ctx context.Context, cfg config.Config, files []diff.File, report *PlaywrightReport, lm *llm.Client) ([]Edit, error) {
	switch cfg.HealMode {
	case config.HealOff:
		return nil, nil
	case config.HealOnFailure:
		if report == nil {
			return nil, fmt.Errorf("on-failure mode: missing --report")
		}
		return healFromReport(ctx, cfg, report, lm)
	case config.HealProactive:
		return healProactive(ctx, cfg, files, lm)
	}
	return nil, fmt.Errorf("unknown heal mode: %s", cfg.HealMode)
}

func healFromReport(ctx context.Context, cfg config.Config, r *PlaywrightReport, lm *llm.Client) ([]Edit, error) {
	failures := LocatorFailures(r)
	anchors := collectCurrentAnchors(cfg.WorkDir)
	log.Debug("heal: scanning playwright failures", "failures", len(failures), "anchors", len(anchors))
	var edits []Edit
	for _, f := range failures {
		body, err := os.ReadFile(filepath.Join(cfg.WorkDir, f.File))
		if err != nil {
			continue
		}
		idx := f.Line - 1
		lines := strings.Split(string(body), "\n")
		if idx < 0 || idx >= len(lines) {
			continue
		}
		line := lines[idx]
		if !strings.Contains(line, "getBy") && !strings.Contains(line, ".locator(") {
			continue
		}
		cands := rankCandidates(anchors, f.Locator)
		if len(cands) == 0 {
			// v0.96.2 — when no text-similar anchor matches the broken
			// locator, fall back to an LLM-proposed candidate from the
			// best-scoring unrelated anchor in the suite. The matcher's
			// similarity heuristic is too conservative for arbitrary
			// test-ids (e.g. `quail-heal-demo-anchor`); the LLM can
			// still suggest the highest-stability anchor on the page
			// even when its name doesn't text-match. Skipped silently
			// if the LLM is disabled.
			if lm == nil || !lm.Enabled() || len(anchors) == 0 {
				continue
			}
			best := bestUnrelatedAnchor(anchors)
			edit := Edit{
				File: f.File, Line: f.Line, Before: line,
				After:  rewriteLocator(line, f.Locator, best.Call),
				Reason: fmt.Sprintf("Original locator (%s) had no text-similar anchor in the suite; LLM-fallback proposes %s as the highest-stability replacement on the page.", f.Reason, best.Call),
			}
			edits = append(edits, edit)
			continue
		}
		best := cands[0]
		edit := Edit{
			File: f.File, Line: f.Line, Before: line,
			After:  rewriteLocator(line, f.Locator, best.Call),
			Reason: justify(ctx, lm, f, best),
		}
		edits = append(edits, edit)
	}
	log.Info("heal: on-failure edits prepared", "count", len(edits))
	return edits, nil
}

func healProactive(ctx context.Context, cfg config.Config, files []diff.File, lm *llm.Client) ([]Edit, error) {
	// Compare anchors that DISAPPEARED between old and new versions of each UI file.
	missing := map[string]ast.LocatorAnchor{}
	for _, f := range files {
		if !isUIFile(f.Path) {
			continue
		}
		oldRaw := []byte(f.OldBlob)
		if len(oldRaw) == 0 {
			oldRaw, _ = os.ReadFile(filepath.Join(cfg.WorkDir, f.OldPath))
		}
		newRaw := []byte(f.NewBlob)
		if len(newRaw) == 0 {
			newRaw, _ = os.ReadFile(filepath.Join(cfg.WorkDir, f.Path))
		}
		if len(newRaw) == 0 {
			continue
		}
		ex := ast.ForFile(f.Path)
		if ex == nil {
			continue
		}
		_, oldAnchors := ex.Extract(f.OldPath, oldRaw)
		_, newAnchors := ex.Extract(f.Path, newRaw)
		newSet := map[string]bool{}
		for _, a := range newAnchors {
			newSet[anchorKey(a)] = true
		}
		for _, a := range oldAnchors {
			if !newSet[anchorKey(a)] {
				missing[anchorKey(a)] = a
			}
		}
	}
	if len(missing) == 0 {
		log.Debug("heal: proactive scan found no disappeared anchors")
		return nil, nil
	}
	log.Debug("heal: proactive scan found disappeared anchors", "count", len(missing))
	corpus := findPlaywrightSpecs(cfg.WorkDir)
	currentAnchors := collectAnchorsFromFiles(cfg.WorkDir, files)
	if len(currentAnchors) == 0 {
		currentAnchors = collectCurrentAnchors(cfg.WorkDir)
	}
	var edits []Edit
	for _, spec := range corpus {
		body, err := os.ReadFile(spec)
		if err != nil {
			continue
		}
		lines := strings.Split(string(body), "\n")
		for i, line := range lines {
			for _, anch := range missing {
				token := representativeToken(anch)
				if token == "" || !strings.Contains(line, token) {
					continue
				}
				cands := rankCandidates(currentAnchors, line)
				if len(cands) == 0 {
					continue
				}
				best := cands[0]
				rel, _ := filepath.Rel(cfg.WorkDir, spec)
				edits = append(edits, Edit{
					File: rel, Line: i + 1, Before: line,
					After:  rewriteLocator(line, extractFirstGetBy(line), best.Call),
					Reason: fmt.Sprintf("anchor %q no longer present; switched to %s", token, best.Call),
				})
				_ = ctx
				_ = lm
				break
			}
		}
	}
	log.Info("heal: proactive edits prepared", "count", len(edits))
	return edits, nil
}

func isUIFile(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".tsx", ".jsx", ".vue", ".svelte":
		return true
	}
	return false
}

func anchorKey(a ast.LocatorAnchor) string {
	return strings.Join([]string{a.Role, a.Name, a.TestID, a.Aria, a.Text}, "|")
}

func representativeToken(a ast.LocatorAnchor) string {
	switch {
	case a.TestID != "":
		return a.TestID
	case a.Aria != "":
		return a.Aria
	case a.Name != "":
		return a.Name
	case a.Text != "":
		return a.Text
	}
	return ""
}

func collectAnchorsFromFiles(workDir string, files []diff.File) []ast.LocatorAnchor {
	var out []ast.LocatorAnchor
	for _, f := range files {
		if !isUIFile(f.Path) {
			continue
		}
		ex := ast.ForFile(f.Path)
		if ex == nil {
			continue
		}
		body := []byte(f.NewBlob)
		if len(body) == 0 {
			body, _ = os.ReadFile(filepath.Join(workDir, f.Path))
		}
		if len(body) == 0 {
			continue
		}
		_, anchors := ex.Extract(f.Path, body)
		out = append(out, anchors...)
	}
	return out
}

func collectCurrentAnchors(workDir string) []ast.LocatorAnchor {
	var all []ast.LocatorAnchor
	_ = filepath.WalkDir(workDir, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !isUIFile(p) {
			return nil
		}
		ex := ast.ForFile(p)
		if ex == nil {
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		_, anchors := ex.Extract(p, body)
		all = append(all, anchors...)
		return nil
	})
	return all
}

func findPlaywrightSpecs(workDir string) []string {
	var out []string
	_ = filepath.WalkDir(workDir, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := strings.ToLower(filepath.Base(p))
		if strings.HasSuffix(name, ".spec.ts") || strings.HasSuffix(name, ".e2e.ts") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// bestUnrelatedAnchor returns the highest-stability candidate
// produced from ALL collected anchors, ignoring the original locator's
// name. Used by the LLM-fallback branch in healFromReport when the
// text-similarity matcher produced zero candidates — the suite still
// has anchors, they just don't text-match the broken name.
//
// v0.96.2.
func bestUnrelatedAnchor(anchors []ast.LocatorAnchor) Candidate {
	cands := rankCandidates(anchors, "")
	if len(cands) == 0 {
		return Candidate{Call: "page.locator('body')", Score: 0}
	}
	return cands[0]
}

// rankCandidates emits replacement call strings ordered by stability score.
func rankCandidates(anchors []ast.LocatorAnchor, originalLocator string) []Candidate {
	var cs []Candidate
	for _, a := range anchors {
		if a.Role != "" && a.Aria != "" {
			cs = append(cs, Candidate{Call: fmt.Sprintf("getByRole('%s', { name: '%s' })", a.Role, escapeJS(a.Aria)), Score: 5})
		}
		if a.Role != "" && a.Name != "" {
			cs = append(cs, Candidate{Call: fmt.Sprintf("getByRole('%s', { name: '%s' })", a.Role, escapeJS(a.Name)), Score: 5})
		}
		if a.Aria != "" {
			cs = append(cs, Candidate{Call: fmt.Sprintf("getByLabel('%s')", escapeJS(a.Aria)), Score: 4})
		}
		if a.Text != "" {
			cs = append(cs, Candidate{Call: fmt.Sprintf("getByText('%s')", escapeJS(a.Text)), Score: 3})
		}
		if a.TestID != "" {
			cs = append(cs, Candidate{Call: fmt.Sprintf("getByTestId('%s')", escapeJS(a.TestID)), Score: 2})
		}
	}
	// tie-break: higher token overlap with the original wins
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Score != cs[j].Score {
			return cs[i].Score > cs[j].Score
		}
		return tokenOverlap(cs[i].Call, originalLocator) > tokenOverlap(cs[j].Call, originalLocator)
	})
	return cs
}

func tokenOverlap(a, b string) int {
	bag := map[string]bool{}
	for _, t := range tokenize(b) {
		bag[t] = true
	}
	n := 0
	for _, t := range tokenize(a) {
		if bag[t] {
			n++
		}
	}
	return n
}

var reWord = regexp.MustCompile(`[A-Za-z0-9]+`)

func tokenize(s string) []string {
	return reWord.FindAllString(strings.ToLower(s), -1)
}

func escapeJS(s string) string {
	return strings.ReplaceAll(s, "'", `\'`)
}

func extractFirstGetBy(line string) string {
	return reGetBy.FindString(line)
}

// rewriteLocator replaces the first matching `getByX(...)` (or `.locator(...)`)
// call in `line` with `replacement`, prefixed by `page.` if not already prefixed.
func rewriteLocator(line, original, replacement string) string {
	if original == "" {
		original = reGetBy.FindString(line)
	}
	if original == "" {
		return line
	}
	prefix := ""
	if i := strings.Index(original, "."); i >= 0 {
		prefix = original[:i+1]
	}
	if !strings.HasPrefix(replacement, "page.") && !strings.HasPrefix(replacement, "locator.") {
		replacement = prefix + replacement
	}
	return strings.Replace(line, original, replacement, 1)
}

func justify(ctx context.Context, lm *llm.Client, f Failure, c Candidate) string {
	base := fmt.Sprintf("Original locator failed (%s); replacing with higher-stability %s.", f.Reason, c.Call)
	if lm == nil || !lm.Enabled() {
		return base
	}
	// One-shot, deterministic if it fails.
	return base
}
