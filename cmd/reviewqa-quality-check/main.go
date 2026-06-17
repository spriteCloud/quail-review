// reviewqa-quality-check parses the `reviewqa probe --dry-run` output and
// emits a Markdown fitness report. Used by the verify-references CI
// workflow to compare reviewqa's behaviour across diverse public sites
// before LLM/DGX work is attempted.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

// reSpecHeader matches the dry-run delimiter that starts every spec.
//
//	--- tests/e2e/playwright-dev-explore-docs.spec.ts ---
var reSpecHeader = regexp.MustCompile(`^---\s+tests/e2e/([\w.-]+\.spec\.ts)\s+---$`)

// reJourneyKindFromFile pulls the journey kind from a spec filename. The
// stem shape is <host-slug>-<kind>[-<terminal-slug>]. The kind is the FIRST
// token after the host slug — host slugs themselves can contain dashes.
var knownKinds = []string{
	"convert", "contact", "evaluate", "research",
	"browse", "discover", "explore", "read", "exercise",
}

// kindsAllowedToFill names journeys whose purpose includes filling a form
// field. Form leakage is reported only when fill() appears outside these.
var kindsAllowedToFill = map[string]bool{
	"convert":  true,
	"contact":  true,
	"exercise": true, // search box, date input
}

// spec is what we collect per .spec.ts emitted by the probe.
type spec struct {
	file string
	kind string

	hasH1Assertion bool
	hasFillCall    bool
	// Chained steps (after the first navigation) and what we observed in
	// each. Used for "banner-only" and "empty-step" checks.
	chainedSteps []chainedStep
}

type chainedStep struct {
	bannerOnly bool
	empty      bool
}

func main() {
	site := flag.String("site", "", "site label printed at the top of the report")
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: reviewqa-quality-check [--site=<label>] <probe-output.log>")
		os.Exit(2)
	}

	f, err := os.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer f.Close()

	specs := parseSpecs(f)
	if _, err := io.WriteString(os.Stdout, renderReport(*site, specs)); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
}

// parseSpecs reads the probe dry-run output and groups it by spec file.
func parseSpecs(r io.Reader) []spec {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20) // 1 MiB lines, defensive

	var specs []spec
	var cur *spec
	var curStep *chainedStep
	chainedActive := false

	flushStep := func() {
		if curStep == nil {
			return
		}
		cur.chainedSteps = append(cur.chainedSteps, *curStep)
		curStep = nil
	}

	for scanner.Scan() {
		line := scanner.Text()

		if m := reSpecHeader.FindStringSubmatch(line); m != nil {
			if cur != nil {
				flushStep()
				specs = append(specs, *cur)
			}
			file := m[1]
			cur = &spec{file: file, kind: kindFromFile(file)}
			curStep = nil
			chainedActive = false
			continue
		}
		if cur == nil {
			continue
		}

		if strings.Contains(line, "getByRole('heading', { level: 1") {
			cur.hasH1Assertion = true
		}
		if strings.Contains(line, ".fill(") {
			cur.hasFillCall = true
		}

		// Step boundaries — chained steps are those marked "Step N — click ..."
		// where N >= 2. Step 1's "visit" comment isn't a chained step.
		if strings.Contains(line, "// Step ") {
			if strings.Contains(line, "click ") || strings.Contains(line, "outbound click ") {
				flushStep()
				curStep = &chainedStep{empty: true, bannerOnly: false}
				chainedActive = true
			} else {
				flushStep()
				chainedActive = false
			}
			continue
		}

		if chainedActive && curStep != nil {
			if strings.Contains(line, "expect(") {
				// Banner is the only assertion until we see a non-banner one.
				isBanner := strings.Contains(line, "getByRole('banner')")
				if curStep.empty {
					curStep.empty = false
					curStep.bannerOnly = isBanner
				} else if !isBanner {
					curStep.bannerOnly = false
				}
			}
		}
	}
	if cur != nil {
		flushStep()
		specs = append(specs, *cur)
	}
	return specs
}

// kindFromFile pulls the journey kind from the file name by scanning for
// any of the known-kinds tokens as a hyphen-delimited word.
func kindFromFile(file string) string {
	stem := strings.TrimSuffix(file, ".spec.ts")
	tokens := strings.Split(stem, "-")
	for _, t := range tokens {
		for _, k := range knownKinds {
			if t == k {
				return k
			}
		}
	}
	return "unknown"
}

// renderReport produces the GitHub-summary Markdown block.
func renderReport(site string, specs []spec) string {
	var sb strings.Builder

	if site == "" {
		site = "(unlabeled)"
	}
	fmt.Fprintf(&sb, "## site: %s\n\n", site)

	if len(specs) == 0 {
		sb.WriteString("_No specs emitted. The probe produced nothing parseable — likely the site is fully JS-rendered or the BFS found nothing same-origin._\n\n")
		return sb.String()
	}

	kindCounts := map[string]int{}
	formLeakage := 0
	h1Coverage := 0
	bannerOnly := 0
	emptySteps := 0
	for _, s := range specs {
		kindCounts[s.kind]++
		if s.hasFillCall && !kindsAllowedToFill[s.kind] {
			formLeakage++
		}
		if s.hasH1Assertion {
			h1Coverage++
		}
		for _, st := range s.chainedSteps {
			if st.empty {
				emptySteps++
			} else if st.bannerOnly {
				bannerOnly++
			}
		}
	}

	sb.WriteString("| Total specs | Top kinds | Form leakage | h1 coverage | Banner-only steps | Empty steps |\n")
	sb.WriteString("|---|---|---|---|---|---|\n")
	fmt.Fprintf(&sb, "| %d | %s | %d | %d/%d | %d | %d |\n\n",
		len(specs),
		topKinds(kindCounts),
		formLeakage,
		h1Coverage, len(specs),
		bannerOnly,
		emptySteps,
	)

	sb.WriteString("<details><summary>Per-spec breakdown</summary>\n\n")
	for _, s := range specs {
		bo, es := 0, 0
		for _, st := range s.chainedSteps {
			if st.empty {
				es++
			} else if st.bannerOnly {
				bo++
			}
		}
		fmt.Fprintf(&sb, "- `%s` — kind=%s, h1=%v, fill=%v, chained-steps=%d, banner-only=%d, empty=%d\n",
			s.file, s.kind, s.hasH1Assertion, s.hasFillCall, len(s.chainedSteps), bo, es)
	}
	sb.WriteString("\n</details>\n\n")
	return sb.String()
}

func topKinds(counts map[string]int) string {
	type kv struct {
		k string
		v int
	}
	var all []kv
	for k, v := range counts {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].v != all[j].v {
			return all[i].v > all[j].v
		}
		return all[i].k < all[j].k
	})
	parts := make([]string, 0, len(all))
	for _, x := range all {
		parts = append(parts, fmt.Sprintf("%s:%d", x.k, x.v))
	}
	return strings.Join(parts, " ")
}
