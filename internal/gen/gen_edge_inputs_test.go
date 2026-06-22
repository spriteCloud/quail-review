package gen

import (
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestFileUploadTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightFileUpload, sym, "https://x.test/")
	mustContain(t, body, "@kind:file-upload")
	mustContain(t, body, "setInputFiles")
	mustContain(t, body, "Buffer.alloc(0)")
	mustContain(t, body, "náïve-文件名")
}

func TestIframeTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightIframe, sym, "https://x.test/")
	mustContain(t, body, "@kind:iframe")
	mustContain(t, body, "contentFrame")
	mustContain(t, body, "content security policy|csp")
}

func TestDateEdgesTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightDateEdges, sym, "https://x.test/")
	mustContain(t, body, "@kind:date-edges")
	mustContain(t, body, "2024-02-29")     // leap-year-feb-29
	mustContain(t, body, "2038-01-19")     // y2038 boundary
	mustContain(t, body, "leap-year-feb-29")
}

func TestPWATemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightPWA, sym, "https://x.test/")
	mustContain(t, body, "@kind:pwa")
	mustContain(t, body, `link[rel="manifest"]`)
	mustContain(t, body, "start_url")
	mustContain(t, body, "icons")
}

func TestHistoryDepthTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightHistoryDepth, sym, "https://x.test/")
	mustContain(t, body, "@kind:history-depth")
	mustContain(t, body, "goBack")
	mustContain(t, body, "goForward")
}
