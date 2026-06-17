package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/reviewqa/reviewqa/internal/plan"
)

func TestRunAll_EmitsDragDropOnlyWhenDraggablePresent(t *testing.T) {
	t.Setenv("REVIEWQA_PROBE_ALLOW_LOOPBACK", "1")
	withDrag := v044TestServer(t, `<html><head><title>Home</title></head><body><h1>Home</h1><a href="/about">About</a><div draggable="true">drag me</div><div data-dropzone>drop here</div></body></html>`)
	defer withDrag.Close()
	items, _ := RunAll(context.Background(), []string{withDrag.URL + "/"})
	hits := 0
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightDragDrop {
			hits++
		}
	}
	if hits == 0 {
		t.Error("expected pw_dragdrop when probe sees [draggable]")
	}

	plain := v044TestServer(t, `<html><head><title>Home</title></head><body><h1>Home</h1><a href="/about">About</a></body></html>`)
	defer plain.Close()
	items, _ = RunAll(context.Background(), []string{plain.URL + "/"})
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightDragDrop {
			t.Errorf("pw_dragdrop should NOT emit without [draggable]; got %s", it.OutPath)
		}
	}
}

func TestRunAll_AlwaysEmitsTouchAndAuthExpiry(t *testing.T) {
	t.Setenv("REVIEWQA_PROBE_ALLOW_LOOPBACK", "1")
	srv := v044TestServer(t, `<html><head><title>Home</title></head><body><h1>Home</h1><a href="/about">About</a></body></html>`)
	defer srv.Close()
	items, _ := RunAll(context.Background(), []string{srv.URL + "/"})
	touch, auth := 0, 0
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightTouch {
			touch++
		}
		if it.Template == plan.TmplPlaywrightAuthExpiry {
			auth++
		}
	}
	if touch == 0 {
		t.Error("expected pw_touch on every probe (always-emitted)")
	}
	if auth == 0 {
		t.Error("expected pw_auth_expiry on every probe (always-emitted)")
	}
}

func TestRunAll_LocaleSwitchOnlyWhenHreflangSiblings(t *testing.T) {
	t.Setenv("REVIEWQA_PROBE_ALLOW_LOOPBACK", "1")
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<html><head><title>Home</title>
<link rel="alternate" hreflang="en" href="/en/"><link rel="alternate" hreflang="es" href="/es/">
</head><body><h1>Home</h1><a href="/about">About</a></body></html>`))
	})
	mux.HandleFunc("/en/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head><title>EN</title></head><body><h1>EN</h1></body></html>`))
	})
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><h1>About</h1></body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	items, _ := RunAll(context.Background(), []string{srv.URL + "/"})
	hits := 0
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightLocaleSwitch {
			hits++
		}
	}
	if hits == 0 {
		t.Error("expected pw_locale_switch when ≥2 hreflang siblings are present")
	}
}
