package gen

import (
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
)

func TestGraphQLStubTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightGraphQLStub, sym, "https://x.test/")
	mustContain(t, body, "@kind:graphql-stub")
	mustContain(t, body, "__schema")
	mustContain(t, body, "REVIEWQA_GRAPHQL_ENDPOINT")
	mustContain(t, body, "test.skip(true")
}

func TestWebhookStubTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightWebhookStub, sym, "https://x.test/")
	mustContain(t, body, "@kind:webhook-stub")
	mustContain(t, body, "X-Hub-Signature-256")
	mustContain(t, body, "Stripe-Signature")
	mustContain(t, body, "createHmac")
	mustContain(t, body, "REVIEWQA_WEBHOOK_SECRET")
}
