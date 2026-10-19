package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aws-containers/retail-store-sample-app/cart/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore(t *testing.T) {
	ctx := context.Background()

	t.Run("GetCart empty and populated", func(t *testing.T) {
		ms := NewMemoryStore()

		// Empty cart returns empty slice
		cart, err := ms.GetCart(ctx, "user-empty")
		require.NoError(t, err)
		assert.Equal(t, "user-empty", cart.CustomerID)
		assert.Empty(t, cart.Items)
		assert.NotNil(t, cart.Items)

		// Populate cart and verify
		item := model.Item{ItemID: "item-1", Quantity: 2, UnitPrice: 500}
		_, err = ms.AddItem(ctx, "user-empty", item)
		require.NoError(t, err)

		cart, err = ms.GetCart(ctx, "user-empty")
		require.NoError(t, err)
		assert.Len(t, cart.Items, 1)
		assert.Equal(t, "item-1", cart.Items[0].ItemID)
		assert.Equal(t, 2, cart.Items[0].Quantity)
		assert.Equal(t, 500, cart.Items[0].UnitPrice)
	})

	t.Run("AddItem new vs existing item", func(t *testing.T) {
		ms := NewMemoryStore()

		// Add new item
		item1 := model.Item{ItemID: "item-a", Quantity: 3, UnitPrice: 150}
		res, err := ms.AddItem(ctx, "user-1", item1)
		require.NoError(t, err)
		assert.Equal(t, "item-a", res.ItemID)
		assert.Equal(t, 3, res.Quantity)
		assert.Equal(t, 150, res.UnitPrice)

		// Add existing item -> accumulates quantity and updates unit price
		item2 := model.Item{ItemID: "item-a", Quantity: 2, UnitPrice: 175}
		res, err = ms.AddItem(ctx, "user-1", item2)
		require.NoError(t, err)
		assert.Equal(t, "item-a", res.ItemID)
		assert.Equal(t, 5, res.Quantity) // 3 + 2
		assert.Equal(t, 175, res.UnitPrice)

		// Verify cart contents
		it, err := ms.GetItem(ctx, "user-1", "item-a")
		require.NoError(t, err)
		assert.Equal(t, 5, it.Quantity)
		assert.Equal(t, 175, it.UnitPrice)
	})

	t.Run("GetItems empty and populated", func(t *testing.T) {
		ms := NewMemoryStore()

		// Non-existent customer returns empty slice
		items, err := ms.GetItems(ctx, "cust-nonexistent")
		require.NoError(t, err)
		assert.Empty(t, items)
		assert.NotNil(t, items)

		// Add multiple items
		_, err = ms.AddItem(ctx, "cust-1", model.Item{ItemID: "item-1", Quantity: 1, UnitPrice: 100})
		require.NoError(t, err)
		_, err = ms.AddItem(ctx, "cust-1", model.Item{ItemID: "item-2", Quantity: 4, UnitPrice: 200})
		require.NoError(t, err)

		items, err = ms.GetItems(ctx, "cust-1")
		require.NoError(t, err)
		assert.Len(t, items, 2)
	})

	t.Run("GetItem found vs ErrNotFound", func(t *testing.T) {
		ms := NewMemoryStore()

		// Customer missing
		_, err := ms.GetItem(ctx, "missing-cust", "item-1")
		assert.ErrorIs(t, err, ErrNotFound)

		// Customer exists but item missing
		_, err = ms.AddItem(ctx, "cust-1", model.Item{ItemID: "item-1", Quantity: 1, UnitPrice: 100})
		require.NoError(t, err)

		_, err = ms.GetItem(ctx, "cust-1", "missing-item")
		assert.ErrorIs(t, err, ErrNotFound)

		// Found
		item, err := ms.GetItem(ctx, "cust-1", "item-1")
		require.NoError(t, err)
		assert.Equal(t, "item-1", item.ItemID)
		assert.Equal(t, 1, item.Quantity)
	})

	t.Run("UpdateItem success vs ErrNotFound", func(t *testing.T) {
		ms := NewMemoryStore()

		// Customer missing
		err := ms.UpdateItem(ctx, "missing-cust", model.Item{ItemID: "item-1", Quantity: 2, UnitPrice: 50})
		assert.ErrorIs(t, err, ErrNotFound)

		// Customer exists, item missing
		_, err = ms.AddItem(ctx, "cust-1", model.Item{ItemID: "item-1", Quantity: 1, UnitPrice: 100})
		require.NoError(t, err)

		err = ms.UpdateItem(ctx, "cust-1", model.Item{ItemID: "item-2", Quantity: 2, UnitPrice: 50})
		assert.ErrorIs(t, err, ErrNotFound)

		// Success
		err = ms.UpdateItem(ctx, "cust-1", model.Item{ItemID: "item-1", Quantity: 10, UnitPrice: 250})
		require.NoError(t, err)

		it, err := ms.GetItem(ctx, "cust-1", "item-1")
		require.NoError(t, err)
		assert.Equal(t, 10, it.Quantity)
		assert.Equal(t, 250, it.UnitPrice)
	})

	t.Run("DeleteItem existing vs non-existent", func(t *testing.T) {
		ms := NewMemoryStore()

		// Delete on non-existent customer does not error
		err := ms.DeleteItem(ctx, "missing-cust", "item-1")
		require.NoError(t, err)

		// Add item and delete it
		_, err = ms.AddItem(ctx, "cust-1", model.Item{ItemID: "item-1", Quantity: 1, UnitPrice: 100})
		require.NoError(t, err)

		// Delete non-existent item on existing customer does not error
		err = ms.DeleteItem(ctx, "cust-1", "item-2")
		require.NoError(t, err)

		// Delete existing item
		err = ms.DeleteItem(ctx, "cust-1", "item-1")
		require.NoError(t, err)

		_, err = ms.GetItem(ctx, "cust-1", "item-1")
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("DeleteCart removes cart and returns empty cart", func(t *testing.T) {
		ms := NewMemoryStore()

		_, err := ms.AddItem(ctx, "cust-del", model.Item{ItemID: "item-1", Quantity: 2, UnitPrice: 100})
		require.NoError(t, err)

		deletedCart, err := ms.DeleteCart(ctx, "cust-del")
		require.NoError(t, err)
		assert.Equal(t, "cust-del", deletedCart.CustomerID)
		assert.Empty(t, deletedCart.Items)

		cart, err := ms.GetCart(ctx, "cust-del")
		require.NoError(t, err)
		assert.Empty(t, cart.Items)
	})

	t.Run("MergeCarts edge cases", func(t *testing.T) {
		// Case 1: session doesn't exist
		ms := NewMemoryStore()
		_, err := ms.AddItem(ctx, "cust-1", model.Item{ItemID: "item-1", Quantity: 1, UnitPrice: 50})
		require.NoError(t, err)

		err = ms.MergeCarts(ctx, "cust-1", "session-unknown")
		require.NoError(t, err)
		items, err := ms.GetItems(ctx, "cust-1")
		require.NoError(t, err)
		assert.Len(t, items, 1)

		// Case 2: session is empty
		ms.carts["session-empty"] = make(map[string]model.Item)
		err = ms.MergeCarts(ctx, "cust-1", "session-empty")
		require.NoError(t, err)
		assert.NotContains(t, ms.carts, "session-empty")

		// Case 3: customer cart doesn't exist yet
		_, err = ms.AddItem(ctx, "session-2", model.Item{ItemID: "item-new", Quantity: 3, UnitPrice: 99})
		require.NoError(t, err)
		err = ms.MergeCarts(ctx, "cust-new", "session-2")
		require.NoError(t, err)

		cartNew, err := ms.GetCart(ctx, "cust-new")
		require.NoError(t, err)
		assert.Len(t, cartNew.Items, 1)
		assert.Equal(t, "item-new", cartNew.Items[0].ItemID)
		assert.Equal(t, 3, cartNew.Items[0].Quantity)
		assert.NotContains(t, ms.carts, "session-2")

		// Case 4: session has overlapping + new items
		_, err = ms.AddItem(ctx, "cust-existing", model.Item{ItemID: "overlap", Quantity: 2, UnitPrice: 100})
		require.NoError(t, err)
		_, err = ms.AddItem(ctx, "cust-existing", model.Item{ItemID: "cust-only", Quantity: 1, UnitPrice: 50})
		require.NoError(t, err)

		_, err = ms.AddItem(ctx, "session-merge", model.Item{ItemID: "overlap", Quantity: 5, UnitPrice: 120})
		require.NoError(t, err)
		_, err = ms.AddItem(ctx, "session-merge", model.Item{ItemID: "session-only", Quantity: 4, UnitPrice: 80})
		require.NoError(t, err)

		err = ms.MergeCarts(ctx, "cust-existing", "session-merge")
		require.NoError(t, err)
		assert.NotContains(t, ms.carts, "session-merge")

		overlapItem, err := ms.GetItem(ctx, "cust-existing", "overlap")
		require.NoError(t, err)
		assert.Equal(t, 7, overlapItem.Quantity)    // 2 + 5
		assert.Equal(t, 120, overlapItem.UnitPrice) // updated unit price

		sessOnly, err := ms.GetItem(ctx, "cust-existing", "session-only")
		require.NoError(t, err)
		assert.Equal(t, 4, sessOnly.Quantity)

		custOnly, err := ms.GetItem(ctx, "cust-existing", "cust-only")
		require.NoError(t, err)
		assert.Equal(t, 1, custOnly.Quantity)
	})

	t.Run("ProcessJob scenarios", func(t *testing.T) {
		ms := NewMemoryStore()

		// DBMode: "none"
		isNew, err := ms.ProcessJob(ctx, model.JobMessage{
			JobID:  "job-none-1",
			DBMode: "none",
		})
		require.NoError(t, err)
		assert.True(t, isNew)

		// DBMode "none" can be processed multiple times since it does not record
		isNew, err = ms.ProcessJob(ctx, model.JobMessage{
			JobID:  "job-none-1",
			DBMode: "none",
		})
		require.NoError(t, err)
		assert.True(t, isNew)

		// DBMode: "light" new vs duplicate
		isNew, err = ms.ProcessJob(ctx, model.JobMessage{
			JobID:  "job-light-1",
			DBMode: "light",
		})
		require.NoError(t, err)
		assert.True(t, isNew)

		isNew, err = ms.ProcessJob(ctx, model.JobMessage{
			JobID:  "job-light-1",
			DBMode: "light",
		})
		require.NoError(t, err)
		assert.False(t, isNew) // duplicate returns false, nil

		// DBMode: "full" with CustomerID
		isNew, err = ms.ProcessJob(ctx, model.JobMessage{
			JobID:      "job-full-1",
			DBMode:     "full",
			CustomerID: "cust-worker",
		})
		require.NoError(t, err)
		assert.True(t, isNew)

		it, err := ms.GetItem(ctx, "cust-worker", "job-item")
		require.NoError(t, err)
		assert.Equal(t, 1, it.Quantity)
		assert.Equal(t, 100, it.UnitPrice)

		// Duplicate of job-full-1 returns false, nil and doesn't increment
		isNew, err = ms.ProcessJob(ctx, model.JobMessage{
			JobID:      "job-full-1",
			DBMode:     "full",
			CustomerID: "cust-worker",
		})
		require.NoError(t, err)
		assert.False(t, isNew)

		it, err = ms.GetItem(ctx, "cust-worker", "job-item")
		require.NoError(t, err)
		assert.Equal(t, 1, it.Quantity)

		// Another full job increments job-item
		isNew, err = ms.ProcessJob(ctx, model.JobMessage{
			JobID:      "job-full-2",
			DBMode:     "full",
			CustomerID: "cust-worker",
		})
		require.NoError(t, err)
		assert.True(t, isNew)

		it, err = ms.GetItem(ctx, "cust-worker", "job-item")
		require.NoError(t, err)
		assert.Equal(t, 2, it.Quantity)

		// DBMode full with empty CustomerID does not create cart item
		isNew, err = ms.ProcessJob(ctx, model.JobMessage{
			JobID:      "job-full-no-cust",
			DBMode:     "full",
			CustomerID: "",
		})
		require.NoError(t, err)
		assert.True(t, isNew)
	})

	t.Run("Ping and Close", func(t *testing.T) {
		ms := NewMemoryStore()
		assert.NoError(t, ms.Ping(ctx))
		assert.NoError(t, ms.Close())
	})
}

func expectMigrations(mock sqlmock.Sqlmock) {
	mock.ExpectExec("CREATE SCHEMA IF NOT EXISTS cart;").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS cart.carts (
			customer_id VARCHAR(255) PRIMARY KEY,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS cart.cart_items (
			id SERIAL PRIMARY KEY,
			customer_id VARCHAR(255) NOT NULL REFERENCES cart.carts(customer_id) ON DELETE CASCADE,
			item_id VARCHAR(255) NOT NULL,
			quantity INT NOT NULL,
			unit_price INT NOT NULL,
			UNIQUE(customer_id, item_id)
		);`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS cart.processed_jobs (
			job_id VARCHAR(64) PRIMARY KEY,
			customer_id VARCHAR(255),
			status VARCHAR(64) DEFAULT 'COMPLETED',
			created_at TIMESTAMPTZ DEFAULT NOW()
		);`).WillReturnResult(sqlmock.NewResult(0, 0))
}

func TestPostgresStore(t *testing.T) {
	ctx := context.Background()

	t.Run("NewPostgresStore migration success and failure", func(t *testing.T) {
		// Success case
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		require.NoError(t, err)
		defer db.Close()

		expectMigrations(mock)
		pgStore, err := NewPostgresStore(db)
		require.NoError(t, err)
		assert.NotNil(t, pgStore)
		require.NoError(t, mock.ExpectationsWereMet())

		// Failure case
		dbFail, mockFail, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		require.NoError(t, err)
		defer dbFail.Close()

		mockFail.ExpectExec("CREATE SCHEMA IF NOT EXISTS cart;").WillReturnError(errors.New("db permission denied"))
		pgFail, err := NewPostgresStore(dbFail)
		assert.Error(t, err)
		assert.Nil(t, pgFail)
		assert.Contains(t, err.Error(), "failed to run postgres migrations")
		require.NoError(t, mockFail.ExpectationsWereMet())
	})

	t.Run("GetCart and GetItems query rows and error case", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		require.NoError(t, err)
		defer db.Close()

		expectMigrations(mock)
		pgStore, err := NewPostgresStore(db)
		require.NoError(t, err)

		// 1. Success case
		mock.ExpectQuery(`SELECT item_id, quantity, unit_price FROM cart.cart_items WHERE customer_id = $1 ORDER BY id ASC`).
			WithArgs("user-1").
			WillReturnRows(sqlmock.NewRows([]string{"item_id", "quantity", "unit_price"}).
				AddRow("item-1", 2, 100).
				AddRow("item-2", 5, 200))

		cart, err := pgStore.GetCart(ctx, "user-1")
		require.NoError(t, err)
		assert.Equal(t, "user-1", cart.CustomerID)
		assert.Len(t, cart.Items, 2)
		assert.Equal(t, "item-1", cart.Items[0].ItemID)
		assert.Equal(t, 2, cart.Items[0].Quantity)

		// 2. Query error case in GetItems
		mock.ExpectQuery(`SELECT item_id, quantity, unit_price FROM cart.cart_items WHERE customer_id = $1 ORDER BY id ASC`).
			WithArgs("user-err").
			WillReturnError(errors.New("connection broken"))

		items, err := pgStore.GetItems(ctx, "user-err")
		assert.Error(t, err)
		assert.Nil(t, items)
		assert.Contains(t, err.Error(), "failed to query cart items")

		// 3. Query error case in GetCart
		mock.ExpectQuery(`SELECT item_id, quantity, unit_price FROM cart.cart_items WHERE customer_id = $1 ORDER BY id ASC`).
			WithArgs("user-err").
			WillReturnError(errors.New("connection broken"))

		cartErr, err := pgStore.GetCart(ctx, "user-err")
		assert.Error(t, err)
		assert.Nil(t, cartErr)

		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("MergeCarts transaction success and rollback", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		require.NoError(t, err)
		defer db.Close()

		expectMigrations(mock)
		pgStore, err := NewPostgresStore(db)
		require.NoError(t, err)

		// 1. Full transaction success:
		// upsert customer cart, query session items, upsert merged items, delete session cart, commit
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO cart.carts (customer_id, updated_at) VALUES ($1, NOW())
		ON CONFLICT (customer_id) DO UPDATE SET updated_at = NOW()`).
			WithArgs("user-dest").
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectQuery(`SELECT item_id, quantity, unit_price FROM cart.cart_items WHERE customer_id = $1`).
			WithArgs("session-src").
			WillReturnRows(sqlmock.NewRows([]string{"item_id", "quantity", "unit_price"}).
				AddRow("item-1", 3, 150).
				AddRow("item-2", 1, 99))

		mock.ExpectExec(`INSERT INTO cart.cart_items (customer_id, item_id, quantity, unit_price)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (customer_id, item_id)
		DO UPDATE SET quantity = cart.cart_items.quantity + EXCLUDED.quantity, unit_price = EXCLUDED.unit_price`).
			WithArgs("user-dest", "item-1", 3, 150).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectExec(`INSERT INTO cart.cart_items (customer_id, item_id, quantity, unit_price)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (customer_id, item_id)
		DO UPDATE SET quantity = cart.cart_items.quantity + EXCLUDED.quantity, unit_price = EXCLUDED.unit_price`).
			WithArgs("user-dest", "item-2", 1, 99).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectExec(`DELETE FROM cart.carts WHERE customer_id = $1`).
			WithArgs("session-src").
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectCommit()

		err = pgStore.MergeCarts(ctx, "user-dest", "session-src")
		require.NoError(t, err)

		// 2. Rollback on error when upserting customer cart fails
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO cart.carts (customer_id, updated_at) VALUES ($1, NOW())
		ON CONFLICT (customer_id) DO UPDATE SET updated_at = NOW()`).
			WithArgs("user-dest").
			WillReturnError(errors.New("tx write error"))
		mock.ExpectRollback()

		err = pgStore.MergeCarts(ctx, "user-dest", "session-src")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to ensure customer cart")

		// 3. Rollback on error when querying session items fails
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO cart.carts (customer_id, updated_at) VALUES ($1, NOW())
		ON CONFLICT (customer_id) DO UPDATE SET updated_at = NOW()`).
			WithArgs("user-dest").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectQuery(`SELECT item_id, quantity, unit_price FROM cart.cart_items WHERE customer_id = $1`).
			WithArgs("session-src").
			WillReturnError(errors.New("query session failed"))
		mock.ExpectRollback()

		err = pgStore.MergeCarts(ctx, "user-dest", "session-src")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to query session items")

		// 4. Rollback on error when upserting merged item fails
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO cart.carts (customer_id, updated_at) VALUES ($1, NOW())
		ON CONFLICT (customer_id) DO UPDATE SET updated_at = NOW()`).
			WithArgs("user-dest").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectQuery(`SELECT item_id, quantity, unit_price FROM cart.cart_items WHERE customer_id = $1`).
			WithArgs("session-src").
			WillReturnRows(sqlmock.NewRows([]string{"item_id", "quantity", "unit_price"}).
				AddRow("item-1", 3, 150))
		mock.ExpectExec(`INSERT INTO cart.cart_items (customer_id, item_id, quantity, unit_price)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (customer_id, item_id)
		DO UPDATE SET quantity = cart.cart_items.quantity + EXCLUDED.quantity, unit_price = EXCLUDED.unit_price`).
			WithArgs("user-dest", "item-1", 3, 150).
			WillReturnError(errors.New("item merge conflict"))
		mock.ExpectRollback()

		err = pgStore.MergeCarts(ctx, "user-dest", "session-src")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to merge item")

		// 5. Rollback on error when deleting session cart fails
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO cart.carts (customer_id, updated_at) VALUES ($1, NOW())
		ON CONFLICT (customer_id) DO UPDATE SET updated_at = NOW()`).
			WithArgs("user-dest").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectQuery(`SELECT item_id, quantity, unit_price FROM cart.cart_items WHERE customer_id = $1`).
			WithArgs("session-src").
			WillReturnRows(sqlmock.NewRows([]string{"item_id", "quantity", "unit_price"}))
		mock.ExpectExec(`DELETE FROM cart.carts WHERE customer_id = $1`).
			WithArgs("session-src").
			WillReturnError(errors.New("delete session failed"))
		mock.ExpectRollback()

		err = pgStore.MergeCarts(ctx, "user-dest", "session-src")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to remove session cart")

		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("UpdateItem ErrNotFound when 0 rows affected", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		require.NoError(t, err)
		defer db.Close()

		expectMigrations(mock)
		pgStore, err := NewPostgresStore(db)
		require.NoError(t, err)

		// 0 rows affected -> ErrNotFound
		mock.ExpectExec(`UPDATE cart.cart_items SET quantity = $1, unit_price = $2 WHERE customer_id = $3 AND item_id = $4`).
			WithArgs(5, 300, "user-1", "item-999").
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = pgStore.UpdateItem(ctx, "user-1", model.Item{ItemID: "item-999", Quantity: 5, UnitPrice: 300})
		assert.ErrorIs(t, err, ErrNotFound)

		// 1 row affected -> success
		mock.ExpectExec(`UPDATE cart.cart_items SET quantity = $1, unit_price = $2 WHERE customer_id = $3 AND item_id = $4`).
			WithArgs(2, 250, "user-1", "item-1").
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = pgStore.UpdateItem(ctx, "user-1", model.Item{ItemID: "item-1", Quantity: 2, UnitPrice: 250})
		require.NoError(t, err)

		// DB error
		mock.ExpectExec(`UPDATE cart.cart_items SET quantity = $1, unit_price = $2 WHERE customer_id = $3 AND item_id = $4`).
			WithArgs(2, 250, "user-1", "item-1").
			WillReturnError(errors.New("update exec error"))

		err = pgStore.UpdateItem(ctx, "user-1", model.Item{ItemID: "item-1", Quantity: 2, UnitPrice: 250})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to update item")

		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("GetItem ErrNotFound on sql.ErrNoRows", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		require.NoError(t, err)
		defer db.Close()

		expectMigrations(mock)
		pgStore, err := NewPostgresStore(db)
		require.NoError(t, err)

		// sql.ErrNoRows -> ErrNotFound
		mock.ExpectQuery(`SELECT item_id, quantity, unit_price FROM cart.cart_items WHERE customer_id = $1 AND item_id = $2`).
			WithArgs("user-1", "item-missing").
			WillReturnError(sql.ErrNoRows)

		item, err := pgStore.GetItem(ctx, "user-1", "item-missing")
		assert.ErrorIs(t, err, ErrNotFound)
		assert.Nil(t, item)

		// DB error
		mock.ExpectQuery(`SELECT item_id, quantity, unit_price FROM cart.cart_items WHERE customer_id = $1 AND item_id = $2`).
			WithArgs("user-1", "item-err").
			WillReturnError(errors.New("db read failure"))

		item, err = pgStore.GetItem(ctx, "user-1", "item-err")
		assert.Error(t, err)
		assert.Nil(t, item)
		assert.Contains(t, err.Error(), "failed to get item")

		// Success
		mock.ExpectQuery(`SELECT item_id, quantity, unit_price FROM cart.cart_items WHERE customer_id = $1 AND item_id = $2`).
			WithArgs("user-1", "item-found").
			WillReturnRows(sqlmock.NewRows([]string{"item_id", "quantity", "unit_price"}).AddRow("item-found", 3, 400))

		item, err = pgStore.GetItem(ctx, "user-1", "item-found")
		require.NoError(t, err)
		assert.Equal(t, "item-found", item.ItemID)
		assert.Equal(t, 3, item.Quantity)
		assert.Equal(t, 400, item.UnitPrice)

		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("ProcessJob DBMode none, light, and full", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		require.NoError(t, err)
		defer db.Close()

		expectMigrations(mock)
		pgStore, err := NewPostgresStore(db)
		require.NoError(t, err)

		// 1. DBMode: "none"
		isNew, err := pgStore.ProcessJob(ctx, model.JobMessage{
			JobID:  "job-none",
			DBMode: "none",
		})
		require.NoError(t, err)
		assert.True(t, isNew)

		// 2. DBMode: "light" new job (rowsAffected == 1)
		mock.ExpectExec(`INSERT INTO cart.processed_jobs (job_id, customer_id, status, created_at)
			VALUES ($1, $2, 'COMPLETED', NOW())
			ON CONFLICT (job_id) DO NOTHING`).
			WithArgs("job-light-new", "cust-1").
			WillReturnResult(sqlmock.NewResult(1, 1))

		isNew, err = pgStore.ProcessJob(ctx, model.JobMessage{
			JobID:      "job-light-new",
			CustomerID: "cust-1",
			DBMode:     "light",
		})
		require.NoError(t, err)
		assert.True(t, isNew)

		// 3. DBMode: "light" duplicate job (rowsAffected == 0)
		mock.ExpectExec(`INSERT INTO cart.processed_jobs (job_id, customer_id, status, created_at)
			VALUES ($1, $2, 'COMPLETED', NOW())
			ON CONFLICT (job_id) DO NOTHING`).
			WithArgs("job-light-dup", "cust-1").
			WillReturnResult(sqlmock.NewResult(0, 0))

		isNew, err = pgStore.ProcessJob(ctx, model.JobMessage{
			JobID:      "job-light-dup",
			CustomerID: "cust-1",
			DBMode:     "light",
		})
		require.NoError(t, err)
		assert.False(t, isNew)

		// 4. DBMode: "full" new job with CustomerID upserting cart & job-item
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO cart.processed_jobs (job_id, customer_id, status, created_at)
		VALUES ($1, $2, 'COMPLETED', NOW())
		ON CONFLICT (job_id) DO NOTHING`).
			WithArgs("job-full-new", "cust-worker").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(`INSERT INTO cart.carts (customer_id, updated_at) VALUES ($1, NOW())
			ON CONFLICT (customer_id) DO UPDATE SET updated_at = NOW()`).
			WithArgs("cust-worker").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec(`INSERT INTO cart.cart_items (customer_id, item_id, quantity, unit_price)
			VALUES ($1, 'job-item', 1, 100)
			ON CONFLICT (customer_id, item_id)
			DO UPDATE SET quantity = cart.cart_items.quantity + EXCLUDED.quantity, unit_price = EXCLUDED.unit_price`).
			WithArgs("cust-worker").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		isNew, err = pgStore.ProcessJob(ctx, model.JobMessage{
			JobID:      "job-full-new",
			CustomerID: "cust-worker",
			DBMode:     "full",
		})
		require.NoError(t, err)
		assert.True(t, isNew)

		// 5. DBMode: "full" duplicate job (rowsAffected == 0)
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO cart.processed_jobs (job_id, customer_id, status, created_at)
		VALUES ($1, $2, 'COMPLETED', NOW())
		ON CONFLICT (job_id) DO NOTHING`).
			WithArgs("job-full-dup", "cust-worker").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		isNew, err = pgStore.ProcessJob(ctx, model.JobMessage{
			JobID:      "job-full-dup",
			CustomerID: "cust-worker",
			DBMode:     "full",
		})
		require.NoError(t, err)
		assert.False(t, isNew)

		// 6. DBMode: "full" new job without CustomerID
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO cart.processed_jobs (job_id, customer_id, status, created_at)
		VALUES ($1, $2, 'COMPLETED', NOW())
		ON CONFLICT (job_id) DO NOTHING`).
			WithArgs("job-full-no-cust", "").
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()

		isNew, err = pgStore.ProcessJob(ctx, model.JobMessage{
			JobID:      "job-full-no-cust",
			CustomerID: "",
			DBMode:     "full",
		})
		require.NoError(t, err)
		assert.True(t, isNew)

		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Ping and Close", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual), sqlmock.MonitorPingsOption(true))
		require.NoError(t, err)

		expectMigrations(mock)
		pgStore, err := NewPostgresStore(db)
		require.NoError(t, err)

		mock.ExpectPing()
		err = pgStore.Ping(ctx)
		require.NoError(t, err)

		mock.ExpectClose()
		err = pgStore.Close()
		require.NoError(t, err)

		require.NoError(t, mock.ExpectationsWereMet())
	})
}
