// Package probe fetches a live URL and synthesises a plan.Item carrying
// the page's anchors/inputs/links — so reviewqa can generate a Playwright
// happy-flow against the URL without any source code in the diff.
//
// The fetcher is deliberately conservative: short timeout, restricted to
// http(s), refuses redirects to private IP ranges, and limits redirects
// to at most 3 same-host hops. This keeps the action safe to run on
// hosted CI runners where SSRF would otherwise be a real concern.
package probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
)

const userAgent = "reviewqa-probe/1 (+https://github.com/spriteCloud/reviewqa)"

// Result is the outcome of a single Fetch.
type Result struct {
	URL  string
	Body []byte
}

// Fetch downloads the HTML at target. Returns (nil, error) on any
// safety guard failure (non-http(s) scheme, private-IP target, redirect
// off-host, etc.) or on transport / HTTP-status failure.
func Fetch(ctx context.Context, target string) (*Result, error) {
	u, err := parseAndValidate(target)
	if err != nil {
		return nil, err
	}
	if err := guardHost(u.Hostname()); err != nil {
		return nil, fmt.Errorf("probe: %w", err)
	}
	client := buildClient(u.Host)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("probe: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("probe: fetch %s: %w", target, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("probe: %s returned %s", target, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MiB cap
	if err != nil {
		return nil, fmt.Errorf("probe: read body: %w", err)
	}
	return &Result{URL: resp.Request.URL.String(), Body: body}, nil
}

func parseAndValidate(target string) (*url.URL, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("probe: parse %q: %w", target, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("probe: scheme %q not allowed (use http or https)", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("probe: missing host")
	}
	return u, nil
}

func guardHost(host string) error {
	if host == "" {
		return errors.New("empty host")
	}
	if os.Getenv("REVIEWQA_PROBE_ALLOW_LOOPBACK") == "1" {
		return nil // test / local-dev escape hatch
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		// DNS failure: let the http client surface a meaningful error.
		return nil
	}
	for _, ip := range ips {
		if isPrivate(ip) {
			return fmt.Errorf("host %q resolves to private/blocked IP %s", host, ip)
		}
	}
	return nil
}

func isPrivate(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 10,
			v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31,
			v4[0] == 192 && v4[1] == 168,
			v4[0] == 169 && v4[1] == 254,
			v4[0] == 127:
			return true
		}
	}
	return false
}

func buildClient(initialHost string) *http.Client {
	hops := 0
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			hops++
			if hops > 3 {
				return errors.New("too many redirects")
			}
			if req.URL.Host != initialHost {
				return fmt.Errorf("cross-host redirect blocked: %s → %s", initialHost, req.URL.Host)
			}
			if err := guardHost(req.URL.Hostname()); err != nil {
				return err
			}
			return nil
		},
	}
}

// BuildItem synthesises a plan.Item carrying the page's locator anchors,
// form inputs and links. The Item drives the pw_happyflow.tmpl template
// to produce a Playwright spec that hits the real URL.
func BuildItem(target string, html []byte) (plan.Item, error) {
	u, err := parseAndValidate(target)
	if err != nil {
		return plan.Item{}, err
	}
	anchors := ast.DedupAnchors(plan.ExtractHTMLAnchors(target, html))
	inputs := ast.DedupInputs(plan.ExtractHTMLInputs(target, html))
	links := ast.DedupLinks(plan.ExtractHTMLLinks(target, html))
	hasForm := strings.Contains(strings.ToLower(string(html)), "<form")

	name := hostToName(u.Hostname())
	stem := outPathStem(u)
	symbol := ast.Symbol{
		Name:     name,
		Kind:     ast.KindComponent,
		File:     target,
		Language: "ts",
		Anchors:  anchors,
		Inputs:   inputs,
		Links:    links,
		HasForm:  hasForm,
	}
	return plan.Item{
		Symbol:   symbol,
		Symbols:  []ast.Symbol{symbol},
		PageURL:  target, // absolute URL — template emits it verbatim
		Template: plan.TmplPlaywrightHappyFlow,
		OutPath:  filepath.ToSlash(filepath.Join("tests", "e2e", stem+".spec.ts")),
	}, nil
}

// hostToName turns "www.spritecloud.com" into "WwwSpritecloudCom".
func hostToName(host string) string {
	parts := strings.FieldsFunc(host, func(r rune) bool { return r == '.' || r == '-' })
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 {
			b.WriteString(strings.ToLower(p[1:]))
		}
	}
	if b.Len() == 0 {
		return "Probe"
	}
	return b.String()
}

// outPathStem produces a slug for the output spec filename. Hostname plus
// a normalised path. "https://www.spritecloud.com/services" → "spritecloud-com-services".
func outPathStem(u *url.URL) string {
	host := strings.ReplaceAll(u.Hostname(), ".", "-")
	host = strings.TrimPrefix(host, "www-")
	pathPart := strings.Trim(u.Path, "/")
	pathPart = strings.ReplaceAll(pathPart, "/", "-")
	if pathPart == "" {
		return host
	}
	return host + "-" + pathPart
}

// RunAll fetches every URL and returns the synthesised Items. Errors per
// URL are returned alongside successes — caller decides whether to fail
// or warn.
func RunAll(ctx context.Context, urls []string) ([]plan.Item, []error) {
	var items []plan.Item
	var errs []error
	for _, raw := range urls {
		u := strings.TrimSpace(raw)
		if u == "" {
			continue
		}
		res, err := Fetch(ctx, u)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		item, err := BuildItem(res.URL, res.Body)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		items = append(items, item)
	}
	return items, errs
}
