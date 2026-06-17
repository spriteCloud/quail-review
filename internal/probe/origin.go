package probe

import (
	"strings"

	"golang.org/x/net/publicsuffix"
)

// BrandFromHost normalises a hostname into the brand-only domain:
//   - drops any port suffix,
//   - strips the leading "www." subdomain,
//   - strips the public suffix (".com", ".co.uk", ".github.io", …).
//
// "www.spritecloud.com"     → "spritecloud"
// "petstore3.swagger.io"    → "petstore3.swagger"
// "blog.example.co.uk"      → "blog.example"
//
// Used by both the probe symbol-name path (`hostToName`, which then
// camel-cases the result) and the stakeholder-summary template helper
// (`gen.brandFromOrigin`, which uses the lowercase form directly).
// Centralising the scheme/www/TLD stripping rules here avoids drift
// between the two readers.
func BrandFromHost(host string) string {
	if i := strings.IndexByte(host, ':'); i != -1 {
		host = host[:i]
	}
	cleaned := strings.ToLower(strings.TrimSpace(host))
	if suffix, _ := publicsuffix.PublicSuffix(cleaned); suffix != "" && strings.HasSuffix(cleaned, "."+suffix) {
		cleaned = strings.TrimSuffix(cleaned, "."+suffix)
	}
	cleaned = strings.TrimPrefix(cleaned, "www.")
	return cleaned
}

// BrandFromOrigin accepts a full origin like "https://www.example.com"
// and returns its brand domain. Convenience wrapper around BrandFromHost
// for callers that have a URL string rather than a hostname.
func BrandFromOrigin(origin string) string {
	s := strings.TrimSpace(origin)
	for _, p := range []string{"https://", "http://"} {
		if strings.HasPrefix(strings.ToLower(s), p) {
			s = s[len(p):]
			break
		}
	}
	// Cut off path / query / fragment if present so they don't end up
	// in the brand string.
	for _, c := range []string{"/", "?", "#"} {
		if i := strings.Index(s, c); i != -1 {
			s = s[:i]
		}
	}
	return BrandFromHost(s)
}
