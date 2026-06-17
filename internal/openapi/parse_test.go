package openapi

import "testing"

func TestParse_FlattensPathsMethodsResponses(t *testing.T) {
	doc := []byte(`{
		"openapi": "3.0.0",
		"info": {"title": "Pet Store", "version": "1.0"},
		"paths": {
			"/pets": {
				"get": {
					"operationId": "listPets",
					"summary": "List all pets",
					"responses": {"200": {"description": "ok"}, "400": {"description": "bad"}}
				},
				"post": {
					"operationId": "createPet",
					"responses": {"201": {"description": "created"}}
				}
			},
			"/pets/{id}": {
				"get": {
					"operationId": "getPet",
					"responses": {"200": {"description": "ok"}, "404": {"description": "missing"}}
				}
			}
		}
	}`)
	_, eps, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(eps) != 3 {
		t.Fatalf("expected 3 endpoints; got %d (%+v)", len(eps), eps)
	}
	gotMethods := map[string]bool{}
	for _, e := range eps {
		gotMethods[e.Method+" "+e.Path] = true
	}
	for _, want := range []string{"get /pets", "post /pets", "get /pets/{id}"} {
		if !gotMethods[want] {
			t.Errorf("missing endpoint %q in %+v", want, gotMethods)
		}
	}
}

func TestParse_RejectsNonOpenAPIDoc(t *testing.T) {
	_, _, err := Parse([]byte(`{"foo":"bar"}`))
	if err == nil {
		t.Error("expected error for non-OpenAPI doc")
	}
}

func TestParse_Swagger2xStillFlattens(t *testing.T) {
	doc := []byte(`{
		"swagger": "2.0",
		"info": {"title": "Old API", "version": "1.0"},
		"paths": {
			"/v1/users": {
				"get": { "responses": {"200": {"description": "ok"}} }
			}
		}
	}`)
	_, eps, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(eps) != 1 || eps[0].Method != "get" || eps[0].Path != "/v1/users" {
		t.Errorf("expected single GET /v1/users endpoint; got %+v", eps)
	}
}
