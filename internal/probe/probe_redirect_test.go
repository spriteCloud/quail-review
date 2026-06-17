package probe

import "testing"

func TestRegistrableDomain(t *testing.T) {
	tests := map[string]string{
		"www.example.com":        "example.com",
		"example.com":            "example.com",
		"blog.example.co.uk":     "example.co.uk",
		"m.example.com":          "example.com",
		"www.spritecloud.com":    "spritecloud.com",
		"":                       "",
		"localhost":              "localhost",
		"127.0.0.1":              "127.0.0.1",
	}
	for host, want := range tests {
		got := registrableDomain(host)
		if got != want {
			t.Errorf("registrableDomain(%q) = %q; want %q", host, got, want)
		}
	}
}

func TestSameRegistrableDomain(t *testing.T) {
	tests := []struct {
		base, candidate string
		want            bool
	}{
		{"example.com", "www.example.com", true},
		{"example.com", "example.com", true},
		{"example.com", "blog.example.com", true},
		{"example.com", "evil.com", false},
		{"example.com", "example.com.evil.com", false},
		{"example.co.uk", "www.example.co.uk", true},
		{"example.co.uk", "blog.example.co.uk", true},
		{"example.co.uk", "example.com", false},
	}
	for _, tc := range tests {
		got := sameRegistrableDomain(tc.base, tc.candidate)
		if got != tc.want {
			t.Errorf("sameRegistrableDomain(%q, %q) = %v; want %v", tc.base, tc.candidate, got, tc.want)
		}
	}
}
