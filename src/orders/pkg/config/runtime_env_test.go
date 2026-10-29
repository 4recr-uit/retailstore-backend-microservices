package config

import (
	"net/url"
	"testing"
)

func TestPostgresRuntimeCredentialsAndTLS(t *testing.T) {
	cfg := Config{DBHost: "postgres.internal", DBPort: "5432", DBName: "retaildb", DBUser: "user@example", DBPassword: "p a'ss:/?&", DBSSLMode: "verify-full", DBSSLRootCert: "/run/certs/db ca.pem", DBConnectTimeout: 7}
	parsed, err := url.Parse(cfg.PostgresConnString())
	if err != nil {
		t.Fatal(err)
	}
	password, _ := parsed.User.Password()
	if password != cfg.DBPassword || parsed.User.Username() != cfg.DBUser {
		t.Fatal("database credentials were corrupted")
	}
	if parsed.Query().Get("sslmode") != "verify-full" || parsed.Query().Get("sslrootcert") != cfg.DBSSLRootCert || parsed.Query().Get("connect_timeout") != "7" {
		t.Fatalf("missing runtime TLS configuration: %v", parsed.Query())
	}
}
