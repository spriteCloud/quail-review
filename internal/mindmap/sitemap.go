package mindmap

import (
	"context"
	"encoding/xml"
	"strings"
)

// sitemapURLSet matches the schema at https://www.sitemaps.org/schemas/sitemap/0.9
type sitemapURLSet struct {
	XMLName xml.Name      `xml:"urlset"`
	URLs    []sitemapItem `xml:"url"`
}

type sitemapIndex struct {
	XMLName  xml.Name        `xml:"sitemapindex"`
	Sitemaps []sitemapNested `xml:"sitemap"`
}

type sitemapItem struct {
	Loc string `xml:"loc"`
}

type sitemapNested struct {
	Loc string `xml:"loc"`
}

// discoverSitemapURLs fetches <origin>/sitemap.xml and returns the same-origin
// URLs listed under <loc>. If the body is a sitemap-index, it follows up to 3
// nested sitemaps. Filters through isAvoidedPath. Caps at 200 URLs total to
// bound work on enormous sites.
//
// Returns an empty slice (and no error) when sitemap.xml is missing or
// unparseable — sites without one fall through to plain link-walking.
func discoverSitemapURLs(ctx context.Context, originRoot string, fetch Fetcher) []string {
	const maxNestedSitemaps = 3
	const maxURLs = 200

	// Step 1: robots.txt-declared sitemap URLs. Many sites publish
	// non-default sitemap paths (e.g. /wp-sitemap.xml, /sitemap-posts.xml,
	// /sitemap-news.xml) and only announce them via robots.txt. Reading
	// those first gives us the site's own opinion on what to crawl.
	var urls []string
	for _, u := range sitemapsFromRobotsTxt(ctx, originRoot, fetch) {
		urls = append(urls, readSitemap(ctx, u, fetch)...)
	}

	// Step 2: default sitemap path.
	primary := originRoot + "/sitemap.xml"
	if found := readSitemap(ctx, primary, fetch); len(found) > 0 {
		urls = append(urls, found...)
	}

	if len(urls) == 0 {
		// Some sites publish only sitemap_index.xml.
		urls = readSitemap(ctx, originRoot+"/sitemap_index.xml", fetch)
	}

	// If the top-level is an index of nested sitemaps, those URLs end in
	// ".xml" and are themselves sitemaps. Recurse one level.
	out := make([]string, 0, len(urls))
	nestedSeen := 0
	for _, u := range urls {
		if strings.HasSuffix(strings.ToLower(u), ".xml") && nestedSeen < maxNestedSitemaps {
			nestedSeen++
			out = append(out, readSitemap(ctx, u, fetch)...)
			continue
		}
		out = append(out, u)
		if len(out) >= maxURLs {
			break
		}
	}

	// Filter to same-origin + not avoided.
	keep := make([]string, 0, len(out))
	seen := map[string]bool{}
	for _, u := range out {
		canonical := canonicalURL(u)
		if canonical == "" {
			continue
		}
		// absoluteSameOrigin handles SSRF/avoid-path; pass canonical as both
		// base and href so the resolver simply validates the URL.
		resolved := absoluteSameOrigin(originRoot, canonical, canonical)
		if resolved == "" {
			continue
		}
		if seen[resolved] {
			continue
		}
		seen[resolved] = true
		keep = append(keep, resolved)
		if len(keep) >= maxURLs {
			break
		}
	}
	return keep
}

// readSitemap fetches one sitemap URL and returns its <loc> entries.
// Returns an empty slice on any failure (missing, non-200, bad XML).
func readSitemap(ctx context.Context, url string, fetch Fetcher) []string {
	body, _, err := fetch(ctx, url)
	if err != nil || len(body) == 0 {
		return nil
	}
	// Try urlset first; if that fails, try sitemapindex.
	var set sitemapURLSet
	if err := xml.Unmarshal(body, &set); err == nil && len(set.URLs) > 0 {
		out := make([]string, 0, len(set.URLs))
		for _, u := range set.URLs {
			loc := strings.TrimSpace(u.Loc)
			if loc != "" {
				out = append(out, loc)
			}
		}
		return out
	}
	var idx sitemapIndex
	if err := xml.Unmarshal(body, &idx); err == nil {
		out := make([]string, 0, len(idx.Sitemaps))
		for _, s := range idx.Sitemaps {
			loc := strings.TrimSpace(s.Loc)
			if loc != "" {
				out = append(out, loc)
			}
		}
		return out
	}
	return nil
}
