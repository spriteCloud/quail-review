// Package proto is the ast.Extractor implementation for `.proto`
// files. Each rpc declaration becomes one ast.Symbol — Receiver is the
// containing service name, Name is the rpc name, FrameworkHint encodes
// the streaming shape so plan.pickTemplate can route to the right
// gRPC template.
package proto

import (
	"github.com/spriteCloud/quail-review/internal/ast"
	rgrpc "github.com/spriteCloud/quail-review/internal/grpc"
)

type extractor struct{}

func (extractor) Language() string { return "proto" }

// Extract walks the .proto file and returns one Symbol per rpc.
// FrameworkHint is set to "grpc-unary" / "grpc-server-stream" /
// "grpc-client-stream" / "grpc-bidi" so the plan layer routes the
// symbol to the matching template.
func (extractor) Extract(file string, content []byte) ([]ast.Symbol, []ast.LocatorAnchor) {
	rpcs := rgrpc.Parse(content)
	out := make([]ast.Symbol, 0, len(rpcs))
	for _, r := range rpcs {
		hint := "grpc-unary"
		switch r.Streaming {
		case rgrpc.ServerStream:
			hint = "grpc-server-stream"
		case rgrpc.ClientStream:
			hint = "grpc-client-stream"
		case rgrpc.Bidi:
			hint = "grpc-bidi"
		}
		out = append(out, ast.Symbol{
			Kind:          ast.KindMethod,
			Name:          r.Name,
			Receiver:      r.Service,
			File:          file,
			Language:      "ts",
			FrameworkHint: hint,
			Params: []ast.Param{
				{Name: "request", Type: r.InputType},
			},
			Returns: r.OutputType,
		})
	}
	return out, nil
}

func init() {
	ast.Register([]string{".proto"}, extractor{})
}
