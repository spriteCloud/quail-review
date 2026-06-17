package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/reviewqa/reviewqa/internal/plan"
)

func v044TestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	})
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head><title>About</title></head><body><h1>About</h1></body></html>`))
	})
	return httptest.NewServer(mux)
}

func TestRunAll_EmitsFileUploadOnlyWhenFileInputDetected(t *testing.T) {
	t.Setenv("REVIEWQA_PROBE_ALLOW_LOOPBACK", "1")
	withFile := v044TestServer(t, `<html><head><title>Home</title></head><body><h1>Home</h1><a href="/about">About</a><form><input type="file" name="avatar"><button type="submit">Upload</button></form></body></html>`)
	defer withFile.Close()
	items, _ := RunAll(context.Background(), []string{withFile.URL + "/"})
	found := 0
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightFileUpload {
			found++
			if !strings.Contains(it.OutPath, "/file-upload/") {
				t.Errorf("file-upload spec at unexpected path: %s", it.OutPath)
			}
		}
	}
	if found == 0 {
		t.Error("expected pw_file_upload spec when probe detects <input type=file>")
	}

	// Same probe, no file input — no emission.
	plain := v044TestServer(t, `<html><head><title>Home</title></head><body><h1>Home</h1><a href="/about">About</a></body></html>`)
	defer plain.Close()
	items, _ = RunAll(context.Background(), []string{plain.URL + "/"})
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightFileUpload {
			t.Errorf("file-upload spec should NOT emit without a file input; got %s", it.OutPath)
		}
	}
}

func TestRunAll_EmitsIframeOnlyWhenIframePresent(t *testing.T) {
	t.Setenv("REVIEWQA_PROBE_ALLOW_LOOPBACK", "1")
	withIframe := v044TestServer(t, `<html><head><title>Home</title></head><body><h1>Home</h1><a href="/about">About</a><iframe src="//example.com/widget"></iframe></body></html>`)
	defer withIframe.Close()
	items, _ := RunAll(context.Background(), []string{withIframe.URL + "/"})
	found := 0
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightIframe {
			found++
		}
	}
	if found == 0 {
		t.Error("expected pw_iframe spec when probe detects <iframe>")
	}
}

func TestRunAll_EmitsPWAOnlyWhenManifestLink(t *testing.T) {
	t.Setenv("REVIEWQA_PROBE_ALLOW_LOOPBACK", "1")
	withManifest := v044TestServer(t, `<html><head><title>Home</title><link rel="manifest" href="/manifest.json"></head><body><h1>Home</h1><a href="/about">About</a></body></html>`)
	defer withManifest.Close()
	items, _ := RunAll(context.Background(), []string{withManifest.URL + "/"})
	found := 0
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightPWA {
			found++
		}
	}
	if found == 0 {
		t.Error("expected pw_pwa spec when probe detects <link rel=manifest>")
	}
}

func TestRunAll_AlwaysEmitsHistoryDepth(t *testing.T) {
	t.Setenv("REVIEWQA_PROBE_ALLOW_LOOPBACK", "1")
	srv := v044TestServer(t, `<html><head><title>Home</title></head><body><h1>Home</h1><a href="/about">About</a></body></html>`)
	defer srv.Close()
	items, _ := RunAll(context.Background(), []string{srv.URL + "/"})
	found := 0
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightHistoryDepth {
			found++
		}
	}
	if found == 0 {
		t.Error("expected pw_history_depth spec on every probe")
	}
}

func TestPageHasInputType_NilSafe(t *testing.T) {
	if pageHasInputType(nil, "file") {
		t.Error("nil page should return false")
	}
}
