package db

import (
	"fmt"
	"log"

	"github.com/aws-containers/retail-store-sample-app/orders/pkg/config"
)

func InitRepository(cfg *config.Config) (Repository, error) {
	if cfg.DBProvider == "postgres" {
		if cfg.DatabaseURL == "" && (cfg.DBHost == "" || cfg.DBName == "" || cfg.DBUser == "" || cfg.DBPassword == "") {
			return nil, fmt.Errorf("PostgreSQL requires DB_HOST, DB_NAME, DB_USER and DB_PASSWORD or DATABASE_URL")
		}
		connStr := cfg.PostgresConnString()
		log.Printf("Initializing PostgreSQL repository with endpoint: %s:%s, db: %s", cfg.DBHost, cfg.DBPort, cfg.DBName)
		return NewPostgresRepository(connStr)
	}

	log.Println("Initializing In-Memory repository")
	return NewInMemoryRepository(), nil
}
