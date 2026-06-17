// Package graphql parses a GraphQL introspection response enough to
// surface the (Query | Mutation) operations a contract test should
// exercise.
//
// We don't validate the schema — just walk types looking for the
// queryType / mutationType root and emit one Op per field with a
// best-effort sample argument set derived from the declared input
// types.
package graphql

import (
	"encoding/json"
	"fmt"
	"strings"
)

// IntrospectionQuery is the canonical GraphQL introspection query.
// Compact form — drops descriptions/directives that the contract test
// doesn't need.
const IntrospectionQuery = `query IntrospectionQuery {
  __schema {
    queryType { name }
    mutationType { name }
    types {
      kind
      name
      fields(includeDeprecated: false) {
        name
        args {
          name
          type { kind name ofType { kind name ofType { kind name } } }
        }
        type { kind name ofType { kind name } }
      }
    }
  }
}`

type schemaEnvelope struct {
	Data struct {
		Schema RawSchema `json:"__schema"`
	} `json:"data"`
	Errors []map[string]any `json:"errors,omitempty"`
}

// RawSchema is the subset we extract from an introspection response.
type RawSchema struct {
	QueryType    *TypeRef   `json:"queryType"`
	MutationType *TypeRef   `json:"mutationType"`
	Types        []TypeDecl `json:"types"`
}

type TypeRef struct {
	Kind   string   `json:"kind"`
	Name   string   `json:"name"`
	OfType *TypeRef `json:"ofType"`
}

type TypeDecl struct {
	Kind   string  `json:"kind"`
	Name   string  `json:"name"`
	Fields []Field `json:"fields"`
}

type Field struct {
	Name string  `json:"name"`
	Args []Arg   `json:"args"`
	Type TypeRef `json:"type"`
}

type Arg struct {
	Name string  `json:"name"`
	Type TypeRef `json:"type"`
}

// Op is the flattened (parent, field, args) tuple the contract template
// renders against.
type Op struct {
	Parent string // "Query" | "Mutation"
	Name   string
	Args   []Arg
}

// Parse decodes an introspection response and returns the flat operation
// list (queries first, then mutations).
func Parse(body []byte) ([]Op, error) {
	var env schemaEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("graphql: parse introspection: %w", err)
	}
	if len(env.Errors) > 0 {
		return nil, fmt.Errorf("graphql: introspection rejected: %v", env.Errors)
	}
	if env.Data.Schema.QueryType == nil {
		return nil, fmt.Errorf("graphql: response missing __schema.queryType")
	}
	byName := map[string]TypeDecl{}
	for _, t := range env.Data.Schema.Types {
		byName[t.Name] = t
	}
	var out []Op
	if q, ok := byName[env.Data.Schema.QueryType.Name]; ok {
		for _, f := range q.Fields {
			out = append(out, Op{Parent: "Query", Name: f.Name, Args: f.Args})
		}
	}
	if env.Data.Schema.MutationType != nil {
		if m, ok := byName[env.Data.Schema.MutationType.Name]; ok {
			for _, f := range m.Fields {
				out = append(out, Op{Parent: "Mutation", Name: f.Name, Args: f.Args})
			}
		}
	}
	return out, nil
}

// SampleArguments returns a best-effort placeholder argument list for
// an Op. Scalars get type-appropriate defaults; required complex types
// get `null` which makes the test fail loudly so the consumer fills it
// in. Returns a single-line GraphQL argument fragment, e.g.
//   id: "1", limit: 0
func SampleArguments(args []Arg) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, a.Name+": "+sampleValue(a.Type))
	}
	return strings.Join(parts, ", ")
}

func sampleValue(t TypeRef) string {
	// Unwrap NON_NULL / LIST to the innermost named scalar.
	cur := t
	for cur.Kind == "NON_NULL" || cur.Kind == "LIST" {
		if cur.OfType == nil {
			break
		}
		cur = *cur.OfType
	}
	switch cur.Name {
	case "Int", "Float":
		return "0"
	case "Boolean":
		return "false"
	case "String", "ID":
		return `"1"`
	}
	// INPUT_OBJECT / ENUM / unknown — emit null so the test surfaces
	// the missing argument rather than silently passing.
	return "null"
}
