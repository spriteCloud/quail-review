package heal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/spriteCloud/quail-review/internal/ast/ts"
	"github.com/spriteCloud/quail-core/config"
	"github.com/spriteCloud/quail-core/diff"
)

func fakeDiff(path, oldBlob, newBlob string) []diff.File {
	return []diff.File{{OldPath: path, Path: path, Status: "modified", OldBlob: oldBlob, NewBlob: newBlob}}
}

func TestExtractLocator(t *testing.T) {
	cases := []struct{ in, want string }{
		{`Error: locator.click: Timeout 5000ms exceeded.\n\nCall log:\n  page.getByText('Save changes')`, `page.getByText('Save changes')`},
		{`page.getByRole("button", { name: "Save" }).click()`, `page.getByRole("button", { name: "Save" })`},
	}
	for _, c := range cases {
		got := extractLocator(c.in)
		if got != c.want {
			t.Errorf("extractLocator(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLoadReport(t *testing.T) {
	dir := t.TempDir()
	report := PlaywrightReport{
		Suites: []Suite{{
			File: "tests/login.spec.ts",
			Specs: []Spec{{Title: "logs in", File: "tests/login.spec.ts", Line: 4, Tests: []Test{{Results: []Result{{Status: "failed", Errors: []struct {
				Message string `json:"message"`
				Stack   string `json:"stack"`
				Snippet string `json:"snippet"`
			}{{Message: "Timeout: page.getByText('Sign in') not found"}}}}}}}},
		}},
	}
	raw, _ := json.Marshal(report)
	p := filepath.Join(dir, "report.json")
	os.WriteFile(p, raw, 0o644)
	r, err := LoadReport(p)
	if err != nil {
		t.Fatal(err)
	}
	fs := LocatorFailures(r)
	if len(fs) != 1 || fs[0].Locator != "page.getByText('Sign in')" {
		t.Errorf("failures: %+v", fs)
	}
	if fs[0].Reason != "timeout" {
		t.Errorf("reason: %s", fs[0].Reason)
	}
}

func TestProactiveHeal(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{WorkDir: dir, HealMode: config.HealProactive}
	oldTSX := `export const Card = () => <button aria-label="Save">Save</button>`
	newTSX := `export const Card = () => <button aria-label="Save changes">Save changes</button>`
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tests"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Card.tsx"), []byte(newTSX), 0o644)
	os.WriteFile(filepath.Join(dir, "tests", "card.spec.ts"),
		[]byte("import {test} from '@playwright/test'\ntest('saves', async ({page}) => { await page.getByLabel('Save').click() })\n"),
		0o644)

	edits, err := Run(context.Background(), cfg, fakeDiff("src/Card.tsx", oldTSX, newTSX), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) == 0 {
		t.Fatalf("expected at least one edit; got none")
	}
	if !strings.Contains(edits[0].After, "getByLabel('Save changes')") &&
		!strings.Contains(edits[0].After, "getByRole") {
		t.Errorf("unexpected replacement: %+v", edits[0])
	}
}
