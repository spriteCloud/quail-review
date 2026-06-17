package serve

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

// mdEngine is configured once and reused. GFM enables tables,
// strikethrough, autolinks, and task lists — everything the
// reviewqa-emitted stakeholder docs (catalogue, findings) need.
var mdEngine = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
)

// renderMarkdown turns a markdown document into rendered HTML.
// WithUnsafe is enabled because the docs we render are produced by
// reviewqa itself in the local workdir — the user owns the input and
// HTML escaping would mangle the existing inline-HTML our templates
// occasionally emit (e.g. <details> blocks in findings.md).
func renderMarkdown(src []byte) string {
	var buf bytes.Buffer
	if err := mdEngine.Convert(src, &buf); err != nil {
		return "<pre>" + string(src) + "</pre>"
	}
	return buf.String()
}
