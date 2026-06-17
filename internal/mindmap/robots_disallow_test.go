package mindmap

import "testing"

func TestParseRobotsRules_WildcardAgentOnly(t *testing.T) {
	body := `User-agent: Googlebot
Disallow: /private/

User-agent: *
Disallow: /admin/
Disallow: /draft/
Allow: /admin/public/

Sitemap: https://example.com/sitemap.xml`
	r := parseRobotsRules(body)
	if !r.Loaded {
		t.Fatal("Loaded should be true after parse")
	}
	if len(r.Disallow) != 2 {
		t.Errorf("Disallow rules = %v (expected 2 for wildcard agent)", r.Disallow)
	}
	if len(r.Allow) != 1 {
		t.Errorf("Allow rules = %v", r.Allow)
	}
	// Googlebot-only rules should NOT bleed into the wildcard set.
	for _, d := range r.Disallow {
		if d == "/private/" {
			t.Errorf("wildcard agent picked up Googlebot rule")
		}
	}
}

func TestAllowPath_NoRobotsTxt_AllowsEverything(t *testing.T) {
	r := RobotsRules{} // Loaded=false
	if !r.AllowPath("https://example.com/admin/secret") {
		t.Error("absent robots.txt should permit any path")
	}
}

func TestAllowPath_LongestMatchWins(t *testing.T) {
	r := RobotsRules{
		Loaded:   true,
		Disallow: []string{"/admin/"},
		Allow:    []string{"/admin/public/"},
	}
	cases := []struct {
		url   string
		allow bool
	}{
		{"https://example.com/", true},
		{"https://example.com/admin/secret", false},
		{"https://example.com/admin/public/page", true},  // longer Allow wins
		{"https://example.com/admin/public", false},      // doesn't match the Allow rule prefix; only "/admin/" matches
		{"https://example.com/blog/post-1", true},
	}
	for _, c := range cases {
		got := r.AllowPath(c.url)
		if got != c.allow {
			t.Errorf("AllowPath(%q) = %v; want %v", c.url, got, c.allow)
		}
	}
}

func TestAllowPath_EmptyPath(t *testing.T) {
	r := RobotsRules{Loaded: true, Disallow: []string{"/"}}
	// "/" disallow covers everything.
	if r.AllowPath("https://example.com") {
		t.Error("root-disallow should block any URL")
	}
}

func TestAllowPath_TiesGoToAllow(t *testing.T) {
	// Equal-length matches resolve to Allow per the de-facto spec.
	r := RobotsRules{
		Loaded:   true,
		Disallow: []string{"/foo"},
		Allow:    []string{"/foo"},
	}
	if !r.AllowPath("https://example.com/foo/bar") {
		t.Error("equal-length tie should favor Allow")
	}
}

func TestParseRobotsRules_EmptyBody_NotLoaded(t *testing.T) {
	r := parseRobotsRules("")
	if !r.Loaded {
		t.Error("parseRobotsRules treats empty as loaded-but-empty; this is by design")
	}
	if !r.AllowPath("https://example.com/anything") {
		t.Error("empty rules should still allow everything")
	}
}
