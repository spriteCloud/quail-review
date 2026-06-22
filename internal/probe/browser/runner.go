package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spriteCloud/quail-review/internal/log"
)

// ErrBrowserUnavailable is returned when the browser probe can't run
// because its environment isn't usable: node missing from PATH, the
// shared Playwright runner can't be installed (no network, no npm),
// or the install completed but produced no @playwright/test. Callers
// distinguish this from "browser ran but produced no pages" — the
// former bails under BrowserAlways, the latter falls back to static.
var ErrBrowserUnavailable = errors.New("browser probe: runner unavailable")

// nodeDepsSentinel marks that the shared @playwright/test +
// stealth dependencies are installed in node_modules. Separate from
// the per-engine binary sentinels because npm install runs once;
// `npx playwright install <engine>` runs per requested engine.
const nodeDepsSentinel = ".quail-node-deps-ready"

// supportedEngines is the set of engines EnsureRunner accepts. We
// validate the arg so a typo doesn't reach `npx playwright install`
// (which would download the wrong thing or fail with a long stderr).
var supportedEngines = map[string]struct{}{
	"chromium": {},
	"firefox":  {},
	"webkit":   {},
}

// RunnerDir returns the canonical Playwright runner cache root.
// Honours XDG_CACHE_HOME for users who relocate caches; otherwise
// uses ~/.cache/quail/playwright-runner.
func RunnerDir() string {
	if base := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); base != "" {
		return filepath.Join(base, "quail", "playwright-runner")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "quail-playwright-runner")
	}
	return filepath.Join(home, ".cache", "quail", "playwright-runner")
}

// ensureRunnerMu serialises concurrent EnsureRunner calls within the
// same process. The flock on disk serialises across processes; the
// in-process mutex saves the extra syscall round-trip and lets
// goroutines share an already-installed runner without each one
// taking the file lock.
var ensureRunnerMu sync.Mutex

// EnsureRunner guarantees the shared Playwright runner has the
// node-side deps installed AND the requested engine's binary on
// disk. Idempotent per (deps, engine).
//
// First-time install runs `npm install @playwright/test
// playwright-extra puppeteer-extra-plugin-stealth` then `npx
// playwright install <engine>` inside RunnerDir(). Output is
// streamed through log.Info so probe SSE viewers see progress.
// Cross-process safety: an flock on .install.lock serialises
// concurrent installs.
//
// engine must be one of chromium|firefox|webkit. The empty string
// defaults to chromium (back-compat with v0.88 callers).
//
// Returns ErrBrowserUnavailable wrapped with the underlying cause
// for any failure callers might want to surface to users.
func EnsureRunner(ctx context.Context, engine string) (string, error) {
	if engine == "" {
		engine = "chromium"
	}
	if _, ok := supportedEngines[engine]; !ok {
		return "", fmt.Errorf("%w: unsupported engine %q (want chromium|firefox|webkit)", ErrBrowserUnavailable, engine)
	}
	dir := RunnerDir()

	ensureRunnerMu.Lock()
	defer ensureRunnerMu.Unlock()

	depsReady := nodeDepsReady(dir)
	engineReady := engineSentinelExists(dir, engine)
	if depsReady && engineReady {
		return dir, nil
	}

	if _, err := exec.LookPath("npm"); err != nil {
		return dir, fmt.Errorf("%w: `npm` not found in PATH (install Node.js + npm)", ErrBrowserUnavailable)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return dir, fmt.Errorf("%w: create %s: %v", ErrBrowserUnavailable, dir, err)
	}

	lockPath := filepath.Join(dir, ".install.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return dir, fmt.Errorf("%w: open lock %s: %v", ErrBrowserUnavailable, lockPath, err)
	}
	defer lock.Close()
	if err := lockRunner(lock); err != nil {
		return dir, fmt.Errorf("%w: lock %s: %v", ErrBrowserUnavailable, lockPath, err)
	}
	defer unlockRunner(lock)

	// Re-check post-lock — another process may have finished while
	// we waited.
	depsReady = nodeDepsReady(dir)
	engineReady = engineSentinelExists(dir, engine)
	if depsReady && engineReady {
		return dir, nil
	}

	if !depsReady {
		log.Info("playwright runner: installing node deps once, ~30s (cached at "+dir+")", "dir", dir)
		pkgPath := filepath.Join(dir, "package.json")
		if _, err := os.Stat(pkgPath); err != nil {
			body := []byte(`{"name":"quail-playwright-runner","private":true,"version":"1.0.0"}` + "\n")
			if werr := os.WriteFile(pkgPath, body, 0o600); werr != nil {
				return dir, fmt.Errorf("%w: write %s: %v", ErrBrowserUnavailable, pkgPath, werr)
			}
		}
		// playwright-extra wraps the stock browsers with a pluggable
		// pipeline; puppeteer-extra-plugin-stealth is the de-facto JS-
		// layer evasion patch (navigator.webdriver, plugins, languages,
		// chrome runtime, …). Works fully on Chromium, partially on
		// Firefox, near-no-op on WebKit — which is fine, those engines
		// have native fingerprints WAFs don't recognise anyway.
		if err := runStream(ctx, dir, "npm", "install", "--no-audit", "--no-fund", "--silent",
			"@playwright/test", "playwright-extra", "puppeteer-extra-plugin-stealth"); err != nil {
			return dir, fmt.Errorf("%w: npm install: %v", ErrBrowserUnavailable, err)
		}
		if !pkgInstalled(dir) {
			return dir, fmt.Errorf("%w: @playwright/test missing after install in %s", ErrBrowserUnavailable, dir)
		}
		if err := os.WriteFile(filepath.Join(dir, nodeDepsSentinel), []byte("ok\n"), 0o600); err != nil {
			return dir, fmt.Errorf("%w: write deps sentinel: %v", ErrBrowserUnavailable, err)
		}
	}

	if !engineReady {
		log.Info("playwright runner: installing engine "+engine+" (~150MB)", "engine", engine, "dir", dir)
		if err := runStream(ctx, dir, "npx", "--yes", "playwright", "install", engine); err != nil {
			return dir, fmt.Errorf("%w: playwright install %s: %v", ErrBrowserUnavailable, engine, err)
		}
		if err := os.WriteFile(engineSentinelPath(dir, engine), []byte("ok\n"), 0o600); err != nil {
			return dir, fmt.Errorf("%w: write %s sentinel: %v", ErrBrowserUnavailable, engine, err)
		}
	}

	return dir, nil
}

func engineSentinelPath(dir, engine string) string {
	return filepath.Join(dir, ".quail-engine-"+engine+"-ready")
}

func engineSentinelExists(dir, engine string) bool {
	_, err := os.Stat(engineSentinelPath(dir, engine))
	return err == nil
}

// nodeDepsReady returns true when the shared @playwright/test +
// stealth deps are installed in node_modules. Separate from per-
// engine readiness so users who switch engines don't re-run npm.
func nodeDepsReady(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, nodeDepsSentinel)); err != nil {
		return false
	}
	return pkgInstalled(dir)
}

func pkgInstalled(dir string) bool {
	pkgJSON := filepath.Join(dir, "node_modules", "@playwright", "test", "package.json")
	_, err := os.Stat(pkgJSON)
	return err == nil
}

// runStream invokes name+args in dir and pipes combined output to
// log.Info line-by-line so SSE viewers see install progress. Returns
// a clean error on non-zero exit including a 4KB tail of output for
// diagnosis.
func runStream(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stderr = nil
	cmd.Stdout = nil
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
			log.Info("playwright runner: " + line)
		}
	}
	if err != nil {
		tail := string(out)
		if len(tail) > 4096 {
			tail = tail[len(tail)-4096:]
		}
		return fmt.Errorf("%s %s: %v (tail: %s)", name, strings.Join(args, " "), err, strings.TrimSpace(tail))
	}
	return nil
}
