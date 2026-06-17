package browser

import (
	"os"
	"strings"
	"testing"
)

func TestWriteScript_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path, cleanup, err := WriteScript(dir)
	if err != nil {
		t.Fatalf("WriteScript: %v", err)
	}
	defer cleanup()

	if !strings.HasPrefix(path, dir) {
		t.Errorf("script written outside baseDir: %s (baseDir=%s)", path, dir)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written script: %v", err)
	}
	for _, want := range []string{
		"@playwright/test",
		"chromium.launch",
		"process.stdout.write",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("written script missing %q", want)
		}
	}
}

func TestWriteScript_CleanupRemovesDir(t *testing.T) {
	dir := t.TempDir()
	path, cleanup, err := WriteScript(dir)
	if err != nil {
		t.Fatalf("WriteScript: %v", err)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected script removed after cleanup; got err=%v", err)
	}
}
