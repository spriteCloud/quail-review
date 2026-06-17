package mindmap

import (
	"context"
	"net/url"
	"strings"
)

// sitemapsFromRobotsTxt fetches `<origin>/robots.txt` and returns the
// URLs declared in `Sitemap:` directives. Returns nil on any failure —
// the site simply doesn't have a robots.txt or it doesn't declare
// sitemaps, both of which are normal.
//
// The robots.txt parsing here is intentionally narrow: it only looks at
// `Sitemap:` lines (case-insensitive). Disallow / Allow rules are
// parsed separately by ParseRobotsRules so the crawl layer can decide
// whether to honor them.
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

// RobotsRules is the subset of robots.txt rules the crawl layer cares
// about: the Allow / Disallow path prefixes that apply to a "*" user
// agent. Per the de-facto robots spec, a request is permitted if the
// longest-matching Allow rule is longer than the longest-matching
// Disallow rule.
type RobotsRules struct {
	Allow    []string
	Disallow []string
	// Loaded reports whether ParseRobotsRules saw a robots.txt at all.
	// When false, AllowPath returns true unconditionally — the absence
	// of a robots.txt is "no restrictions", not "deny everything".
	Loaded bool
}

// LoadRobotsRules fetches and parses `<origin>/robots.txt` returning a
// RobotsRules ready to consult via AllowPath. Returns a Loaded=false
// rules on any fetch / empty-body failure.
func LoadRobotsRules(ctx context.Context, originRoot string, fetch Fetcher) RobotsRules {
	body, _, err := fetch(ctx, originRoot+"/robots.txt")
	if err != nil || len(body) == 0 {
		return RobotsRules{}
	}
	return parseRobotsRules(string(body))
}

// parseRobotsRules reads the body of a robots.txt and extracts the
// Allow/Disallow rules from the "*" user-agent block. Other named
// blocks (Googlebot, etc.) are ignored — we crawl as a generic agent.
func parseRobotsRules(body string) RobotsRules {
	out := RobotsRules{Loaded: true}
	var inWildcard bool
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "user-agent:"):
			agent := strings.TrimSpace(line[len("user-agent:"):])
			inWildcard = agent == "*"
		case inWildcard && strings.HasPrefix(lower, "disallow:"):
			rule := strings.TrimSpace(line[len("disallow:"):])
			if rule != "" {
				out.Disallow = append(out.Disallow, rule)
			}
		case inWildcard && strings.HasPrefix(lower, "allow:"):
			rule := strings.TrimSpace(line[len("allow:"):])
			if rule != "" {
				out.Allow = append(out.Allow, rule)
			}
		}
	}
	return out
}

// AllowPath returns true when the rules permit fetching the given URL.
// When no robots.txt was loaded, returns true. When loaded, applies the
// standard longest-match precedence: the path is allowed if and only if
// the longest matching Allow rule is longer than the longest matching
// Disallow rule. Ties resolve to Allow.
func (r RobotsRules) AllowPath(rawURL string) bool {
	if !r.Loaded {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	p := u.Path
	if p == "" {
		p = "/"
	}
	allowLen := longestPrefix(r.Allow, p)
	disallowLen := longestPrefix(r.Disallow, p)
	if disallowLen == 0 {
		return true
	}
	return allowLen >= disallowLen
}

// longestPrefix returns the length of the longest rule that is a path
// prefix of p, or zero when no rule matches.
func longestPrefix(rules []string, p string) int {
	best := 0
	for _, r := range rules {
		if !strings.HasPrefix(p, r) {
			continue
		}
		if len(r) > best {
			best = len(r)
		}
	}
	return best
}
