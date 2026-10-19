package config

import (
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("APP_MODE", "")
	t.Setenv("PORT", "")
	t.Setenv("RETAIL_ORDERS_PERSISTENCE_PROVIDER", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DB_HOST", "")
	t.Setenv("RETAIL_ORDERS_PERSISTENCE_ENDPOINT", "")
	t.Setenv("SQS_QUEUE_URL", "")
	t.Setenv("SQS_MAX_MESSAGES", "")
	t.Setenv("WORKER_CONCURRENCY", "")

	cfg := LoadConfig()

	if cfg.AppMode != "api" {
		t.Errorf("expected AppMode 'api', got %q", cfg.AppMode)
	}
	if cfg.Port != "8080" {
		t.Errorf("expected Port '8080', got %q", cfg.Port)
	}
	if cfg.DBProvider != "in-memory" {
		t.Errorf("expected DBProvider 'in-memory', got %q", cfg.DBProvider)
	}
	if cfg.SQSMaxMessages != 1 {
		t.Errorf("expected SQSMaxMessages 1, got %d", cfg.SQSMaxMessages)
	}
	if cfg.WorkerConcurrency != 1 {
		t.Errorf("expected WorkerConcurrency 1, got %d", cfg.WorkerConcurrency)
	}
	if cfg.QueueName != "orders-queue" {
		t.Errorf("expected QueueName 'orders-queue', got %q", cfg.QueueName)
	}
	if cfg.ServiceName != "orders" {
		t.Errorf("expected ServiceName 'orders', got %q", cfg.ServiceName)
	}
	if cfg.PodName == "" {
		t.Errorf("expected non-empty PodName, got %q", cfg.PodName)
	}
}

func TestLoadConfig_DBProviderResolution(t *testing.T) {
	tests := []struct {
		name             string
		providerEnv      string
		databaseURL      string
		dbHost           string
		endpointEnv      string
		expectedProvider string
	}{
		{
			name:             "Explicit provider takes precedence",
			providerEnv:      "custom-provider",
			databaseURL:      "postgres://localhost",
			expectedProvider: "custom-provider",
		},
		{
			name:             "DATABASE_URL auto-resolves to postgres",
			providerEnv:      "",
			databaseURL:      "postgres://usr:pass@localhost:5432/db",
			expectedProvider: "postgres",
		},
		{
			name:             "DB_HOST auto-resolves to postgres",
			providerEnv:      "",
			dbHost:           "localhost",
			expectedProvider: "postgres",
		},
		{
			name:             "RETAIL_ORDERS_PERSISTENCE_ENDPOINT auto-resolves to postgres",
			providerEnv:      "",
			endpointEnv:      "rds.amazonaws.com",
			expectedProvider: "postgres",
		},
		{
			name:             "No DB settings defaults to in-memory",
			providerEnv:      "",
			expectedProvider: "in-memory",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("RETAIL_ORDERS_PERSISTENCE_PROVIDER", tc.providerEnv)
			t.Setenv("DATABASE_URL", tc.databaseURL)
			t.Setenv("DB_HOST", tc.dbHost)
			t.Setenv("RETAIL_ORDERS_PERSISTENCE_ENDPOINT", tc.endpointEnv)

			cfg := LoadConfig()
			if cfg.DBProvider != tc.expectedProvider {
				t.Errorf("expected DBProvider %q, got %q", tc.expectedProvider, cfg.DBProvider)
			}
		})
	}
}

func TestLoadConfig_SQSMaxMessagesClamping(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"Negative clamped to 1", "-5", 1},
		{"Zero clamped to 1", "0", 1},
		{"Valid middle value", "5", 5},
		{"Upper bound 10", "10", 10},
		{"Exceeding upper bound clamped to 10", "25", 10},
		{"Invalid integer defaults to 1", "abc", 1},
		{"Empty defaults to 1", "", 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SQS_MAX_MESSAGES", tc.input)
			cfg := LoadConfig()
			if cfg.SQSMaxMessages != tc.expected {
				t.Errorf("for input %q: expected SQSMaxMessages %d, got %d", tc.input, tc.expected, cfg.SQSMaxMessages)
			}
		})
	}
}

func TestLoadConfig_WorkerConcurrency(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"Zero clamped to 1", "0", 1},
		{"Negative clamped to 1", "-3", 1},
		{"Valid concurrency 4", "4", 4},
		{"Invalid integer defaults to 1", "xyz", 1},
		{"Empty defaults to 1", "", 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WORKER_CONCURRENCY", tc.input)
			cfg := LoadConfig()
			if cfg.WorkerConcurrency != tc.expected {
				t.Errorf("for input %q: expected WorkerConcurrency %d, got %d", tc.input, tc.expected, cfg.WorkerConcurrency)
			}
		})
	}
}

func TestLoadConfig_SQSQueueURLParsing(t *testing.T) {
	tests := []struct {
		name          string
		queueURL      string
		expectedQueue string
	}{
		{
			name:          "Standard LocalStack SQS URL",
			queueURL:      "http://localhost:4566/000000000000/custom-orders-q",
			expectedQueue: "custom-orders-q",
		},
		{
			name:          "AWS SQS URL with trailing slash",
			queueURL:      "https://sqs.us-east-1.amazonaws.com/123456789012/my-orders-queue/",
			expectedQueue: "my-orders-queue",
		},
		{
			name:          "Empty queue URL falls back to orders-queue",
			queueURL:      "",
			expectedQueue: "orders-queue",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SQS_QUEUE_URL", tc.queueURL)
			cfg := LoadConfig()
			if cfg.QueueName != tc.expectedQueue {
				t.Errorf("for SQS_QUEUE_URL %q: expected QueueName %q, got %q", tc.queueURL, tc.expectedQueue, cfg.QueueName)
			}
		})
	}
}

func TestPostgresConnString(t *testing.T) {
	t.Run("DatabaseURL is set", func(t *testing.T) {
		cfg := &Config{
			DatabaseURL: "postgres://user:secret@custom-host:5432/mydb?sslmode=require",
		}
		expected := "postgres://user:secret@custom-host:5432/mydb?sslmode=require"
		if got := cfg.PostgresConnString(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("DBHost is localhost", func(t *testing.T) {
		cfg := &Config{
			DBHost:     "localhost",
			DBPort:     "5432",
			DBUser:     "postgres",
			DBPassword: "password123",
			DBName:     "retaildb",
			DBSSLMode:  "disable",
		}
		expected := "host=localhost port=5432 user=postgres password=password123 dbname=retaildb sslmode=disable"
		if got := cfg.PostgresConnString(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("DBHost contains host:port dbhost:5433", func(t *testing.T) {
		cfg := &Config{
			DBHost:     "dbhost:5433",
			DBPort:     "5432",
			DBUser:     "postgres",
			DBPassword: "password123",
			DBName:     "retaildb",
			DBSSLMode:  "disable",
		}
		expected := "host=dbhost port=5433 user=postgres password=password123 dbname=retaildb sslmode=disable"
		if got := cfg.PostgresConnString(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})
}
