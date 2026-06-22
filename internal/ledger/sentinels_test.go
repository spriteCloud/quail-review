package ledger

import (
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestEmitSentinels_OneItemPerOpenFinding(t *testing.T) {
	findings := []Finding{
		{Spec: "tests/e2e/x.spec.ts", Test: "submit blocked", Symptom: "5xx on empty", Severity: "high", Status: "open"},
		{Spec: "tests/e2e/y.spec.ts", Test: "email accepts whitespace", Symptom: "200 with bad data", Severity: "medium", Status: "open"},
		{Spec: "tests/e2e/z.spec.ts", Test: "old bug", Symptom: "fixed", Severity: "low", Status: "resolved"},
	}
	items := EmitSentinels(findings)
	if len(items) != 2 {
		t.Fatalf("expected 2 sentinels (resolved skipped); got %d", len(items))
	}
	for _, it := range items {
		if it.Template != plan.TmplPlaywrightSentinel {
			t.Errorf("template = %s", it.Template)
		}
		if !strings.HasPrefix(it.OutPath, "tests/e2e/sentinels/") {
			t.Errorf("OutPath = %s", it.OutPath)
		}
		if !strings.HasSuffix(it.OutPath, ".sentinel.spec.ts") {
			t.Errorf("OutPath suffix = %s", it.OutPath)
		}
	}
}

func TestSentinelStem_FilesystemSafe(t *testing.T) {
	cases := map[string]string{
		"x.spec.ts|submit blocked!":       "x-spec-ts-submit-blocked",
		"a/b/c.spec.ts|test with spaces":  "a-b-c-spec-ts-test-with-spaces",
	}
	for raw, want := range cases {
		parts := strings.SplitN(raw, "|", 2)
		got := sentinelStem(Finding{Spec: parts[0], Test: parts[1]})
		if got != want {
			t.Errorf("stem(%q) = %q; want %q", raw, got, want)
		}
	}
}

func TestEmitSentinels_EmptyInputReturnsNothing(t *testing.T) {
	if got := EmitSentinels(nil); got != nil {
		t.Errorf("nil input → nil output; got %+v", got)
	}
}
