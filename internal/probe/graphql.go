package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-core/graphql"
	"github.com/spriteCloud/quail-core/log"
	"github.com/spriteCloud/quail-review/internal/plan"
)

// graphQLContractItems probes for a GraphQL endpoint at the origin and,
// when one responds to the introspection query, emits a contract spec
// per top-level Query / Mutation. Capped to keep probe cost bounded.
func graphQLContractItems(ctx context.Context, sourceURL string, projectLabel string) []plan.Item {
	const cap = 16
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed == nil {
		return nil
	}
	origin := parsed.Scheme + "://" + parsed.Host
	candidates := []string{"/graphql", "/api/graphql", "/v1/graphql"}
	var rawSchema []byte
	var endpoint string
	for _, p := range candidates {
		target := origin + p
		if err := guardHost(parsed.Hostname()); err != nil {
			return nil
		}
		body, ok := postIntrospection(ctx, target)
		if !ok {
			continue
		}
		// Quick sniff: must contain "__schema".
		if !bytes.Contains(body, []byte("__schema")) {
			continue
		}
		rawSchema = body
		endpoint = target
		break
	}
	if rawSchema == nil {
		return nil
	}
	ops, err := graphql.Parse(rawSchema)
	if err != nil {
		log.Warn("graphql: parse introspection failed", "endpoint", endpoint, "err", err)
		return nil
	}
	if len(ops) > cap {
		log.Info("graphql: capping ops", "discovered", len(ops), "cap", cap)
		ops = ops[:cap]
	}
	host := parsed.Hostname()

	// Pack ops into the Symbol.Anchors slice — the template uses
	// (Tag, Name, CSS) for (Parent, OpName, SampleArgs).
	anchors := make([]ast.LocatorAnchor, 0, len(ops))
	for _, op := range ops {
		anchors = append(anchors, ast.LocatorAnchor{
			Tag:  op.Parent,
			Name: op.Name,
			CSS:  graphql.SampleArguments(op.Args),
		})
	}
	stub := ast.Symbol{
		Name:     projectName(projectLabel, host),
		Kind:     ast.KindComponent,
		File:     endpoint,
		Language: "ts",
		Anchors:  anchors,
	}
	return []plan.Item{{
		Symbol:   stub,
		Symbols:  []ast.Symbol{stub},
		PageURL:  endpoint,
		Template: plan.TmplPlaywrightGraphQL,
		OutPath:  "tests/e2e/contract/graphql.contract.spec.ts",
	}}
}

// postIntrospection sends the introspection query to the candidate URL.
// Tight timeout — GraphQL endpoints either respond fast or aren't one.
func postIntrospection(ctx context.Context, target string) ([]byte, bool) {
	u, err := url.Parse(target)
	if err != nil || u == nil {
		return nil, false
	}
	body, _ := json.Marshal(map[string]string{"query": graphql.IntrospectionQuery})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, false
	}
	const maxBody = 2 << 20 // 2 MiB
	buf := bytes.NewBuffer(nil)
	if _, err := buf.ReadFrom(http.MaxBytesReader(nil, resp.Body, maxBody)); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}
