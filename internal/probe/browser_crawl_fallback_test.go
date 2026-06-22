package probe

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/spriteCloud/quail-review/internal/mindmap"
	"github.com/spriteCloud/quail-review/internal/probe/browser"
)

// v0.88: when the browser runner can't start (node missing, npm
// install failed, no network), `--browser always` must surface the
// problem instead of silently degrading to static. A real crawl that
// just came back empty is a different signal — that one still falls
// back to static.

func TestCrawlOriginWithFallback_AlwaysBailsOnUnavailable(t *testing.T) {
	prev := browserCrawler
	t.Cleanup(func() { browserCrawler = prev })
	browserCrawler = func(ctx context.Context, u string, engine EngineMode, stealth bool, opts mindmap.Options) (*mindmap.Map, []error) {
		return nil, []error{fmt.Errorf("%w: node missing", browser.ErrBrowserUnavailable)}
	}

	var fetcherCalls atomic.Int32
	fetch := func(ctx context.Context, u string) ([]byte, string, error) {
		fetcherCalls.Add(1)
		return nil, "", errors.New("should not be called")
	}

	m, errs := crawlOriginWithFallback(context.Background(), "https://example.com/", mindmap.Fetcher(fetch), mindmap.Options{}, BrowserAlways)
	if m != nil {
		t.Errorf("expected nil map on Unavailable; got %+v", m)
	}
	if len(errs) == 0 || !errors.Is(errs[0], browser.ErrBrowserUnavailable) {
		t.Errorf("expected ErrBrowserUnavailable in errs; got %v", errs)
	}
	if got := fetcherCalls.Load(); got != 0 {
		t.Errorf("static fetcher called %d times; want 0 (BrowserAlways must not silently degrade)", got)
	}
}

// v0.89 cascade: auto mode walks chromium → firefox → webkit and
// stops at the first engine that returns pages. Mirrors how WAF-
// fingerprinted sites like ing.nl drop Playwright Chromium at the
// TLS layer but accept Firefox/WebKit through.
func TestCrawlOriginWithFallback_AutoCascadeStopsAtFirstSuccess(t *testing.T) {
	prev := browserCrawler
	t.Cleanup(func() { browserCrawler = prev })

	var calls []EngineMode
	browserCrawler = func(ctx context.Context, u string, engine EngineMode, stealth bool, opts mindmap.Options) (*mindmap.Map, []error) {
		calls = append(calls, engine)
		if engine == EngineFirefox {
			return &mindmap.Map{
				Origin: u,
				Pages:  map[string]*mindmap.Page{u: {URL: u}},
				Order:  []string{u},
			}, nil
		}
		return &mindmap.Map{Origin: u, Pages: map[string]*mindmap.Page{}}, nil
	}

	m, _ := crawlOriginWithFallback(context.Background(), "https://example.com/", nil, mindmap.Options{}, BrowserAlways)
	if m == nil || len(m.Pages) != 1 {
		t.Fatalf("expected firefox's populated map; got %+v", m)
	}
	if len(calls) != 2 || calls[0] != EngineChromium || calls[1] != EngineFirefox {
		t.Errorf("cascade order wrong: got %v; want [chromium firefox] (webkit must not be called)", calls)
	}
}

// When the user pins a single engine (--engine webkit), the cascade
// is a one-element slice — chromium / firefox MUST NOT be called.
func TestCrawlOriginWithFallback_ExplicitEngineDoesNotCascade(t *testing.T) {
	prev := browserCrawler
	t.Cleanup(func() { browserCrawler = prev })

	var calls []EngineMode
	browserCrawler = func(ctx context.Context, u string, engine EngineMode, stealth bool, opts mindmap.Options) (*mindmap.Map, []error) {
		calls = append(calls, engine)
		return &mindmap.Map{Origin: u, Pages: map[string]*mindmap.Page{}}, nil
	}

	ctx := WithEngineMode(context.Background(), EngineWebKit)
	_, _ = crawlOriginWithFallback(ctx, "https://example.com/",
		func(ctx context.Context, u string) ([]byte, string, error) {
			return []byte("<html></html>"), "text/html", nil
		}, mindmap.Options{}, BrowserAlways)

	if len(calls) != 1 || calls[0] != EngineWebKit {
		t.Errorf("explicit engine cascade: got %v; want [webkit] only", calls)
	}
}

func TestCrawlOriginWithFallback_AlwaysFallsBackOnZeroPages(t *testing.T) {
	prev := browserCrawler
	t.Cleanup(func() { browserCrawler = prev })
	browserCrawler = func(ctx context.Context, u string, engine EngineMode, stealth bool, opts mindmap.Options) (*mindmap.Map, []error) {
		// Empty mindmap with no Unavailable signal — a real browser
		// crawl that just found nothing. Static fallback is correct.
		return &mindmap.Map{Origin: u, Pages: map[string]*mindmap.Page{}}, nil
	}

	var fetcherCalls atomic.Int32
	fetch := func(ctx context.Context, u string) ([]byte, string, error) {
		fetcherCalls.Add(1)
		return []byte("<html><body>ok</body></html>"), "text/html", nil
	}

	_, _ = crawlOriginWithFallback(context.Background(), "https://example.com/", mindmap.Fetcher(fetch), mindmap.Options{}, BrowserAlways)

	if got := fetcherCalls.Load(); got == 0 {
		t.Error("expected static fetcher to be called on browser-returned-zero-pages; got 0 calls")
	}
}
