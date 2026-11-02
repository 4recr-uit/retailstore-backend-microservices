package repository

import (
	"context"
	"fmt"
	"github.com/aws-containers/retail-store-sample-app/catalog/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"testing"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return db
}
func TestSeedIsRepeatableAndPingDetectsDisconnect(t *testing.T) {
	db := testDB(t)
	if err := initializeInMemoryData(db); err != nil {
		t.Fatal(err)
	}
	var before, after int64
	db.Model(&model.Product{}).Count(&before)
	if before == 0 {
		t.Fatal("no seed products")
	}
	if err := initializeInMemoryData(db); err != nil {
		t.Fatal(err)
	}
	db.Model(&model.Product{}).Count(&after)
	if before != after {
		t.Fatal("seed was duplicated")
	}
	repo := &Database{DB: db}
	if err := repo.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.Close()
	if repo.Ping(context.Background()) == nil {
		t.Fatal("closed DB reported ready")
	}
}
func TestInitializationPropagatesMigrationAndSeedErrors(t *testing.T) {
	t.Run("migration", func(t *testing.T) {
		db := testDB(t)
		sqlDB, _ := db.DB()
		sqlDB.Close()
		if initializeInMemoryData(db) == nil {
			t.Fatal("migration failure ignored")
		}
	})
	t.Run("seed", func(t *testing.T) {
		db := testDB(t)
		db.Callback().Create().Before("gorm:create").Register("fail_seed", func(tx *gorm.DB) { tx.AddError(fmt.Errorf("seed denied")) })
		if initializeInMemoryData(db) == nil {
			t.Fatal("seed failure ignored")
		}
	})
}

func TestPersistentInitializationIsReadOnly(t *testing.T) {
	db := testDB(t)
	if err := initializeInMemoryData(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE products SET name = ?", "custom catalog name").Error; err != nil {
		t.Fatal(err)
	}
	// SQLite enforces read-only SQL, exercising the MySQL startup branch without
	// allowing CREATE/ALTER or seed writes to pass unnoticed.
	if err := db.Exec("PRAGMA query_only = ON").Error; err != nil {
		t.Fatal(err)
	}
	if err := initializeRepository(context.Background(), db, "mysql"); err != nil {
		t.Fatalf("startup must work with SELECT-only access: %v", err)
	}
	var product model.Product
	if err := db.First(&product).Error; err != nil {
		t.Fatal(err)
	}
	if product.Name != "custom catalog name" {
		t.Fatal("startup overwrote existing catalog data")
	}
}

func TestPersistentInitializationRejectsMissingSchema(t *testing.T) {
	for _, ddl := range []string{
		"", // No tables.
		"CREATE TABLE products (id TEXT, name TEXT, description TEXT)",                // Missing price.
		"CREATE TABLE products (id TEXT, name TEXT, description TEXT, price INTEGER)", // Missing tags.
	} {
		t.Run(ddl, func(t *testing.T) {
			db := testDB(t)
			if ddl != "" {
				if err := db.Exec(ddl).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := initializeRepository(context.Background(), db, "mysql"); err == nil {
				t.Fatal("startup accepted missing tables or columns")
			}
			if db.Migrator().HasTable(&model.Tag{}) {
				t.Fatal("startup created a missing table")
			}
		})
	}
}
