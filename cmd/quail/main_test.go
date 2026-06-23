package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-core/config"
	"github.com/spriteCloud/quail-review/internal/gen"
	"github.com/spriteCloud/quail-review/internal/heal"
	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestProbeBranchName_HostSlugForSingleURL(t *testing.T) {
	cfg := config.Config{BranchPrefix: "quail"}
	tests := []struct {
		urls []string
		want string
	}{
		{[]string{"https://www.spritecloud.com"}, "quail/probe-spritecloud-com"},
		{[]string{"https://es.wikipedia.org/wiki/Madrid"}, "quail/probe-es-wikipedia-org"},
		{[]string{"http://localhost:18181/page"}, "quail/probe-localhost:18181"},
	}
	for _, tc := range tests {
		got := probeBranchName(cfg, tc.urls)
		if got != tc.want {
			t.Errorf("probeBranchName(%v) = %q; want %q", tc.urls, got, tc.want)
		}
	}
}

func TestProbeBranchName_TimestampFallbackForMultiOrMissing(t *testing.T) {
	cfg := config.Config{BranchPrefix: "quail"}
	// Zero URLs → timestamp fallback
	got := probeBranchName(cfg, nil)
	if !strings.HasPrefix(got, "quail/probe-") {
		t.Errorf("expected quail/probe- prefix; got %q", got)
	}
	if strings.Contains(got, "-com") || strings.Contains(got, "-org") {
		t.Errorf("expected timestamp form, not host-slug; got %q", got)
	}
	// Multiple URLs → timestamp fallback (we don't pick one over the other)
	got = probeBranchName(cfg, []string{"https://a.test", "https://b.test"})
	if strings.Contains(got, "a-test") || strings.Contains(got, "b-test") {
		t.Errorf("expected timestamp fallback for multi-URL; got %q", got)
	}
}

func TestShortSHA(t *testing.T) {
	if got := shortSHA("abcdef1234567890"); got != "abcdef1" {
		t.Errorf("long: %q", got)
	}
	if got := shortSHA("abc"); got != "abc" {
		t.Errorf("short: %q", got)
	}
	if got := shortSHA(""); got == "" {
		t.Error("empty should produce fallback")
	}
}

func TestReadPRFromEvent(t *testing.T) {
	dir := t.TempDir()

	// pull_request shape
	p := filepath.Join(dir, "pr.json")
	if err := os.WriteFile(p, []byte(`{"pull_request":{"number":42},"number":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_EVENT_PATH", p)
	if got := readPRFromEvent(); got != 42 {
		t.Errorf("pull_request: %d", got)
	}

	// fallback to top-level number
	p2 := filepath.Join(dir, "issue.json")
	if err := os.WriteFile(p2, []byte(`{"number":7}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_EVENT_PATH", p2)
	if got := readPRFromEvent(); got != 7 {
		t.Errorf("top-level: %d", got)
	}

	// missing env var → 0
	t.Setenv("GITHUB_EVENT_PATH", "")
	if got := readPRFromEvent(); got != 0 {
		t.Errorf("missing: %d", got)
	}
}

func TestApplyEditsInMemory(t *testing.T) {
	indexed := map[string]string{"x.spec.ts": "line1\nline2\nline3"}
	edits := []heal.Edit{
		{File: "x.spec.ts", Line: 2, Before: "line2", After: "REWRITTEN"},
	}
	out := applyEdits(".", indexed, edits)
	got, ok := out["x.spec.ts"]
	if !ok {
		t.Fatal("missing file in output")
	}
	if !strings.Contains(string(got), "REWRITTEN") {
		t.Errorf("not applied: %s", got)
	}
}

func TestApplyEditsOnDisk(t *testing.T) {
	dir := t.TempDir()
	src := "alpha\nbeta\ngamma"
	if err := os.WriteFile(filepath.Join(dir, "spec.ts"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	edits := []heal.Edit{
		{File: "spec.ts", Line: 2, Before: "beta", After: "BETA"},
	}
	out := applyEdits(dir, nil, edits)
	got := string(out["spec.ts"])
	if !strings.Contains(got, "BETA") || !strings.Contains(got, "alpha") {
		t.Errorf("disk edit not applied: %q", got)
	}
}

func TestApplyEditsOutOfRange(t *testing.T) {
	indexed := map[string]string{"x.spec.ts": "only-line"}
	edits := []heal.Edit{
		{File: "x.spec.ts", Line: 99, Before: "x", After: "y"},
	}
	out := applyEdits(".", indexed, edits)
	if !strings.Contains(string(out["x.spec.ts"]), "only-line") {
		t.Errorf("out-of-range edit shouldn't corrupt: %s", out["x.spec.ts"])
	}
}

func TestGenPRBody(t *testing.T) {
	// v0.99 — body is now a kind summary + collapsible full list.
	// One a11y spec + one perf spec + one scaffold file should
	// produce: header, three kind bullets (a11y, perf, scaffold)
	// each with a sample path, and a <details> block.
	pr := &prSummary{Number: 9}
	rs := []gen.Rendered{
		{Path: "tests/e2e/a11y/landing.a11y.spec.ts", Template: plan.TmplPlaywrightA11y, Symbol: ast.Symbol{Name: "landing"}},
		{Path: "tests/e2e/perf/landing.perf.spec.ts", Template: plan.TmplPlaywrightPerf, Symbol: ast.Symbol{Name: "landing"}},
		{Path: "playwright.config.ts", Template: plan.TmplPlaywrightConfig, Symbol: ast.Symbol{Name: "spritecloud"}},
	}
	got := genPRBody(pr, rs)
	for _, want := range []string{
		"#9",
		"**3 files**",
		"**a11y** — 1 file",
		"**perf** — 1 file",
		"**scaffold** — 1 file",
		"tests/e2e/a11y/landing.a11y.spec.ts",
		"<details>",
		"Full file list (3 files)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestHealPRBody(t *testing.T) {
	pr := &prSummary{Number: 12}
	es := []heal.Edit{{File: "x.spec.ts", Line: 5, Reason: "anchor moved"}}
	got := healPRBody(pr, es)
	for _, want := range []string{"#12", "x.spec.ts:5", "anchor moved"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestSiblingPath(t *testing.T) {
	cases := map[string]string{
		"internal/diff/diff_test.go":  "internal/diff/diff_quail_test.go",
		"src/foo.test.ts":             "src/foo.quail.test.ts",
		"src/foo.test.js":             "src/foo.quail.test.js",
		"tests/e2e/foo.spec.ts":       "tests/e2e/foo.quail.spec.ts",
		"tests/test_users.py":         "tests/test_users_quail.py",
		"src/test/java/x/YTest.java":  "src/test/java/x/YQuailTest.java",
		"some/other.txt":              "some/other_quail.txt",
	}
	for in, want := range cases {
		if got := siblingPath(in); got != want {
			t.Errorf("siblingPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWriteStepSummary(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "summary.md")
	if err := os.WriteFile(p, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_STEP_SUMMARY", p)
	writeStepSummary("hello\n")
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Errorf("got %q", got)
	}
	// no env → no-op (no panic)
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	writeStepSummary("ignored")
}
