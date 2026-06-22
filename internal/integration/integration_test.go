package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestLoad_MissingFileReturnsNil(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil || cfg != nil {
		t.Errorf("expected (nil, nil); got (%+v, %v)", cfg, err)
	}
}

func TestLoad_ParsesSampleConfig(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`
databases:
  - name: app-db
    driver: postgres
    image: postgres:16-alpine
    migrations: ./migrations
brokers:
  - kind: kafka
    image: confluentinc/cp-kafka:latest
    topics: [orders, payments]
caches:
  - kind: redis
    image: redis:7-alpine
`)
	if err := os.WriteFile(filepath.Join(dir, "quail.yml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || len(cfg.Databases) != 1 || cfg.Databases[0].Driver != "postgres" {
		t.Errorf("unexpected config: %+v", cfg)
	}
	if len(cfg.Brokers) != 1 || cfg.Brokers[0].Kind != "kafka" || len(cfg.Brokers[0].Topics) != 2 {
		t.Errorf("unexpected brokers: %+v", cfg.Brokers)
	}
}

func TestEmitItems_OnePerResourcePlusCompanions(t *testing.T) {
	cfg := &Config{
		Databases: []Database{{Name: "app", Driver: "postgres", Image: "postgres:16"}},
		Brokers:   []Broker{{Kind: "kafka", Image: "k:1", Topics: []string{"t"}}},
		Caches:    []Cache{{Kind: "redis", Image: "redis:7"}},
	}
	items := EmitItems(cfg)
	// 3 resources + 2 companions (_containers.ts, docker-compose.test.yml).
	if len(items) != 5 {
		t.Fatalf("expected 5 items; got %d", len(items))
	}
	gotKinds := map[plan.Template]int{}
	for _, it := range items {
		gotKinds[it.Template]++
	}
	for _, want := range []plan.Template{
		plan.TmplIntegrationDB,
		plan.TmplIntegrationBroker,
		plan.TmplIntegrationCache,
		plan.TmplIntegrationContainers,
		plan.TmplIntegrationCompose,
	} {
		if gotKinds[want] == 0 {
			t.Errorf("missing template %s in output", want)
		}
	}
	// The compose file should land at the repo root.
	for _, it := range items {
		if it.Template == plan.TmplIntegrationCompose && !strings.HasSuffix(it.OutPath, "docker-compose.test.yml") {
			t.Errorf("compose OutPath unexpected: %s", it.OutPath)
		}
	}
}

func TestEmitItems_EmptyConfigReturnsNothing(t *testing.T) {
	items := EmitItems(&Config{})
	if items != nil {
		t.Errorf("expected nil; got %+v", items)
	}
}
