package integration

import (
	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/plan"
)

// EmitItems materializes one plan.Item per declared resource in the
// config, plus the shared _containers.ts and docker-compose.test.yml.
// Returns an empty slice when the config is nil or empty.
func EmitItems(cfg *Config) []plan.Item {
	if cfg.IsEmpty() {
		return nil
	}
	var items []plan.Item
	containers := &plan.IntegrationContainers{}
	for _, db := range cfg.Databases {
		d := db
		containers.Databases = append(containers.Databases, plan.IntegrationDB{Name: d.Name, Driver: d.Driver, Image: d.Image, Migrations: d.Migrations})
		items = append(items, plan.Item{
			Symbol:   stub("db-" + d.Name),
			Template: plan.TmplIntegrationDB,
			OutPath:  "tests/integration/db-" + d.Name + ".integration.test.ts",
			Integration: &plan.IntegrationCtx{
				Database: &plan.IntegrationDB{Name: d.Name, Driver: d.Driver, Image: d.Image, Migrations: d.Migrations},
			},
		})
	}
	for _, br := range cfg.Brokers {
		b := br
		containers.Brokers = append(containers.Brokers, plan.IntegrationBroker{Kind: b.Kind, Image: b.Image, Topics: b.Topics})
		items = append(items, plan.Item{
			Symbol:   stub("broker-" + b.Kind),
			Template: plan.TmplIntegrationBroker,
			OutPath:  "tests/integration/broker-" + b.Kind + ".integration.test.ts",
			Integration: &plan.IntegrationCtx{
				Broker: &plan.IntegrationBroker{Kind: b.Kind, Image: b.Image, Topics: b.Topics},
			},
		})
	}
	for _, ch := range cfg.Caches {
		c := ch
		containers.Caches = append(containers.Caches, plan.IntegrationCache{Kind: c.Kind, Image: c.Image})
		items = append(items, plan.Item{
			Symbol:   stub("cache-" + c.Kind),
			Template: plan.TmplIntegrationCache,
			OutPath:  "tests/integration/cache-" + c.Kind + ".integration.test.ts",
			Integration: &plan.IntegrationCtx{
				Cache: &plan.IntegrationCache{Kind: c.Kind, Image: c.Image},
			},
		})
	}
	for _, st := range cfg.Storage {
		s := st
		containers.Storage = append(containers.Storage, plan.IntegrationStorage{Kind: s.Kind, Bucket: s.Bucket})
		items = append(items, plan.Item{
			Symbol:   stub("storage-" + s.Kind),
			Template: plan.TmplIntegrationStorage,
			OutPath:  "tests/integration/storage-" + s.Kind + ".integration.test.ts",
			Integration: &plan.IntegrationCtx{
				Storage: &plan.IntegrationStorage{Kind: s.Kind, Bucket: s.Bucket},
			},
		})
	}
	for _, sr := range cfg.Search {
		s := sr
		containers.Search = append(containers.Search, plan.IntegrationSearch{Kind: s.Kind})
		items = append(items, plan.Item{
			Symbol:   stub("search-" + s.Kind),
			Template: plan.TmplIntegrationSearch,
			OutPath:  "tests/integration/search-" + s.Kind + ".integration.test.ts",
			Integration: &plan.IntegrationCtx{
				Search: &plan.IntegrationSearch{Kind: s.Kind},
			},
		})
	}
	for _, au := range cfg.Auth {
		a := au
		containers.Auth = append(containers.Auth, plan.IntegrationAuth{Provider: a.Provider, Issuer: a.Issuer})
		items = append(items, plan.Item{
			Symbol:   stub("auth-" + a.Provider),
			Template: plan.TmplIntegrationAuth,
			OutPath:  "tests/integration/auth-" + a.Provider + ".integration.test.ts",
			Integration: &plan.IntegrationCtx{
				Auth: &plan.IntegrationAuth{Provider: a.Provider, Issuer: a.Issuer},
			},
		})
	}
	// Shared companion files.
	items = append(items, plan.Item{
		Symbol:      stub("_containers"),
		Template:    plan.TmplIntegrationContainers,
		OutPath:     "tests/integration/_containers.ts",
		Integration: &plan.IntegrationCtx{Containers: containers},
	})
	items = append(items, plan.Item{
		Symbol:        stub("compose"),
		Template:      plan.TmplIntegrationCompose,
		OutPath:       "docker-compose.test.yml",
		Integration:   &plan.IntegrationCtx{Containers: containers},
		IfMissingOnly: true, // don't clobber a hand-edited compose file
	})
	return items
}

func stub(name string) ast.Symbol {
	return ast.Symbol{Name: name, Kind: ast.KindFunction, Language: "ts", File: "quail.yml"}
}
