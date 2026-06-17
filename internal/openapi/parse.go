// Package openapi parses OpenAPI 3.x documents enough to emit
// contract-test specs. Intentionally minimal — we don't validate the
// document, we just walk Paths × Methods × Responses to surface the
// (endpoint, declared status codes) pairs the contract template
// needs.
package openapi

import (
	"encoding/json"
	"fmt"
)

// Doc is the subset of an OpenAPI 3.x document we read.
type Doc struct {
	OpenAPI string                     `json:"openapi"`
	Swagger string                     `json:"swagger"`
	Info    Info                       `json:"info"`
	Servers []Server                   `json:"servers"`
	Paths   map[string]PathItem        `json:"paths"`
}

type Info struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

type Server struct {
	URL string `json:"url"`
}

// PathItem holds the per-method operations. We don't bother with
// parameters; the contract spec only asserts response shape.
type PathItem struct {
	Get     *Operation `json:"get,omitempty"`
	Post    *Operation `json:"post,omitempty"`
	Put     *Operation `json:"put,omitempty"`
	Patch   *Operation `json:"patch,omitempty"`
	Delete  *Operation `json:"delete,omitempty"`
	Options *Operation `json:"options,omitempty"`
	Head    *Operation `json:"head,omitempty"`
}

// Operation captures only the response status codes for now. Schema
// validation is a future enhancement — adding it requires an in-tree
// JSON Schema validator or a vendored ajv-style npm dep.
type Operation struct {
	OperationID string                 `json:"operationId"`
	Summary     string                 `json:"summary"`
	Responses   map[string]ResponseRef `json:"responses"`
}

type ResponseRef struct {
	Description string `json:"description"`
}

// Endpoint is the flattened (method, path, declared statuses) tuple
// the contract template renders against.
type Endpoint struct {
	Method      string
	Path        string
	OperationID string
	Summary     string
	Statuses    []string // declared response status codes (or "default")
}

// Parse decodes a JSON OpenAPI document and returns its flattened
// endpoints. Tolerant of swagger 2.x docs — for those, we still pull
// out paths × methods × responses. Schemas are ignored.
func Parse(body []byte) (*Doc, []Endpoint, error) {
	var d Doc
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, nil, fmt.Errorf("openapi: parse: %w", err)
	}
	if d.OpenAPI == "" && d.Swagger == "" {
		return &d, nil, fmt.Errorf("openapi: not an OpenAPI/Swagger document")
	}
	var endpoints []Endpoint
	for p, item := range d.Paths {
		for _, m := range []struct {
			method string
			op     *Operation
		}{
			{"get", item.Get},
			{"post", item.Post},
			{"put", item.Put},
			{"patch", item.Patch},
			{"delete", item.Delete},
			{"options", item.Options},
			{"head", item.Head},
		} {
			if m.op == nil {
				continue
			}
			statuses := make([]string, 0, len(m.op.Responses))
			for code := range m.op.Responses {
				statuses = append(statuses, code)
			}
			endpoints = append(endpoints, Endpoint{
				Method:      m.method,
				Path:        p,
				OperationID: m.op.OperationID,
				Summary:     m.op.Summary,
				Statuses:    statuses,
			})
		}
	}
	return &d, endpoints, nil
}
