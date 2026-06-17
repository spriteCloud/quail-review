package mindmap

import (
	"context"
	"strings"
)

// sitemapsFromRobotsTxt fetches `<origin>/robots.txt` and returns the
// URLs declared in `Sitemap:` directives. Returns nil on any failure —
// the site simply doesn't have a robots.txt or it doesn't declare
// sitemaps, both of which are normal.
//
// The robots.txt parsing is deliberately conservative: we only honor
// `Sitemap:` lines (case-insensitive). Disallow rules are NOT parsed
// here — that's a separate concern handled at the crawl layer.
func sitemapsFromRobotsTxt(ctx context.Context, originRoot string, fetch Fetcher) []string {
	body, _, err := fetch(ctx, originRoot+"/robots.txt")
	if err != nil || len(body) == 0 {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Sitemap: <URL>
		if !strings.HasPrefix(strings.ToLower(line), "sitemap:") {
			continue
		}
		idx := strings.IndexByte(line, ':')
		if idx == -1 || idx == len(line)-1 {
			continue
		}
		url := strings.TrimSpace(line[idx+1:])
		if url != "" {
			out = append(out, url)
		}
	}
	return out
}
