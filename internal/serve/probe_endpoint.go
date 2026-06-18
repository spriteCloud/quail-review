package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/reviewqa/reviewqa/internal/probe"
)

// capture holds the most recent stdout lines from the most recent
// streamCommand invocation. The probe endpoint reads it post-run
// to derive item count / verdict reason from log markers without
// re-running the subprocess. Not concurrent-safe by design —
// streamCommand is gated by acquireRun's per-workdir lock and the
// probe endpoint is the only other reader.
var (
	captureMu    sync.Mutex
	captureLines []string
)

const captureMax = 200

// newProbeCmd builds the probe subprocess. NEVER uses
// exec.CommandContext — the probe must outlive the HTTP request
// context so an SSE client disconnect (curl --max-time, browser
// closed) does not SIGKILL it mid-pipeline. Tests swap this to
// redirect spawns at a stub.
var newProbeCmd = exec.Command

func resetCapture() {
	captureMu.Lock()
	captureLines = captureLines[:0]
	captureMu.Unlock()
}

func captureLine(line string) {
	captureMu.Lock()
	if len(captureLines) >= captureMax {
		captureLines = append(captureLines[1:], line)
	} else {
		captureLines = append(captureLines, line)
	}
	captureMu.Unlock()
}

func lastStdoutLines() []string {
	captureMu.Lock()
	out := make([]string, len(captureLines))
	copy(out, captureLines)
	captureMu.Unlock()
	return out
}

// ProbeRequest is the JSON body accepted by POST /api/probe.
//
// `URL` is required; everything else is optional and maps to a probe
// CLI flag. The serve UI surfaces a single input box and an "Advanced"
// disclosure for the other fields.
type ProbeRequest struct {
	URL         string `json:"url"`
	Coverage    string `json:"coverage,omitempty"`
	JourneyKind string `json:"journeyKind,omitempty"`
	LLM         string `json:"llm,omitempty"`
	// Browser picks how the CLI invokes the browser probe.
	// v0.86: auto / always / never. Empty defaults to auto.
	Browser string `json:"browser,omitempty"`
	// Engine picks which Playwright engine to launch. v0.89:
	// auto (cascade chromium→firefox→webkit) / chromium / firefox /
	// webkit. Empty defaults to auto.
	Engine string `json:"engine,omitempty"`
	// Stealth toggles playwright-extra + StealthPlugin for JS-layer
	// bot-detection evasion. v0.89: on (default) / off.
	Stealth string `json:"stealth,omitempty"`
}

// ProbeStream invokes the reviewqa binary's `probe` subcommand and
// streams its stdout/stderr as Server-Sent Events to the response
// writer. Returns the exit code so the handler can emit a final
// "done" event with the verdict.
//
// The probe binary writes into tests/e2e/<...> relative to its cwd,
// so we set cwd = parent-of-workdir when workdir ends in tests/e2e
// (the normal serve layout). For any other workdir we use the
// workdir itself — the user can still verify the output landed in
// the right place after the run.
func ProbeStream(ctx context.Context, w http.ResponseWriter, workdir string, req ProbeRequest) (int, error) {
	if strings.TrimSpace(req.URL) == "" {
		return -1, errors.New("url is required")
	}
	u, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return -1, errors.New("url must be a valid http(s) URL")
	}

	exe, err := os.Executable()
	if err != nil {
		if path, lerr := exec.LookPath("reviewqa"); lerr == nil {
			exe = path
		} else {
			return -1, fmt.Errorf("locate reviewqa binary: %w", err)
		}
	}

	cwd := pickProbeDestination(workdir, u)

	// --local: write rendered files into the workdir, do NOT try to
	// open a GitHub PR. The serve UI is a local control room, not a
	// CI runner; gh.New would fail without GITHUB_TOKEN.
	args := []string{"probe", "--url", u.String(), "--local"}
	if req.Coverage != "" {
		args = append(args, "--coverage", req.Coverage)
	}
	if req.JourneyKind != "" {
		// The probe CLI has no --journey-kind flag yet; preserve the
		// field for the future filter wiring. For now it lands in the
		// log line so the UI shows the user what they asked for.
		_ = req.JourneyKind
	}
	if req.LLM != "" {
		args = append(args, "--llm", req.LLM)
	}
	// v0.86: pass through the browser-probe mode. The serve UI's
	// user has Node + Playwright available so `auto` is the right
	// default — it tries static first, then falls back to Chromium
	// when the static client gets WAF-blocked.
	browserMode := req.Browser
	if browserMode == "" {
		browserMode = "auto"
	}
	args = append(args, "--browser", browserMode)
	if req.Engine != "" {
		args = append(args, "--engine", req.Engine)
	}
	if req.Stealth != "" {
		args = append(args, "--stealth", req.Stealth)
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		return -1, errors.New("response writer does not support flushing")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")

	writeEvent(w, flusher, "start", map[string]any{
		"workdir": cwd,
		"url":     u.String(),
		"command": exe + " " + strings.Join(args, " "),
		"at":      time.Now().UTC().Format(time.RFC3339),
	})

	// v0.87.2: detach the probe subprocess from the request context.
	// A client disconnect (browser closed, curl --max-time fired)
	// closes the SSE stream — with exec.CommandContext that cancel
	// SIGKILLs the probe mid-pipeline, dropping the .feature output
	// even though 167 static specs already landed on disk. We want
	// the probe to run to completion regardless; the next UI load
	// picks up the new project. streamCommand keeps draining stdout
	// (writes to a gone client silently fail at the kernel) so the
	// OS pipe buffer never blocks the subprocess.
	cmd := newProbeCmd(exe, args...)
	cmd.Dir = cwd
	cmd.Env = probeSubprocessEnv()
	exitCode, err := streamCommand(ctx, w, flusher, cmd)

	// Inspect the captured stdout lines to derive item count + a
	// short human reason for the verdict. The streamer tees into
	// captureRecentLines (set up in streamCommand); we read that
	// here via the package-scoped lastStdoutLines slice.
	itemCount, reason := parseProbeOutcome(lastStdoutLines())

	if err != nil {
		writeEvent(w, flusher, "done", map[string]any{
			"exitCode":  -1,
			"passed":    false,
			"itemCount": itemCount,
			"reason":    firstNonEmpty(err.Error(), reason),
			"error":     err.Error(),
			"at":        time.Now().UTC().Format(time.RFC3339),
		})
		return -1, err
	}

	passed := exitCode == 0 && itemCount != 0
	finalReason := reason
	if !passed && finalReason == "" {
		finalReason = "Probe finished with no items"
	}
	writeEvent(w, flusher, "done", map[string]any{
		"exitCode":  exitCode,
		"passed":    passed,
		"itemCount": itemCount,
		"reason":    finalReason,
		"at":        time.Now().UTC().Format(time.RFC3339),
	})
	return exitCode, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// parseProbeOutcome scans the streamed CLI stdout for the markers
// `probe: wrote local files` (success — followed by a count) and
// `probe: no items produced` (failure). Returns the inferred item
// count (-1 if unknown) and a short verdict line for the UI.
func parseProbeOutcome(lines []string) (int, string) {
	count := -1
	reason := ""
	for _, line := range lines {
		low := strings.ToLower(line)
		switch {
		case strings.Contains(low, "probe: wrote local files"):
			// log format: 'probe: wrote local files count=N workdir=...'
			if i := strings.Index(line, "count="); i >= 0 {
				rest := line[i+len("count="):]
				j := 0
				for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
					j++
				}
				if j > 0 {
					n := 0
					for k := 0; k < j; k++ {
						n = n*10 + int(rest[k]-'0')
					}
					count = n
				}
			}
		case strings.Contains(low, "probe: no items produced"):
			count = 0
			// Tail of the line is the parenthesised hint.
			if i := strings.Index(line, "("); i >= 0 {
				reason = strings.TrimSuffix(line[i+1:], ")")
			} else {
				reason = "Site unreachable or empty crawl"
			}
		case strings.Contains(low, "internal_error") && strings.Contains(low, "received from peer"):
			reason = "Site dropped the connection (likely a WAF). Try --browser=always for a real-browser crawl."
		}
	}
	return count, reason
}

// probeSubprocessEnv returns the env the probe subprocess should
// inherit, with the user's saved LLM settings translated into the
// env vars `cmd/reviewqa/main.go::newProbeCmd` expects:
//
//   - LLM.Endpoint   → REVIEWQA_LLM (applyLLMOverride uses this)
//   - LLM.Model      → REVIEWQA_MODEL
//   - LLM.APIKey     → OPENAI_API_KEY (or "ollama" sentinel when
//                      the endpoint is set but the key is blank,
//                      matching llmConfigFromEnv)
//   - LLM.TimeoutSec → REVIEWQA_LLM_TIMEOUT (Go duration string)
//
// LLM.Enabled=false zeroes OPENAI_API_KEY so the subprocess sees
// the LLM as disabled regardless of the host env. v0.77's
// llm_endpoint.go already did this for in-process callers; v0.87
// extends the policy to the spawned probe.
//
// Settings are appended AFTER os.Environ() so a value the user
// saved in Settings wins over whatever was inherited.
func probeSubprocessEnv() []string {
	env := append([]string(nil), os.Environ()...)
	s, err := LoadSettings()
	if err != nil {
		return env
	}
	apply := func(k, v string) {
		if v == "" {
			// Replace the inherited entry with an empty value so
			// the subprocess sees "disabled", not the host env.
			env = append(env, k+"=")
			return
		}
		env = append(env, k+"="+v)
	}
	if s.LLM.Enabled {
		if s.LLM.Endpoint != "" {
			apply("REVIEWQA_LLM", s.LLM.Endpoint)
		}
		if s.LLM.Model != "" {
			apply("REVIEWQA_MODEL", s.LLM.Model)
		}
		switch {
		case s.LLM.APIKey != "":
			apply("OPENAI_API_KEY", s.LLM.APIKey)
		case s.LLM.Endpoint != "":
			// Keyless local Ollama path: same sentinel
			// llmConfigFromEnv falls back to.
			apply("OPENAI_API_KEY", "ollama")
		}
		if s.LLM.TimeoutSec > 0 {
			apply("REVIEWQA_LLM_TIMEOUT", fmt.Sprintf("%ds", s.LLM.TimeoutSec))
		}
	} else if s.LLM.Endpoint != "" || s.LLM.APIKey != "" {
		// User explicitly turned LLM OFF — strip the key.
		apply("OPENAI_API_KEY", "")
	}
	return env
}

// probeCwd returns the directory the probe subprocess should run in
// for an *in-place* re-probe (no URL switch). When workdir ends in
// `tests/e2e` (the normal serve layout) we step up two parents so
// probe re-emits into the same project root. Otherwise the workdir
// itself is used.
func probeCwd(workdir string) string {
	abs := filepath.Clean(workdir)
	parent := filepath.Dir(abs)
	if filepath.Base(parent) == "tests" && filepath.Base(abs) == "e2e" {
		return filepath.Dir(parent)
	}
	return abs
}

// pickProbeDestination chooses where the probe subprocess should
// write. If the URL's brand matches the current workdir's name, we
// re-probe in place (probeCwd). Otherwise we land in a new sibling
// dir named after `BrandFromOrigin(url)`, suffixing `-1`, `-2`, etc.
// if a non-reviewqa dir already squats the slot.
//
// Scratch mode (v0.85): when the current workdir is empty or
// doesn't exist on disk, we have no parent dir to plant a sibling
// in. Fall back to a per-user projects root at
// `~/reviewqa-projects/<brand>/` — same XDG-ish location pattern
// as the settings file at `~/.config/reviewqa/serve.json`. Falls
// back to cwd if the user's home dir can't be resolved.
//
// Created dirs are mkdir'd here so the probe subprocess has a cwd to
// run in. The frontend reads the destination from the SSE `start`
// event so it can /api/switch-project to the new dir on `done`.
func pickProbeDestination(workdir string, u *url.URL) string {
	brand := probe.BrandFromHost(u.Host)
	// Scratch mode: no current project; pick the user-level root.
	if workdir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			parent := filepath.Join(home, "reviewqa-projects")
			return reserveSiblingDir(parent, brand, workdir)
		}
		// Last-resort fallback: cwd.
		cwd, _ := os.Getwd()
		return reserveSiblingDir(cwd, brand, workdir)
	}
	if info, err := os.Stat(workdir); err != nil || !info.IsDir() {
		if home, err := os.UserHomeDir(); err == nil {
			parent := filepath.Join(home, "reviewqa-projects")
			return reserveSiblingDir(parent, brand, workdir)
		}
		cwd, _ := os.Getwd()
		return reserveSiblingDir(cwd, brand, workdir)
	}
	current := probeCwd(workdir)
	if brand == "" {
		return current
	}
	// If the current project's name matches the brand, re-probe in place.
	if strings.EqualFold(filepath.Base(current), brand) ||
		strings.HasPrefix(strings.ToLower(filepath.Base(current)), brand) {
		return current
	}
	return reserveSiblingDir(filepath.Dir(current), brand, current)
}

// reserveSiblingDir picks a destination subdir of `parent` named
// after `brand`. If `parent/brand` is free it's created and
// returned. If a reviewqa-looking project already exists there we
// re-probe in place. If a non-reviewqa dir (or non-dir file) is
// squatting the slot, we suffix `-1`, `-2`, … until we find a free
// or compatible slot. Returns `fallback` if anything fails (parent
// can't be created, 100 collisions, etc).
func reserveSiblingDir(parent, brand, fallback string) string {
	if brand == "" {
		return fallback
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fallback
	}
	candidate := filepath.Join(parent, brand)
	for i := 1; i < 100; i++ {
		info, err := os.Stat(candidate)
		if err != nil {
			// Doesn't exist — create it and use it.
			if mkerr := os.MkdirAll(candidate, 0o755); mkerr == nil {
				return candidate
			}
			return fallback
		}
		if !info.IsDir() {
			candidate = filepath.Join(parent, brand+"-"+strconv.Itoa(i))
			continue
		}
		// Exists as a dir. Reuse if it's a reviewqa project (re-
		// probe in place) OR if it's empty (likely a residue from
		// a prior aborted probe or scratch-mode warmup — safe to
		// land into). Otherwise suffix.
		if looksLikeReviewqaProject(candidate) {
			return candidate
		}
		if entries, derr := os.ReadDir(candidate); derr == nil && len(entries) == 0 {
			return candidate
		}
		candidate = filepath.Join(parent, brand+"-"+strconv.Itoa(i))
	}
	return fallback
}

// handleProbe is the http handler. Registered by Run as
// POST /api/probe. Validates the JSON body and delegates the streaming
// to ProbeStream.
func handleProbe(workdir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req ProbeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, err := ProbeStream(r.Context(), w, workdir, req); err != nil {
			// If the stream hasn't been initiated yet, surface as 400.
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
}
