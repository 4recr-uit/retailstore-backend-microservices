package config

import (
	"context"
	"github.com/sethvargo/go-envconfig/pkg/envconfig"
	"testing"
)

func TestRuntimeEnvironmentSettings(t *testing.T) {
	values := map[string]string{"RETAIL_CATALOG_PERSISTENCE_PROVIDER": "mysql", "RETAIL_CATALOG_PERSISTENCE_ENDPOINT": "mysql.internal:3307", "RETAIL_CATALOG_PERSISTENCE_USER": "catalog_runtime", "RETAIL_CATALOG_PERSISTENCE_PASSWORD": "placeholder", "RETAIL_CATALOG_PERSISTENCE_TLS": "true", "DB_MAX_OPEN_CONNS": "7", "DB_MAX_IDLE_CONNS": "2", "DB_CONN_MAX_LIFETIME_SECONDS": "60", "DB_CONNECT_ATTEMPTS": "2", "DB_CONNECT_RETRY_SECONDS": "1", "HTTP_SHUTDOWN_TIMEOUT_SECONDS": "4"}
	for key, value := range values {
		t.Setenv(key, value)
	}
	var cfg AppConfiguration
	if err := envconfig.Process(context.Background(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Endpoint != "mysql.internal:3307" || !cfg.Database.TLS || cfg.Database.MaxOpenConns != 7 || cfg.Database.MaxIdleConns != 2 || cfg.Database.ConnMaxLifetimeSeconds != 60 || cfg.Database.ConnectAttempts != 2 || cfg.Database.ConnectRetrySeconds != 1 || cfg.ShutdownTimeoutSeconds != 4 {
		t.Fatalf("runtime configuration was not applied: endpoint=%s max_open=%d", cfg.Database.Endpoint, cfg.Database.MaxOpenConns)
	}
}
