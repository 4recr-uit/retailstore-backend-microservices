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
	if err := initializeData(db); err != nil {
		t.Fatal(err)
	}
	var before, after int64
	db.Model(&model.Product{}).Count(&before)
	if before == 0 {
		t.Fatal("no seed products")
	}
	if err := initializeData(db); err != nil {
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
		if initializeData(db) == nil {
			t.Fatal("migration failure ignored")
		}
	})
	t.Run("seed", func(t *testing.T) {
		db := testDB(t)
		db.Callback().Create().Before("gorm:create").Register("fail_seed", func(tx *gorm.DB) { tx.AddError(fmt.Errorf("seed denied")) })
		if initializeData(db) == nil {
			t.Fatal("seed failure ignored")
		}
	})
}
