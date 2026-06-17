package grpc

import "testing"

func TestParse_ClassifiesAllStreamingShapes(t *testing.T) {
	proto := []byte(`
syntax = "proto3";
package petstore;

service Pets {
  rpc GetPet(GetPetReq) returns (Pet);
  rpc ListPets(ListPetsReq) returns (stream Pet);
  rpc CreatePets(stream Pet) returns (Created);
  rpc Chat(stream Msg) returns (stream Msg);
}

service Users {
  rpc GetUser(GetUserReq) returns (User);
}
`)
	got := Parse(proto)
	if len(got) != 5 {
		t.Fatalf("expected 5 RPCs; got %d (%+v)", len(got), got)
	}
	want := []RPC{
		{Service: "Pets", Name: "GetPet", Streaming: Unary, InputType: "GetPetReq", OutputType: "Pet"},
		{Service: "Pets", Name: "ListPets", Streaming: ServerStream, InputType: "ListPetsReq", OutputType: "Pet"},
		{Service: "Pets", Name: "CreatePets", Streaming: ClientStream, InputType: "Pet", OutputType: "Created"},
		{Service: "Pets", Name: "Chat", Streaming: Bidi, InputType: "Msg", OutputType: "Msg"},
		{Service: "Users", Name: "GetUser", Streaming: Unary, InputType: "GetUserReq", OutputType: "User"},
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("RPC[%d] = %+v; want %+v", i, got[i], w)
		}
	}
}

func TestStreamingString(t *testing.T) {
	cases := map[Streaming]string{
		Unary:        "unary",
		ServerStream: "server-streaming",
		ClientStream: "client-streaming",
		Bidi:         "bidi",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("%d.String() = %q; want %q", s, got, want)
		}
	}
}

func TestParse_TolerantOfComments(t *testing.T) {
	proto := []byte(`
service Demo {
  // GetThing is unary.
  rpc GetThing(In) returns (Out);
  /* multi
     line */
  rpc StreamThings(In) returns (stream Out);
}
`)
	got := Parse(proto)
	if len(got) != 2 {
		t.Fatalf("expected 2 RPCs; got %d", len(got))
	}
}
