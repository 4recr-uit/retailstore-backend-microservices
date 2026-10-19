package db

import (
	"log"

	"github.com/aws-containers/retail-store-sample-app/orders/pkg/config"
)

func InitRepository(cfg *config.Config) (Repository, error) {
	if cfg.DBProvider == "postgres" {
		connStr := cfg.PostgresConnString()
		log.Printf("Initializing PostgreSQL repository with endpoint: %s:%s, db: %s", cfg.DBHost, cfg.DBPort, cfg.DBName)
		return NewPostgresRepository(connStr)
	}

	log.Println("Initializing In-Memory repository")
	return NewInMemoryRepository(), nil
}
