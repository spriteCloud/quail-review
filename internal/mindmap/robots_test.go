package mindmap

import (
	"context"
	"testing"
)

func TestSitemapsFromRobotsTxt(t *testing.T) {
	robots := `User-agent: *
Disallow: /admin/
Allow: /

Sitemap: https://x.test/sitemap-news.xml
sitemap: https://x.test/sitemap-products.xml
# Sitemap: https://x.test/commented-out.xml
Sitemap: https://x.test/sitemap-blog.xml
`
	pages := fakeFetcher{
		"https://x.test/robots.txt": robots,
	}
	got := sitemapsFromRobotsTxt(context.Background(), "https://x.test", pages.fetch)
	want := []string{
		"https://x.test/sitemap-news.xml",
		"https://x.test/sitemap-products.xml",
		"https://x.test/sitemap-blog.xml",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d sitemap URLs, got %d: %+v", len(want), len(got), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("sitemap[%d] = %q; want %q", i, got[i], want[i])
		}
	}
}

func TestSitemapsFromRobotsTxt_MissingRobots(t *testing.T) {
	pages := fakeFetcher{}
	got := sitemapsFromRobotsTxt(context.Background(), "https://x.test", pages.fetch)
	if got != nil {
		t.Errorf("expected nil for missing robots.txt; got %+v", got)
	}
}

func TestSitemapsFromRobotsTxt_NoSitemapDirective(t *testing.T) {
	pages := fakeFetcher{
		"https://x.test/robots.txt": "User-agent: *\nDisallow: /admin/\n",
	}
	got := sitemapsFromRobotsTxt(context.Background(), "https://x.test", pages.fetch)
	if got != nil {
		t.Errorf("expected nil when no sitemap declared; got %+v", got)
	}
}
