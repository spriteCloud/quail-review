package proto

import (
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
)

func TestExtract_RoutesByStreamingShape(t *testing.T) {
	e := ast.ForFile("foo.proto")
	if e == nil {
		t.Fatal("no extractor registered for .proto")
	}
	syms, _ := e.Extract("foo.proto", []byte(`
service Pets {
  rpc GetPet(Req) returns (Resp);
  rpc ListPets(Req) returns (stream Resp);
  rpc Upload(stream Req) returns (Resp);
  rpc Chat(stream Req) returns (stream Resp);
}
`))
	if len(syms) != 4 {
		t.Fatalf("expected 4 symbols; got %d", len(syms))
	}
	hints := []string{
		syms[0].FrameworkHint, syms[1].FrameworkHint,
		syms[2].FrameworkHint, syms[3].FrameworkHint,
	}
	want := []string{"grpc-unary", "grpc-server-stream", "grpc-client-stream", "grpc-bidi"}
	for i := range hints {
		if hints[i] != want[i] {
			t.Errorf("syms[%d].FrameworkHint = %s; want %s", i, hints[i], want[i])
		}
	}
	if syms[0].Receiver != "Pets" {
		t.Errorf("receiver should be the service name; got %s", syms[0].Receiver)
	}
}
