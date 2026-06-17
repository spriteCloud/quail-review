package plan

import (
	"testing"

	"reflect"
)

func TestExtractImages_KeepsAlt(t *testing.T) {
	html := []byte(`<html><body>
<img src="/hero.png" alt="Hero illustration showing two people">
<img src="/decorative.png" alt="">
<img src="/no-alt.png">
<img data-src="/lazy.png" alt="Lazy hero">
</body></html>`)
	out := ExtractImages("page.html", html)
	if len(out) != 2 {
		t.Fatalf("expected 2 images with alt; got %d: %+v", len(out), out)
	}
	if out[0].Alt != "Hero illustration showing two people" {
		t.Errorf("first image alt = %q; want descriptive", out[0].Alt)
	}
	if out[0].Src != "/hero.png" {
		t.Errorf("first image src = %q; want /hero.png", out[0].Src)
	}
	if out[1].Src != "/lazy.png" {
		t.Errorf("data-src fallback didn't kick in; got src=%q", out[1].Src)
	}
}

func TestExtractImages_Cap(t *testing.T) {
	var html string
	for i := 0; i < 20; i++ {
		html += `<img src="/x.png" alt="Image">`
	}
	out := ExtractImages("page.html", []byte(html))
	if len(out) != 8 {
		t.Errorf("expected cap of 8; got %d", len(out))
	}
}

func TestExtractMetaTags(t *testing.T) {
	html := []byte(`<html><head>
<meta name="description" content="A clean test page">
<meta name="viewport" content="width=device-width">
<meta property="og:title" content="Test Page">
<meta property="og:type" content="article">
<meta content="OG description here" property="og:description">
<link rel="canonical" href="https://example.com/test">
</head></html>`)
	meta := ExtractMetaTags(html)
	if meta.Description != "A clean test page" {
		t.Errorf("description = %q", meta.Description)
	}
	if meta.OGTitle != "Test Page" {
		t.Errorf("og:title = %q", meta.OGTitle)
	}
	if meta.OGType != "article" {
		t.Errorf("og:type = %q", meta.OGType)
	}
	if meta.OGDescription != "OG description here" {
		t.Errorf("og:description = %q (reverse-attr order must still parse)", meta.OGDescription)
	}
	if meta.ViewportContent != "width=device-width" {
		t.Errorf("viewport = %q", meta.ViewportContent)
	}
	if meta.Canonical != "https://example.com/test" {
		t.Errorf("canonical = %q", meta.Canonical)
	}
}

func TestExtractMetaTags_Empty(t *testing.T) {
	html := []byte(`<html><body><h1>No meta</h1></body></html>`)
	meta := ExtractMetaTags(html)
	if meta.OGTitle != "" || meta.Description != "" || meta.Canonical != "" {
		t.Errorf("expected zero-value MetaTags for meta-less page; got %+v", meta)
	}
}
