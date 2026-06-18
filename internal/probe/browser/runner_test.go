package browser

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunnerDir_RespectsXDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/custom/cache")
	got := RunnerDir()
	want := "/custom/cache/reviewqa/playwright-runner"
	if got != want {
		t.Errorf("RunnerDir = %q, want %q", got, want)
	}
}

func TestRunnerDir_DefaultsToUserCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir on this platform")
	}
	got := RunnerDir()
	want := filepath.Join(home, ".cache", "reviewqa", "playwright-runner")
	if got != want {
		t.Errorf("RunnerDir = %q, want %q", got, want)
	}
}

// EnsureRunner must be a no-op when the deps sentinel + the
// requested-engine sentinel exist and the package is on disk. We
// prove it by pointing XDG at a tempdir that we hand-populate,
// then running EnsureRunner with PATH cleared so any subprocess
// invocation would fail.
func TestEnsureRunner_NoopWhenReady(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	runner := filepath.Join(cache, "reviewqa", "playwright-runner")
	if err := os.MkdirAll(filepath.Join(runner, "node_modules", "@playwright", "test"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runner, "node_modules", "@playwright", "test", "package.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runner, nodeDepsSentinel), []byte("ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engineSentinelPath(runner, "chromium"), []byte("ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Cleared PATH guarantees any npm/npx exec call would explode.
	// If EnsureRunner is idempotent it never reaches that point.
	t.Setenv("PATH", "")

	got, err := EnsureRunner(context.Background(), "chromium")
	if err != nil {
		t.Fatalf("EnsureRunner on pre-populated runner: %v", err)
	}
	if got != runner {
		t.Errorf("EnsureRunner returned %q, want %q", got, runner)
	}
}

// When the sentinel is missing AND no npm is reachable, EnsureRunner
// must return ErrBrowserUnavailable (wrapped) so callers can
// distinguish "environment broken" from "browser ran but found
// nothing".
func TestEnsureRunner_ReturnsUnavailableWithoutNpm(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("PATH", "")

	_, err := EnsureRunner(context.Background(), "chromium")
	if err == nil {
		t.Fatal("expected error when npm is unreachable")
	}
	if !errorsIs(err, ErrBrowserUnavailable) {
		t.Errorf("expected wraps ErrBrowserUnavailable; got %v", err)
	}
}

// Bogus engine should bail early with ErrBrowserUnavailable, before
// any disk or subprocess action. Pins the cli-flag validation
// boundary so a typo doesn't reach npx.
func TestEnsureRunner_RejectsUnknownEngine(t *testing.T) {
	_, err := EnsureRunner(context.Background(), "internet-explorer")
	if err == nil {
		t.Fatal("expected error for unknown engine")
	}
	if !errorsIs(err, ErrBrowserUnavailable) {
		t.Errorf("expected wraps ErrBrowserUnavailable; got %v", err)
	}
}

// Per-engine sentinels: with deps already installed and the chromium
// sentinel present, requesting firefox must NOT skip the install
// (so the user gets firefox) — but with both sentinels present a
// firefox call is a no-op. The first half is implicitly tested by
// the absence of a chromium-only short-circuit; this test pins the
// no-op case for firefox specifically so a future regression doesn't
// merge the sentinels.
func TestEnsureRunner_PerEngineSentinel(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	runner := filepath.Join(cache, "reviewqa", "playwright-runner")
	if err := os.MkdirAll(filepath.Join(runner, "node_modules", "@playwright", "test"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runner, "node_modules", "@playwright", "test", "package.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runner, nodeDepsSentinel), []byte("ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engineSentinelPath(runner, "firefox"), []byte("ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", "")
	if _, err := EnsureRunner(context.Background(), "firefox"); err != nil {
		t.Fatalf("EnsureRunner(firefox) when firefox sentinel present: %v", err)
	}

	// Cross-check: chromium sentinel is NOT present — calling with
	// PATH still cleared MUST fail because npm/npx aren't reachable
	// for the engine install step.
	if _, err := EnsureRunner(context.Background(), "chromium"); err == nil {
		t.Errorf("EnsureRunner(chromium) succeeded with no chromium sentinel and no PATH — sentinels are not engine-scoped")
	}
}

// Tiny local wrapper so we don't have to import "errors" for one
// call. Mirrors errors.Is behavior on the wrapped sentinel.
func errorsIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		type unwrapper interface{ Unwrap() error }
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
			continue
		}
		return false
	}
	return false
}
