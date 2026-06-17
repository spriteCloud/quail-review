package serve

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Settings is the user-tunable serve configuration persisted to
// ~/.config/reviewqa/serve.json. Loaded once per Get() call (no
// long-lived cache) so a user can edit it from the UI and the next
// request sees the change without restarting the binary.
//
// All fields are optional. Zero values mean "fall back to the env-
// var / hard-coded default" — `Get()` performs the overlay.
type Settings struct {
	LLM   LLMSettings   `json:"llm"`
	Probe ProbeSettings `json:"probe"`
	Run   RunSettings   `json:"run"`
}

// LLMSettings persist the LLM composer / chat configuration. Empty
// Endpoint or `Enabled = false` returns the compose endpoints to
// deterministic mode.
type LLMSettings struct {
	Enabled    bool   `json:"enabled"`
	Endpoint   string `json:"endpoint,omitempty"`
	Model      string `json:"model,omitempty"`
	APIKey     string `json:"apiKey,omitempty"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
}

// ProbeSettings affect the /api/probe endpoint defaults — what
// coverage the HOME probe form selects when the user doesn't pick
// one explicitly.
type ProbeSettings struct {
	DefaultCoverage string `json:"defaultCoverage,omitempty"`
}

// RunSettings affect the /api/run-scenario endpoint — soft caps and
// post-run cleanup policy.
type RunSettings struct {
	TimeoutSec int  `json:"timeoutSec,omitempty"`
	KeepReport bool `json:"keepReport,omitempty"`
}

// settingsMu serialises writes so concurrent POSTs to /api/settings
// don't race. Reads are unsynchronised — the JSON marshal of a
// fresh-from-disk Settings is sequential anyway.
var settingsMu sync.Mutex

// SettingsPath returns the absolute path to the settings file under
// XDG_CONFIG_HOME (or ~/.config as the fallback). The file may not
// exist yet — LoadSettings tolerates that.
func SettingsPath() (string, error) {
	if base := strings.TrimSpace(os.Getenv("REVIEWQA_SETTINGS_PATH")); base != "" {
		return base, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "reviewqa", "serve.json"), nil
}

// LoadSettings reads the settings file, returning a zero-valued
// Settings if the file does not exist. Any parse error surfaces to
// the caller so the UI can prompt the user to fix it.
func LoadSettings() (Settings, error) {
	path, err := SettingsPath()
	if err != nil {
		return Settings{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Settings{}, nil
		}
		return Settings{}, err
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return Settings{}, err
	}
	return s, nil
}

// SaveSettings writes the settings file atomically (tmp + rename)
// with 0600 perms so the embedded API key is at least mode-protected
// against other local users. Creates the parent directory if needed.
func SaveSettings(s Settings) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
