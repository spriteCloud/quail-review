package probe

import "testing"

func TestParseCoverage(t *testing.T) {
	cases := map[string]CoverageMode{
		"":                  CoverageStandard,
		"standard":          CoverageStandard,
		"breadth":           CoverageBreadth,
		"depth":             CoverageDepth,
		"DEPTH":             CoverageDepth,
		"  breadth  ":       CoverageBreadth,
		"unknown-mode":      CoverageStandard, // defaults to standard
	}
	for raw, want := range cases {
		if got := ParseCoverage(raw); got != want {
			t.Errorf("ParseCoverage(%q) = %q; want %q", raw, got, want)
		}
	}
}

func TestCoverageMode_DialsTheKnobs(t *testing.T) {
	// Knobs must scale: breadth < standard < depth across all three dials.
	for _, k := range []struct {
		name string
		fn   func(CoverageMode) int
	}{
		{"MaxPages", func(c CoverageMode) int { return c.crawlOpts().MaxPages }},
		{"MaxDepth", func(c CoverageMode) int { return c.crawlOpts().MaxDepth }},
		{"JourneysPerKind", func(c CoverageMode) int { return c.JourneysPerKind() }},
		{"FuzzCap", func(c CoverageMode) int { return c.FuzzCap() }},
	} {
		b, s, d := k.fn(CoverageBreadth), k.fn(CoverageStandard), k.fn(CoverageDepth)
		if !(b < s && s < d) {
			t.Errorf("%s should scale breadth(%d) < standard(%d) < depth(%d)", k.name, b, s, d)
		}
	}
}
