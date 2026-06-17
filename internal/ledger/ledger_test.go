package ledger

import (
	"strings"
	"testing"
)

func TestFindingsFromReport_OnlyFailedTests(t *testing.T) {
	r := &Report{
		Suites: []Suite{{
			Title: "root",
			File:  "tests/e2e/x-contact.spec.ts",
			Specs: []Spec{{
				Title: "contact",
				Tests: []TestCase{
					{Title: "happy path", Results: []TestResult{{Status: "passed"}}},
					{Title: "submit blocked", Results: []TestResult{{Status: "failed", Error: &Error{Message: "expect(received).toBeLessThan(400)\nreceived: 500"}}}},
					{Title: "timeout case", Results: []TestResult{{Status: "timedOut", ErrorMsg: "test timeout 30000ms exceeded"}}},
				},
			}},
		}},
	}
	got := FindingsFromReport(r, "2026-06-17")
	if len(got) != 2 {
		t.Fatalf("expected 2 failed findings; got %d", len(got))
	}
	if got[0].FirstSeen != "2026-06-17" || got[0].LastSeen != "2026-06-17" {
		t.Errorf("FirstSeen/LastSeen should both be today; got %+v", got[0])
	}
	if got[0].Severity != "high" {
		t.Errorf("contact spec should be high severity; got %s", got[0].Severity)
	}
	if !strings.Contains(got[0].Symptom, "toBeLessThan") {
		t.Errorf("symptom missing message text: %q", got[0].Symptom)
	}
}

func TestSeverityForSpec(t *testing.T) {
	cases := map[string]string{
		"tests/e2e/x-convert.spec.ts":          "high",
		"tests/e2e/x-contact-foo.spec.ts":      "high",
		"tests/e2e/x-authenticate-login.spec.ts": "high",
		"tests/e2e/x-browse-blog.spec.ts":      "medium",
		"tests/e2e/x-fuzz.spec.ts":             "medium",
		"tests/e2e/x-exercise.spec.ts":         "medium",
		"tests/e2e/x-explore-docs.spec.ts":     "low",
		"tests/e2e/x-read-blog.spec.ts":        "low",
		"tests/e2e/api/x-api.api.spec.ts":      "medium",
	}
	for path, want := range cases {
		if got := SeverityForSpec(path); got != want {
			t.Errorf("SeverityForSpec(%q) = %q; want %q", path, got, want)
		}
	}
}

func TestMerge_IdempotentOnSecondRun(t *testing.T) {
	fresh := []Finding{
		{Spec: "tests/e2e/x-contact.spec.ts", Test: "submit blocked", Symptom: "5xx", FirstSeen: "2026-06-17", LastSeen: "2026-06-17", Severity: "high", Status: "open"},
	}
	first := Merge(nil, fresh)
	if !strings.Contains(string(first), "# Bug discovery ledger") {
		t.Errorf("merged ledger missing header")
	}
	if !strings.Contains(string(first), "submit blocked") {
		t.Errorf("merged ledger missing finding")
	}

	// Re-merge with the same fresh batch — row count must not double.
	second := Merge(first, fresh)
	rows := strings.Count(string(second), "tests/e2e/x-contact.spec.ts")
	if rows != 1 {
		t.Errorf("re-merge must be idempotent; got %d rows for the same finding", rows)
	}
}

func TestMerge_UpdatesLastSeenForReoccurringFinding(t *testing.T) {
	yesterday := []Finding{{Spec: "tests/e2e/x.spec.ts", Test: "t", Symptom: "boom", FirstSeen: "2026-06-16", LastSeen: "2026-06-16", Severity: "high", Status: "open"}}
	existing := Merge(nil, yesterday)
	today := []Finding{{Spec: "tests/e2e/x.spec.ts", Test: "t", Symptom: "boom (still)", FirstSeen: "2026-06-17", LastSeen: "2026-06-17", Severity: "high", Status: "open"}}
	merged := Merge(existing, today)
	// First-seen must NOT regress; last-seen must update.
	if !strings.Contains(string(merged), "2026-06-16 | 2026-06-17") {
		t.Errorf("expected FirstSeen=2026-06-16, LastSeen=2026-06-17; got:\n%s", string(merged))
	}
}

func TestMerge_PreservesHandResolvedStatus(t *testing.T) {
	// A finding marked "resolved" by a human shouldn't get flipped back
	// to "open" if it stops appearing in the report. We approximate this
	// by merging an existing resolved row with NO fresh findings.
	existing := Merge(nil, []Finding{{Spec: "tests/e2e/x.spec.ts", Test: "t", Symptom: "s", FirstSeen: "2026-06-15", LastSeen: "2026-06-15", Severity: "medium", Status: "resolved"}})
	merged := Merge(existing, nil)
	if !strings.Contains(string(merged), "| resolved |") {
		t.Errorf("resolved status not preserved across an empty merge:\n%s", string(merged))
	}
}

func TestLoadReport_MissingFileIsNotAnError(t *testing.T) {
	got, err := LoadReport("/tmp/definitely-does-not-exist.json")
	if err != nil {
		t.Errorf("missing report should not error; got %v", err)
	}
	if got != nil {
		t.Errorf("missing report should return nil")
	}
}
