package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/mindmap"
)

func TestFetch_HappyPath(t *testing.T) {
	t.Setenv("QUAIL_PROBE_ALLOW_LOOPBACK", "1")
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
	t.Setenv("QUAIL_PROBE_ALLOW_LOOPBACK", "")
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
	if item.OutPath != "tests/e2e/landing.spec.ts" {
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
	if item.Symbol.Name != "Spritecloud" {
		t.Errorf("Name = %q, want Spritecloud", item.Symbol.Name)
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

func TestRunAll_EmitsMultipleJourneys(t *testing.T) {
	t.Setenv("QUAIL_PROBE_ALLOW_LOOPBACK", "1")
	// Two-page site shaped like a marketing brochure:
	//   /         — landing with an h1 + nav links
	//   /contact  — detail page (h1 + few links)
	// Expect mindmap to identify ≥1 journey (e.g. explore: home → contact).
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`<html><body><h1>Home</h1><a href="/contact">Contact us</a><a href="/about">About</a></body></html>`))
	})
	mux.HandleFunc("/contact", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><h1>Contact</h1></body></html>`))
	})
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><h1>About</h1></body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	items, errs := RunAll(context.Background(), []string{srv.URL + "/"})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(items) == 0 {
		t.Fatalf("expected ≥1 journey item, got 0")
	}
	// Each item must carry at least one symbol; chained items have
	// EnteredVia populated on every non-first symbol.
	for _, it := range items {
		if len(it.Symbols) == 0 {
			t.Errorf("item with no symbols: %+v", it)
		}
		for i, s := range it.Symbols {
			if i > 0 && s.EnteredVia == "" {
				t.Errorf("chained symbol %d missing EnteredVia: %+v", i, s)
			}
		}
	}
}

func TestOutPathStemForJourney_ExerciseGetsSlugSuffix(t *testing.T) {
	// Two exercise journeys against different pages on the same site
	// must produce DIFFERENT filenames so they don't overwrite each
	// other on disk. Before v0.12.1 both stems were just `host-exercise`.
	landing := &mindmap.Page{URL: "https://x.test/"}
	about := &mindmap.Page{URL: "https://x.test/about"}
	stemLanding := outPathStemForJourney(
		mindmap.Journey{Kind: mindmap.JourneyExercise, Steps: []mindmap.Step{{Page: landing}}},
	)
	stemAbout := outPathStemForJourney(
		mindmap.Journey{Kind: mindmap.JourneyExercise, Steps: []mindmap.Step{{Page: about}}},
	)
	if stemLanding == stemAbout {
		t.Errorf("expected different stems for landing vs /about exercise; got both = %q", stemLanding)
	}
	if stemLanding != "exercise" {
		t.Errorf("landing exercise stem = %q; want exercise", stemLanding)
	}
	if stemAbout != "exercise-about" {
		t.Errorf("about exercise stem = %q; want exercise-about", stemAbout)
	}
}

func TestRunAll_AggregatesErrors(t *testing.T) {
	t.Setenv("QUAIL_PROBE_ALLOW_LOOPBACK", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><h1>Home</h1><a href="/about">About</a></body></html>`))
	}))
	defer srv.Close()
	urls := []string{srv.URL, "file:///etc/passwd", "  "}
	items, errs := RunAll(context.Background(), urls)
	if len(items) == 0 {
		t.Errorf("expected ≥1 item from good URL, got %d", len(items))
	}
	if len(errs) == 0 {
		t.Errorf("expected error from file://, got none")
	}
}
