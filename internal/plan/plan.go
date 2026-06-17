// Package plan decides what to generate per discovered symbol.
package plan

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/diff"
)

type Template string

const (
	TmplJestUnit            Template = "jest_unit"
	TmplJestAPI             Template = "jest_api"
	TmplPlaywrightE2E       Template = "pw_e2e"
	TmplPlaywrightHappyFlow Template = "pw_happyflow"
	TmplPlaywrightFixtures  Template = "pw_fixtures"
	TmplPlaywrightConfig    Template = "pw_config"
	TmplPlaywrightReadme    Template = "pw_readme"
	TmplPlaywrightPackage   Template = "pw_package"
	TmplPlaywrightTsconfig  Template = "pw_tsconfig"
	TmplPlaywrightCIFile    Template = "pw_ci_workflow"
	TmplPlaywrightFuzz      Template = "pw_fuzz"
	TmplPlaywrightFeature   Template = "pw_feature"
	TmplPytestUnit          Template = "pytest_unit"
	TmplPytestAPI           Template = "pytest_api"
	TmplGoUnit              Template = "gotest_unit"
	TmplGoHTTPTest          Template = "gotest_httptest"
	TmplJUnit5Unit          Template = "junit5_unit"
	TmplJUnit5RestAssured   Template = "junit5_restassured"
)

type Item struct {
	Symbol   ast.Symbol
	Template Template
	OutPath  string
	// Symbols carries multiple symbols when an Item represents a
	// page-scoped happy-flow (TmplPlaywrightHappyFlow). For all other
	// templates, Symbols is empty and Symbol is authoritative.
	Symbols []ast.Symbol
	// PageURL is the relative URL the happy-flow visits, e.g. "/", "/home".
	PageURL string
	// JourneyKind names the user goal this spec exercises (convert,
	// browse, explore, read). Empty for non-journey items.
	JourneyKind string
}

type Layout struct {
	// JS/TS
	HasJestDir   bool
	HasTestsDir  bool
	HasUnderTest bool // *.test.ts siblings
	UsesVitest   bool
	// Python
	HasPyTestsDir bool
	// Java
	HasMavenLayout bool // src/test/java
	// Generic
	WorkDir string
}

func Detect(workDir string) Layout {
	l := Layout{WorkDir: workDir}
	if has(workDir, "__tests__") {
		l.HasJestDir = true
	}
	if has(workDir, "tests") {
		l.HasTestsDir = true
		l.HasPyTestsDir = true
	}
	if hasMatch(workDir, "src", ".test.") {
		l.HasUnderTest = true
	}
	if has(workDir, filepath.Join("src", "test", "java")) {
		l.HasMavenLayout = true
	}
	// crude pkg detection
	if b, err := os.ReadFile(filepath.Join(workDir, "package.json")); err == nil {
		s := string(b)
		if strings.Contains(s, `"vitest"`) {
			l.UsesVitest = true
		}
	}
	return l
}

func has(root, sub string) bool {
	_, err := os.Stat(filepath.Join(root, sub))
	return err == nil
}

func hasMatch(root, dir, contains string) bool {
	matches, _ := filepath.Glob(filepath.Join(root, dir, "*"+contains+"*"))
	if len(matches) > 0 {
		return true
	}
	matches, _ = filepath.Glob(filepath.Join(root, dir, "**", "*"+contains+"*"))
	return len(matches) > 0
}

func Build(files []diff.File, layout Layout) []Item {
	var items []Item
	for _, f := range files {
		if f.Status == "removed" {
			continue
		}
		ex := ast.ForFile(f.Path)
		if ex == nil {
			continue
		}
		content := readNew(layout.WorkDir, f.Path, f.NewBlob)
		if len(content) == 0 {
			continue
		}
		syms, _ := ex.Extract(f.Path, content)
		for _, s := range syms {
			if !diff.Intersects(f.Added, s.Line, s.EndLine) {
				continue
			}
			it := Item{Symbol: s}
			it.Template = pickTemplate(s, layout)
			it.OutPath = testPathFor(s, it.Template, layout)
			items = append(items, it)
		}
	}
	if os.Getenv("REVIEWQA_E2E_STYLE") != "per-component" {
		items = groupByPage(items, files, layout)
	}
	return items
}

func readNew(workDir, rel, fallback string) []byte {
	if fallback != "" {
		return []byte(fallback)
	}
	b, err := os.ReadFile(filepath.Join(workDir, rel))
	if err != nil {
		return nil
	}
	return b
}

func pickTemplate(s ast.Symbol, l Layout) Template {
	switch s.Language {
	case "ts":
		switch s.Kind {
		case ast.KindRoute:
			return TmplJestAPI
		case ast.KindComponent:
			return TmplPlaywrightE2E
		default:
			return TmplJestUnit
		}
	case "python":
		if s.Kind == ast.KindRoute {
			return TmplPytestAPI
		}
		return TmplPytestUnit
	case "go":
		if s.Kind == ast.KindRoute {
			return TmplGoHTTPTest
		}
		return TmplGoUnit
	case "java":
		if s.Kind == ast.KindRoute {
			return TmplJUnit5RestAssured
		}
		return TmplJUnit5Unit
	}
	return TmplJestUnit
}

func testPathFor(s ast.Symbol, t Template, l Layout) string {
	dir, base := filepath.Split(s.File)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	switch s.Language {
	case "ts":
		return testPathForTS(dir, stem, t, l)
	case "python":
		return filepath.Join("tests", "test_"+stem+".py")
	case "go":
		return filepath.Join(dir, stem+"_test.go")
	case "java":
		return testPathForJava(dir, stem, s.File, l)
	}
	return filepath.Join("tests", stem+".test")
}

func testPathForTS(dir, stem string, t Template, l Layout) string {
	if t == TmplPlaywrightE2E || t == TmplPlaywrightHappyFlow {
		return filepath.Join("tests", "e2e", stem+".spec.ts")
	}
	if l.HasJestDir {
		return filepath.Join(dir, "__tests__", stem+".test.ts")
	}
	if l.HasUnderTest {
		return filepath.Join(dir, stem+".test.ts")
	}
	return filepath.Join("tests", stem+".test.ts")
}

func testPathForJava(dir, stem, file string, l Layout) string {
	if l.HasMavenLayout {
		rel := strings.TrimPrefix(file, "src/main/java/")
		return filepath.Join("src", "test", "java", strings.TrimSuffix(rel, ".java")+"Test.java")
	}
	return filepath.Join(dir, stem+"Test.java")
}
