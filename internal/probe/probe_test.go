package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetch_HappyPath(t *testing.T) {
	t.Setenv("REVIEWQA_PROBE_ALLOW_LOOPBACK", "1")
	body := `<!doctype html><html><body>
<form data-testid="signup">
  <input type="email" name="email" required />
  <button type="submit">Go</button>
</form>
<a href="/about">About</a>
</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("expected User-Agent header")
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	res, err := Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(string(res.Body), `data-testid="signup"`) {
		t.Errorf("body missing expected markup: %s", res.Body)
	}
}

func TestFetch_RejectsNonHTTPScheme(t *testing.T) {
	if _, err := Fetch(context.Background(), "file:///etc/passwd"); err == nil {
		t.Error("expected error for file:// scheme")
	}
	if _, err := Fetch(context.Background(), "ftp://example.com"); err == nil {
		t.Error("expected error for ftp:// scheme")
	}
}

func TestFetch_RejectsLoopback(t *testing.T) {
	// Without the escape hatch, loopback must be blocked.
	t.Setenv("REVIEWQA_PROBE_ALLOW_LOOPBACK", "")
	if _, err := Fetch(context.Background(), "http://127.0.0.1:9/"); err == nil {
		t.Error("expected error for loopback address")
	}
}

func TestBuildItem(t *testing.T) {
	html := []byte(`<!doctype html><html><body>
<header data-testid="hero">hello</header>
<form>
  <input type="email" name="email" data-testid="email-input" required />
  <button type="submit">Go</button>
</form>
<a href="/about">About</a>
</body></html>`)
	item, err := BuildItem("https://www.spritecloud.com/", html)
	if err != nil {
		t.Fatalf("BuildItem: %v", err)
	}
	if item.PageURL != "https://www.spritecloud.com/" {
		t.Errorf("PageURL = %q, want absolute URL", item.PageURL)
	}
	if item.OutPath != "tests/e2e/spritecloud-com.spec.ts" {
		t.Errorf("OutPath = %q", item.OutPath)
	}
	if len(item.Symbol.Inputs) != 1 {
		t.Errorf("expected 1 input, got %d", len(item.Symbol.Inputs))
	}
	if len(item.Symbol.Links) != 1 || item.Symbol.Links[0].Aria != "/about" {
		t.Errorf("expected one /about link, got %+v", item.Symbol.Links)
	}
	if !item.Symbol.HasForm {
		t.Error("HasForm should be true")
	}
	if item.Symbol.Name != "WwwSpritecloudCom" {
		t.Errorf("Name = %q, want WwwSpritecloudCom", item.Symbol.Name)
	}
}

func TestBuildItem_DedupsRepeatedAnchors(t *testing.T) {
	// Three identical <header role="banner"> should collapse to one anchor.
	html := []byte(`<html><body>
<header role="banner">a</header>
<header role="banner">b</header>
<header role="banner">c</header>
<a href="/about">A</a>
<a href="/about">A again</a>
</body></html>`)
	item, err := BuildItem("https://example.com/", html)
	if err != nil {
		t.Fatal(err)
	}
	bannerCount := 0
	for _, a := range item.Symbol.Anchors {
		if a.Role == "banner" {
			bannerCount++
		}
	}
	if bannerCount != 1 {
		t.Errorf("expected 1 banner anchor after dedup, got %d", bannerCount)
	}
	if len(item.Symbol.Links) != 1 {
		t.Errorf("expected 1 deduped link, got %d", len(item.Symbol.Links))
	}
}

func TestBuildItem_SpritecloudShape_FormIntent(t *testing.T) {
	// The actual shape of spritecloud.com's hero form. v0.6.x got this
	// wrong (classified as nav). After Fix 1 it must be form intent.
	html := []byte(`<!doctype html><html><head><title>spriteCloud — Test</title></head><body>
<header role="banner">x</header>
<h1>Test your software, not your reputation!</h1>
<form>
  <input type="email" name="email-2" placeholder="Your email address" required />
  <input type="submit" value="Submit" />
</form>
<a href="/contact">Contact us</a>
<a href="/case-studies">Case studies</a>
</body></html>`)
	item, err := BuildItem("https://www.spritecloud.com/", html)
	if err != nil {
		t.Fatal(err)
	}
	// Submit anchor with the input[type=submit] should be present with Tag=submit.
	hasSubmit := false
	for _, a := range item.Symbol.Anchors {
		if a.Tag == "submit" {
			hasSubmit = true
		}
	}
	if !hasSubmit {
		t.Fatalf("expected a submit anchor; got %+v", item.Symbol.Anchors)
	}
	// PageTitle captured.
	if !strings.Contains(item.Symbol.PageTitle, "spriteCloud") {
		t.Errorf("PageTitle = %q", item.Symbol.PageTitle)
	}
	// Content anchors include the h1.
	hasH1 := false
	for _, c := range item.Symbol.Contents {
		if c.Tag == "h1" && strings.Contains(c.Text, "Test your software") {
			hasH1 = true
		}
	}
	if !hasH1 {
		t.Errorf("expected h1 content anchor; got %+v", item.Symbol.Contents)
	}
}

func TestRunAll_LinearJourney(t *testing.T) {
	t.Setenv("REVIEWQA_PROBE_ALLOW_LOOPBACK", "1")
	// Two-page site: '/' nav-links to '/contact'. RunAll should produce
	// ONE Item whose Symbols carries the chain in order — not two separate
	// items. The second Symbol's EnteredVia must be "/contact".
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<html><body><h1>Home</h1><a href="/contact">Contact us</a></body></html>`))
	})
	mux.HandleFunc("/contact", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><h1>Contact</h1></body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	items, errs := RunAll(context.Background(), []string{srv.URL + "/"})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item (single linear journey), got %d", len(items))
	}
	if len(items[0].Symbols) != 2 {
		t.Fatalf("expected 2 symbols in chain, got %d", len(items[0].Symbols))
	}
	if items[0].Symbols[1].EnteredVia != "/contact" {
		t.Errorf("Symbols[1].EnteredVia = %q, want /contact", items[0].Symbols[1].EnteredVia)
	}
}

func TestRunAll_AggregatesErrors(t *testing.T) {
	t.Setenv("REVIEWQA_PROBE_ALLOW_LOOPBACK", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<form><input type="text" name="q" /></form>`))
	}))
	defer srv.Close()
	urls := []string{srv.URL, "file:///etc/passwd", "  "}
	items, errs := RunAll(context.Background(), urls)
	if len(items) != 1 {
		t.Errorf("expected 1 item from good URL, got %d", len(items))
	}
	if len(errs) != 1 {
		t.Errorf("expected 1 error from file://, got %d: %+v", len(errs), errs)
	}
}
