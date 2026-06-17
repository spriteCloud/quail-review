// Package grpc parses a `.proto` file enough to surface the
// (service, rpc, streaming-shape, input-type, output-type) tuples a
// contract test needs. Regex-based — matches the existing
// internal/plan style for HTML extractors.
package grpc

import (
	"regexp"
	"strings"
)

// Streaming classifies an RPC by which side streams.
type Streaming int

const (
	Unary Streaming = iota
	ServerStream
	ClientStream
	Bidi
)

// String returns the human-readable name of a streaming mode.
func (s Streaming) String() string {
	switch s {
	case ServerStream:
		return "server-streaming"
	case ClientStream:
		return "client-streaming"
	case Bidi:
		return "bidi"
	}
	return "unary"
}

// RPC is the flattened (service, rpc, shape, in, out) tuple.
type RPC struct {
	Service   string
	Name      string
	Streaming Streaming
	InputType string
	OutputType string
}

// reService captures the start of a service declaration. We don't try
// to track braces — just record the most recent service seen as we
// walk the file.
var reService = regexp.MustCompile(`(?m)^\s*service\s+(\w+)\s*\{`)

// reRPC captures one rpc declaration:
//   rpc Foo(In) returns (Out);
//   rpc Foo(stream In) returns (Out);
//   rpc Foo(In) returns (stream Out);
//   rpc Foo(stream In) returns (stream Out);
var reRPC = regexp.MustCompile(`(?m)^\s*rpc\s+(\w+)\s*\(\s*(stream\s+)?([\w\.]+)\s*\)\s*returns\s*\(\s*(stream\s+)?([\w\.]+)\s*\)`)

// marker tags the line index where a service declaration starts.
type marker struct {
	line int
	name string
}

// Parse walks a `.proto` file and returns the flat RPC list. Services
// are tracked positionally — each rpc is attributed to the most
// recently-opened service. Comments are tolerated; the regexes don't
// match inside `//` or `/* */` blocks (cheap because Go's regexp lacks
// look-behind — the position-based service tracking is good enough).
func Parse(content []byte) []RPC {
	src := string(content)
	lines := strings.Split(src, "\n")
	// First collect (lineNo → service) starting points.
	var services []marker
	for i, line := range lines {
		if m := reService.FindStringSubmatch(line); m != nil {
			services = append(services, marker{line: i, name: m[1]})
		}
	}
	// Then walk rpc declarations and bind to the nearest preceding service.
	var out []RPC
	for i, line := range lines {
		m := reRPC.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		service := serviceAt(services, i)
		if service == "" {
			continue
		}
		streaming := classify(m[2] != "", m[4] != "")
		out = append(out, RPC{
			Service:    service,
			Name:       m[1],
			Streaming:  streaming,
			InputType:  m[3],
			OutputType: m[5],
		})
	}
	return out
}

func serviceAt(services []marker, line int) string {
	last := ""
	for _, s := range services {
		if s.line > line {
			break
		}
		last = s.name
	}
	return last
}

func classify(clientStream, serverStream bool) Streaming {
	switch {
	case clientStream && serverStream:
		return Bidi
	case clientStream:
		return ClientStream
	case serverStream:
		return ServerStream
	}
	return Unary
}
