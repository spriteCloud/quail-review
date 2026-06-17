package graphql

import "testing"

func TestParse_FlattensQueriesAndMutations(t *testing.T) {
	body := []byte(`{
		"data": {
			"__schema": {
				"queryType": {"name": "Query"},
				"mutationType": {"name": "Mutation"},
				"types": [
					{"kind": "OBJECT", "name": "Query", "fields": [
						{"name": "user", "args": [
							{"name": "id", "type": {"kind": "NON_NULL", "ofType": {"kind": "SCALAR", "name": "ID"}}}
						], "type": {"kind": "OBJECT", "name": "User"}},
						{"name": "users", "args": [], "type": {"kind": "LIST", "ofType": {"kind": "OBJECT", "name": "User"}}}
					]},
					{"kind": "OBJECT", "name": "Mutation", "fields": [
						{"name": "createUser", "args": [
							{"name": "email", "type": {"kind": "NON_NULL", "ofType": {"kind": "SCALAR", "name": "String"}}}
						], "type": {"kind": "OBJECT", "name": "User"}}
					]}
				]
			}
		}
	}`)
	ops, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(ops) != 3 {
		t.Fatalf("expected 3 ops; got %d (%+v)", len(ops), ops)
	}
	if ops[0].Parent != "Query" || ops[0].Name != "user" {
		t.Errorf("ops[0] = %+v; want Query.user", ops[0])
	}
	if ops[2].Parent != "Mutation" || ops[2].Name != "createUser" {
		t.Errorf("ops[2] = %+v; want Mutation.createUser", ops[2])
	}
}

func TestSampleArguments_TypeDefaults(t *testing.T) {
	args := []Arg{
		{Name: "id", Type: TypeRef{Kind: "NON_NULL", OfType: &TypeRef{Kind: "SCALAR", Name: "ID"}}},
		{Name: "limit", Type: TypeRef{Kind: "SCALAR", Name: "Int"}},
		{Name: "active", Type: TypeRef{Kind: "SCALAR", Name: "Boolean"}},
		{Name: "filter", Type: TypeRef{Kind: "INPUT_OBJECT", Name: "Filter"}},
	}
	got := SampleArguments(args)
	want := `id: "1", limit: 0, active: false, filter: null`
	if got != want {
		t.Errorf("SampleArguments = %q; want %q", got, want)
	}
}

func TestParse_RejectsResponseWithErrors(t *testing.T) {
	body := []byte(`{"errors": [{"message": "introspection disabled"}], "data": null}`)
	if _, err := Parse(body); err == nil {
		t.Error("expected error when response carries errors[]")
	}
}
