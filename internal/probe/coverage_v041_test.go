package probe

import "testing"

func TestParseCoverage_Max(t *testing.T) {
	if got := ParseCoverage("max"); got != CoverageMax {
		t.Errorf("ParseCoverage(max) = %q; want CoverageMax", got)
	}
	if got := ParseCoverage("MAX"); got != CoverageMax {
		t.Errorf("ParseCoverage is case-insensitive")
	}
}

func TestCoverageMax_PushesLimitsHigherThanDepth(t *testing.T) {
	max := CoverageMax.crawlOpts()
	depth := CoverageDepth.crawlOpts()
	if max.MaxPages <= depth.MaxPages {
		t.Errorf("max MaxPages (%d) should exceed depth (%d)", max.MaxPages, depth.MaxPages)
	}
}

func TestCrawlOpts_RaisedDefaults(t *testing.T) {
	// v0.41b bumped standard 20→30 and depth 50→75.
	if got := CoverageStandard.crawlOpts().MaxPages; got < 30 {
		t.Errorf("standard MaxPages = %d; expected ≥30 after v0.41b bump", got)
	}
	if got := CoverageDepth.crawlOpts().MaxPages; got < 75 {
		t.Errorf("depth MaxPages = %d; expected ≥75 after v0.41b bump", got)
	}
}
