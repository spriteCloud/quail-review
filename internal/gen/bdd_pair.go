package gen

import "strings"

// BDDPair groups the .feature files in a render batch with the single
// step-definition file they bind to. The humanize layer treats the
// pair as one unit so a rewrite of step-text in any .feature can be
// matched by a corresponding rewrite of the pattern in the .steps.ts.
//
// FeatureIdx / StepsIdx hold indices into the original Rendered slice
// so the humanize layer can write results back without rebuilding it.
type BDDPair struct {
	FeatureIdx []int
	StepsIdx   int
}

// GroupBDDPair partitions `rs` into (pair, leftover) where the pair
// is "every tests/e2e/features/*.feature plus tests/e2e/steps/quail.steps.ts".
// If either half is missing (a probe that produced no BDD output, or
// the deterministic templates were skipped) the function returns
// nil and all entries fall through to leftover.
func GroupBDDPair(rs []Rendered) (*BDDPair, []int) {
	pair := &BDDPair{StepsIdx: -1}
	var leftover []int
	for i, r := range rs {
		switch {
		case strings.HasSuffix(r.Path, ".feature") && strings.Contains(r.Path, "tests/e2e/features/"):
			pair.FeatureIdx = append(pair.FeatureIdx, i)
		case strings.HasSuffix(r.Path, "quail.steps.ts") && strings.Contains(r.Path, "tests/e2e/steps/"):
			pair.StepsIdx = i
		default:
			leftover = append(leftover, i)
		}
	}
	if pair.StepsIdx < 0 || len(pair.FeatureIdx) == 0 {
		// Roll the partial pair back into leftover.
		all := make([]int, 0, len(rs))
		for i := range rs {
			all = append(all, i)
		}
		return nil, all
	}
	return pair, leftover
}
