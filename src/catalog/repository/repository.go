package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/aws-containers/retail-store-sample-app/catalog/config"
	"github.com/aws-containers/retail-store-sample-app/catalog/model"
	"github.com/glebarez/sqlite"
	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/plugin/opentelemetry/tracing"
)

type Database struct {
	DB *gorm.DB
}

type CatalogRepository interface {
	GetProducts(tags []string, order string, pageNum, pageSize int, ctx context.Context) ([]model.Product, error)
	CountProducts(tags []string, ctx context.Context) (int, error)
	GetProduct(id string, ctx context.Context) (*model.Product, error)
	GetTags(ctx context.Context) ([]model.Tag, error)
}

func createMySQLDatabase(config config.DatabaseConfiguration) (*gorm.DB, error) {
	if config.Endpoint == "" || config.Name == "" || config.User == "" || config.Password == "" {
		return nil, fmt.Errorf("MySQL requires catalog endpoint, database, user and password")
	}
	timeout := config.ConnectTimeout
	if timeout <= 0 {
		timeout = 5
	}
	dsn := drivermysql.NewConfig()
	dsn.User, dsn.Passwd, dsn.Net, dsn.Addr, dsn.DBName = config.User, config.Password, "tcp", config.Endpoint, config.Name
	dsn.Timeout = time.Duration(timeout) * time.Second
	dsn.ReadTimeout, dsn.WriteTimeout = dsn.Timeout, dsn.Timeout
	dsn.ParseTime = true
	dsn.Params = map[string]string{"charset": "utf8mb4"}
	if config.TLS {
		dsn.TLSConfig = "true"
	}
	connectionString := dsn.FormatDSN()
	attempts := config.ConnectAttempts
	if attempts <= 0 {
		attempts = 6
	}
	retrySeconds := config.ConnectRetrySeconds
	if retrySeconds <= 0 {
		retrySeconds = 5
	}
	maxOpen := config.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 10
	}
	maxIdle := config.MaxIdleConns
	if maxIdle < 0 {
		maxIdle = 0
	}
	lifetime := config.ConnMaxLifetimeSeconds
	if lifetime <= 0 {
		lifetime = 1800
	}

	var db *gorm.DB
	var err error

	for i := 0; i < attempts; i++ {
		db, err = gorm.Open(mysql.Open(connectionString), &gorm.Config{})
		if err == nil {
			sqlDB, sqlErr := db.DB()
			if sqlErr != nil {
				return nil, fmt.Errorf("failed to get underlying sql.DB: %w", sqlErr)
			}
			sqlDB.SetMaxOpenConns(maxOpen)
			sqlDB.SetMaxIdleConns(maxIdle)
			sqlDB.SetConnMaxLifetime(time.Duration(lifetime) * time.Second)
			return db, nil
		}
		fmt.Printf("Waiting for MySQL to be ready... (%v)\n", err)
		if i+1 < attempts {
			time.Sleep(time.Duration(retrySeconds) * time.Second)
		}
	}

	return nil, err
}

func NewRepository(config config.DatabaseConfiguration) (CatalogRepository, error) {
	var db *gorm.DB
	var err error

	if config.Type == "mysql" {
		fmt.Printf("Using mysql database %s\n", config.Endpoint)
		db, err = createMySQLDatabase(config)
	} else {
		fmt.Println("Using in-memory database")
		db, err = gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	if err := db.Use(tracing.NewPlugin(tracing.WithoutMetrics())); err != nil {
		return nil, fmt.Errorf("initialize database tracing: %w", err)
	}
	if err := initializeData(db); err != nil {
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
		return nil, err
	}
	return &Database{DB: db}, nil
}

func initializeData(db *gorm.DB) error {
	if err := db.AutoMigrate(&model.Product{}); err != nil {
		return fmt.Errorf("migrate catalog schema: %w", err)
	}
	products, err := LoadProductData()
	if err != nil {
		return err
	}

	tags, err := LoadProductTagData()
	if err != nil {
		return err
	}

	return db.Transaction(func(db *gorm.DB) error {
		tagMap := make(map[string]model.Tag)

		for _, tag := range tags {
			tagEntity := model.Tag{Name: tag.Name, DisplayName: tag.DisplayName}
			if err := db.Save(&tagEntity).Error; err != nil {
				return fmt.Errorf("seed catalog tag: %w", err)
			}

			tagMap[tag.Name] = tagEntity
		}

		for _, product := range products {
			var result model.Product
			r := db.
				Where("id = ?", product.ID).
				Limit(1).
				Find(&result)
			if r.Error != nil {
				return fmt.Errorf("look up catalog seed product: %w", r.Error)
			}

			if r.RowsAffected > 0 {
				continue
			}

			productTags := []model.Tag{}
			for _, tag := range product.Tags {
				productTags = append(productTags, tagMap[tag])
			}

			if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.Product{
				ID:          product.ID,
				Name:        product.Name,
				Description: product.Description,
				Price:       product.Price,
				Tags:        productTags,
			}).Error; err != nil {
				return fmt.Errorf("seed catalog product: %w", err)
			}
		}
		return nil
	})
}

func (db *Database) Ping(ctx context.Context) error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

func (db *Database) GetProducts(tags []string, order string, pageNum, pageSize int, ctx context.Context) ([]model.Product, error) {
	products := []model.Product{}

	query := db.DB.Preload("Tags")

	// Apply tags filter if provided
	if len(tags) > 0 {
		query = query.Joins("JOIN product_tags ON product_tags.product_id = products.id").
			Joins("JOIN tags ON tags.name = product_tags.tag_name").
			Where("tags.name IN ?", tags).
			Group("products.id")
	}

	// Apply ordering
	switch order {
	case "price_asc":
		query = query.Order("price asc")
	case "price_desc":
		query = query.Order("price desc")
	default:
		query = query.Order("products.name asc") // default ordering
	}

	// Apply pagination
	offset := (pageNum - 1) * pageSize
	query = query.Offset(offset).Limit(pageSize)

	// Execute the query
	err := query.WithContext(ctx).Find(&products).Error
	if err != nil {
		return nil, err
	}

	return products, err
}

func (db *Database) GetProduct(id string, ctx context.Context) (*model.Product, error) {
	product := model.Product{}

	err := db.DB.WithContext(ctx).
		Preload("Tags").
		Where("id = ?", id).
		First(&product).Error

	if err != nil {
		return nil, err
	}

	return &product, err
}

func (db *Database) CountProducts(tags []string, ctx context.Context) (int, error) {
	var count int64

	query := db.DB.WithContext(ctx).Model(&model.Product{})

	// Apply tags filter if provided
	if len(tags) > 0 {
		query = query.Joins("JOIN product_tags ON product_tags.product_id = products.id").
			Joins("JOIN tags ON tags.name = product_tags.tag_name").
			Where("tags.name IN ?", tags).
			Group("products.id")
	}

	err := query.Count(&count).Error
	if err != nil {
		return 0, err
	}

	return int(count), nil
}

func (db *Database) GetTags(ctx context.Context) ([]model.Tag, error) {
	tags := []model.Tag{}

	err := db.DB.WithContext(ctx).
		Model(&model.Tag{}).
		Order("display_name asc"). // Order by display name alphabetically
		Find(&tags).Error

	if err != nil {
		return nil, fmt.Errorf("failed to fetch tags: %w", err)
	}

	return tags, err
}
