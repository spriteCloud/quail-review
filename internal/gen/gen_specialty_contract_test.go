package gen

import (
	"strings"
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
)

func TestVisualTemplate_ThreeViewportBaselines(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://x.test/",
		Template: plan.TmplPlaywrightVisual,
		OutPath:  "tests/e2e/visual/x.visual.spec.ts",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "@kind:visual @smoke")
	mustContain(t, body, "toHaveScreenshot")
	mustContain(t, body, "mobile")
	mustContain(t, body, "tablet")
	mustContain(t, body, "desktop")
	mustContain(t, body, "animations: 'disabled'")
	mustContain(t, body, "maxDiffPixelRatio: 0.01")
}

func TestGraphQLTemplate_PerOperationTest(t *testing.T) {
	sym := ast.Symbol{
		Name: "X", Kind: ast.KindComponent, File: "https://x.test/graphql", Language: "ts",
		Anchors: []ast.LocatorAnchor{
			{Tag: "Query", Name: "user", CSS: `id: "1"`},
			{Tag: "Mutation", Name: "createUser", CSS: `email: "1"`},
		},
	}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://x.test/graphql",
		Template: plan.TmplPlaywrightGraphQL,
		OutPath:  "tests/e2e/contract/x-graphql.contract.spec.ts",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, `@kind:graphql @smoke Query.user`)
	mustContain(t, body, `@kind:graphql @smoke Mutation.createUser`)
	mustContain(t, body, `query { user(id: "1") { __typename } }`)
	mustContain(t, body, `mutation { createUser(email: "1") { __typename } }`)
}

func TestWebhookTemplate_HappyPlusNegativePerEndpoint(t *testing.T) {
	sym := ast.Symbol{
		Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts",
		Anchors: []ast.LocatorAnchor{
			{Name: "/webhooks/stripe", CSS: "https://x.test/webhooks/stripe"},
			{Name: "/webhooks/github", CSS: "https://x.test/webhooks/github"},
		},
	}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://x.test/",
		Template: plan.TmplPlaywrightWebhook,
		OutPath:  "tests/e2e/webhooks/x.webhook.spec.ts",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	for _, want := range []string{
		`@kind:webhook @negative /webhooks/stripe rejects unsigned payload`,
		`@kind:webhook @smoke /webhooks/stripe accepts a signed payload`,
		`@kind:webhook @negative /webhooks/github rejects unsigned payload`,
		`@kind:webhook @smoke /webhooks/github accepts a signed payload`,
		`X-Hub-Signature-256`,
		`Stripe-Signature`,
		`WEBHOOK_SECRET unset`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("webhook spec missing %q", want)
		}
	}
}

func TestGRPCTemplates_RouteByFrameworkHint(t *testing.T) {
	for _, c := range []struct {
		hint     string
		tmpl     plan.Template
		expectIn string
	}{
		{"grpc-unary", plan.TmplGRPCUnary, "unary @kind:grpc"},
		{"grpc-server-stream", plan.TmplGRPCServerStream, "server-streaming @kind:grpc"},
		{"grpc-client-stream", plan.TmplGRPCClientStream, "client-streaming @kind:grpc"},
		{"grpc-bidi", plan.TmplGRPCBidi, "bidi @kind:grpc"},
	} {
		t.Run(c.hint, func(t *testing.T) {
			sym := ast.Symbol{
				Kind: ast.KindMethod, Name: "GetUser", Receiver: "UserService",
				Language: "ts", FrameworkHint: c.hint,
			}
			it := plan.Item{
				Symbol:   sym,
				Symbols:  []ast.Symbol{sym},
				Template: c.tmpl,
				OutPath:  "tests/grpc/userservice.getuser.test.ts",
			}
			out, err := Render([]plan.Item{it}, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			body := string(out[0].Content)
			mustContain(t, body, c.expectIn)
			mustContain(t, body, "UserServiceClient")
			mustContain(t, body, "GRPC_TARGET env var is required")
		})
	}
}
