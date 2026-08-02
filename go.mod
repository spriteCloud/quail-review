module github.com/spriteCloud/quail-review

go 1.25.10

require (
	github.com/spf13/cobra v1.10.2
	github.com/spriteCloud/quail-core v0.30.2
)

// Development-only: point at the sibling quail-core checkout's
// agentic-explore branch instead of the pinned release, so both repos'
// matching branches build/test together. Remove once quail-core cuts
// a release off that branch and go.mod is bumped to it.
replace github.com/spriteCloud/quail-core => ../quail-core

require (
	github.com/google/go-github/v66 v66.0.0 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
