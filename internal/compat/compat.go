// Package compat compares old-vs-new versions of schema files
// (OpenAPI, .proto, AsyncAPI) and surfaces backward-breaking changes.
//
// Each comparator returns a list of Regression entries — the
// generator embeds them verbatim in the emitted compatibility-test
// source so the assertion lives in the PR alongside the schema diff.
package compat

import (
	"strings"

	"github.com/spriteCloud/quail-core/asyncapi"
	rgrpc "github.com/spriteCloud/quail-core/grpc"
	"github.com/spriteCloud/quail-core/openapi"
)

// Regression is one breaking-change finding. Kind is the category
// ("endpoint-removed", "field-narrowed", "rpc-removed", …); Detail is
// the human-readable description.
type Regression struct {
	Kind   string
	Detail string
}

// OpenAPI compares two OpenAPI documents. Returns the list of
// regressions detected — empty when the diff is backward-compatible.
func OpenAPI(oldBody, newBody []byte) ([]Regression, error) {
	_, oldEPs, oldErr := openapi.Parse(oldBody)
	_, newEPs, newErr := openapi.Parse(newBody)
	if oldErr != nil {
		return nil, oldErr
	}
	if newErr != nil {
		return nil, newErr
	}
	// Endpoint set: (method, path) → declared statuses.
	type key struct{ m, p string }
	oldByKey := map[key][]string{}
	newByKey := map[key][]string{}
	for _, e := range oldEPs {
		oldByKey[key{e.Method, e.Path}] = e.Statuses
	}
	for _, e := range newEPs {
		newByKey[key{e.Method, e.Path}] = e.Statuses
	}
	var out []Regression
	for k, oldStatuses := range oldByKey {
		newStatuses, ok := newByKey[k]
		if !ok {
			out = append(out, Regression{
				Kind:   "endpoint-removed",
				Detail: strings.ToUpper(k.m) + " " + k.p,
			})
			continue
		}
		// Any 2xx status that existed in old but not in new is a break.
		oldSet := stringSet(oldStatuses)
		newSet := stringSet(newStatuses)
		for s := range oldSet {
			if !newSet[s] && strings.HasPrefix(s, "2") {
				out = append(out, Regression{
					Kind:   "status-removed",
					Detail: strings.ToUpper(k.m) + " " + k.p + " no longer returns " + s,
				})
			}
		}
	}
	return out, nil
}

// Proto compares two `.proto` files. Catches the wire-breaking
// changes the protobuf spec calls out: removed services / rpcs, and
// changes to streaming shape on a kept rpc.
func Proto(oldBody, newBody []byte) ([]Regression, error) {
	oldRPCs := rgrpc.Parse(oldBody)
	newRPCs := rgrpc.Parse(newBody)
	type key struct{ svc, rpc string }
	oldByKey := map[key]rgrpc.RPC{}
	newByKey := map[key]rgrpc.RPC{}
	for _, r := range oldRPCs {
		oldByKey[key{r.Service, r.Name}] = r
	}
	for _, r := range newRPCs {
		newByKey[key{r.Service, r.Name}] = r
	}
	var out []Regression
	for k, oldR := range oldByKey {
		newR, ok := newByKey[k]
		if !ok {
			out = append(out, Regression{
				Kind:   "rpc-removed",
				Detail: k.svc + "." + k.rpc,
			})
			continue
		}
		if oldR.Streaming != newR.Streaming {
			out = append(out, Regression{
				Kind:   "streaming-shape-changed",
				Detail: k.svc + "." + k.rpc + ": " + oldR.Streaming.String() + " → " + newR.Streaming.String(),
			})
		}
		if oldR.InputType != newR.InputType {
			out = append(out, Regression{
				Kind:   "rpc-input-type-changed",
				Detail: k.svc + "." + k.rpc + ": " + oldR.InputType + " → " + newR.InputType,
			})
		}
		if oldR.OutputType != newR.OutputType {
			out = append(out, Regression{
				Kind:   "rpc-output-type-changed",
				Detail: k.svc + "." + k.rpc + ": " + oldR.OutputType + " → " + newR.OutputType,
			})
		}
	}
	return out, nil
}

// AsyncAPI compares two AsyncAPI documents. Catches removed channels
// and changes in direction (publish ↔ subscribe).
func AsyncAPI(oldBody, newBody []byte) ([]Regression, error) {
	_, oldCh, oldErr := asyncapi.Parse(oldBody)
	_, newCh, newErr := asyncapi.Parse(newBody)
	if oldErr != nil {
		return nil, oldErr
	}
	if newErr != nil {
		return nil, newErr
	}
	type key struct{ ch, dir string }
	oldByKey := map[key]bool{}
	newByKey := map[key]bool{}
	for _, c := range oldCh {
		oldByKey[key{c.Path, c.Direction}] = true
	}
	for _, c := range newCh {
		newByKey[key{c.Path, c.Direction}] = true
	}
	var out []Regression
	for k := range oldByKey {
		if !newByKey[k] {
			out = append(out, Regression{
				Kind:   "channel-removed",
				Detail: k.ch + " (" + k.dir + ")",
			})
		}
	}
	return out, nil
}

func stringSet(ss []string) map[string]bool {
	out := make(map[string]bool, len(ss))
	for _, s := range ss {
		out[s] = true
	}
	return out
}
