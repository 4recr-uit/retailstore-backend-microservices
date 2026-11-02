package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	appconfig "github.com/aws-containers/retail-store-sample-app/cart/pkg/config"

	"github.com/aws-containers/retail-store-sample-app/cart/pkg/model"
	_ "github.com/lib/pq"
)

// PostgresStore implements Store backed by PostgreSQL.
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore connects to an existing schema without creating or changing it.
func NewPostgresStore(db *sql.DB) (*PostgresStore, error) {
	db.SetMaxOpenConns(appconfig.EnvInt("DB_MAX_OPEN_CONNS", 3))
	db.SetMaxIdleConns(appconfig.EnvInt("DB_MAX_IDLE_CONNS", 2))
	db.SetConnMaxLifetime(appconfig.EnvSeconds("DB_CONN_MAX_LIFETIME_SECONDS", 300))

	p := &PostgresStore{db: db}
	ctx, cancel := context.WithTimeout(context.Background(), appconfig.EnvSeconds("DB_CONNECT_TIMEOUT_SECONDS", 10))
	defer cancel()
	if err := p.checkSchema(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cart database schema is not ready: %w", err)
	}

	return p, nil
}

// LIMIT 0 validates required columns and SELECT access without reading user data.
func (p *PostgresStore) checkSchema(ctx context.Context) error {
	queries := []string{
		"SELECT customer_id, updated_at FROM cart.carts LIMIT 0",
		"SELECT id, customer_id, item_id, quantity, unit_price FROM cart.cart_items LIMIT 0",
		"SELECT job_id, customer_id, status, created_at FROM cart.processed_jobs LIMIT 0",
	}
	for _, query := range queries {
		rows, err := p.db.QueryContext(ctx, query)
		if err != nil {
			return fmt.Errorf("check existing schema (provision it before starting the service): %w", err)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (p *PostgresStore) GetCart(ctx context.Context, customerID string) (*model.Cart, error) {
	items, err := p.GetItems(ctx, customerID)
	if err != nil {
		return nil, err
	}
	return &model.Cart{
		CustomerID: customerID,
		Items:      items,
	}, nil
}

func (p *PostgresStore) DeleteCart(ctx context.Context, customerID string) (*model.Cart, error) {
	query := `DELETE FROM cart.carts WHERE customer_id = $1`
	if _, err := p.db.ExecContext(ctx, query, customerID); err != nil {
		return nil, fmt.Errorf("failed to delete cart: %w", err)
	}
	return &model.Cart{
		CustomerID: customerID,
		Items:      make([]model.Item, 0),
	}, nil
}

func (p *PostgresStore) MergeCarts(ctx context.Context, customerID, sessionID string) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Ensure customer cart exists
	upsertCart := `INSERT INTO cart.carts (customer_id, updated_at) VALUES ($1, NOW())
		ON CONFLICT (customer_id) DO UPDATE SET updated_at = NOW()`
	if _, err := tx.ExecContext(ctx, upsertCart, customerID); err != nil {
		return fmt.Errorf("failed to ensure customer cart: %w", err)
	}

	// Fetch items from session cart
	querySession := `SELECT item_id, quantity, unit_price FROM cart.cart_items WHERE customer_id = $1`
	rows, err := tx.QueryContext(ctx, querySession, sessionID)
	if err != nil {
		return fmt.Errorf("failed to query session items: %w", err)
	}
	defer rows.Close()

	type itemRow struct {
		itemID    string
		quantity  int
		unitPrice int
	}
	var sessionItems []itemRow
	for rows.Next() {
		var it itemRow
		if err := rows.Scan(&it.itemID, &it.quantity, &it.unitPrice); err != nil {
			return err
		}
		sessionItems = append(sessionItems, it)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Merge into customer cart
	upsertItem := `INSERT INTO cart.cart_items (customer_id, item_id, quantity, unit_price)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (customer_id, item_id)
		DO UPDATE SET quantity = cart.cart_items.quantity + EXCLUDED.quantity, unit_price = EXCLUDED.unit_price`
	for _, it := range sessionItems {
		if _, err := tx.ExecContext(ctx, upsertItem, customerID, it.itemID, it.quantity, it.unitPrice); err != nil {
			return fmt.Errorf("failed to merge item: %w", err)
		}
	}

	// Delete session cart
	deleteSession := `DELETE FROM cart.carts WHERE customer_id = $1`
	if _, err := tx.ExecContext(ctx, deleteSession, sessionID); err != nil {
		return fmt.Errorf("failed to remove session cart: %w", err)
	}

	return tx.Commit()
}

func (p *PostgresStore) GetItems(ctx context.Context, customerID string) ([]model.Item, error) {
	query := `SELECT item_id, quantity, unit_price FROM cart.cart_items WHERE customer_id = $1 ORDER BY id ASC`
	rows, err := p.db.QueryContext(ctx, query, customerID)
	if err != nil {
		return nil, fmt.Errorf("failed to query cart items: %w", err)
	}
	defer rows.Close()

	items := make([]model.Item, 0)
	for rows.Next() {
		var it model.Item
		if err := rows.Scan(&it.ItemID, &it.Quantity, &it.UnitPrice); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (p *PostgresStore) AddItem(ctx context.Context, customerID string, item model.Item) (*model.Item, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Ensure customer cart exists
	upsertCart := `INSERT INTO cart.carts (customer_id, updated_at) VALUES ($1, NOW())
		ON CONFLICT (customer_id) DO UPDATE SET updated_at = NOW()`
	if _, err := tx.ExecContext(ctx, upsertCart, customerID); err != nil {
		return nil, fmt.Errorf("failed to ensure cart: %w", err)
	}

	// Upsert item
	upsertItem := `INSERT INTO cart.cart_items (customer_id, item_id, quantity, unit_price)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (customer_id, item_id)
		DO UPDATE SET quantity = cart.cart_items.quantity + EXCLUDED.quantity, unit_price = EXCLUDED.unit_price
		RETURNING quantity, unit_price`

	var finalQty, finalPrice int
	if err := tx.QueryRowContext(ctx, upsertItem, customerID, item.ItemID, item.Quantity, item.UnitPrice).
		Scan(&finalQty, &finalPrice); err != nil {
		return nil, fmt.Errorf("failed to upsert item: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &model.Item{
		ItemID:    item.ItemID,
		Quantity:  finalQty,
		UnitPrice: finalPrice,
	}, nil
}

func (p *PostgresStore) UpdateItem(ctx context.Context, customerID string, item model.Item) error {
	query := `UPDATE cart.cart_items SET quantity = $1, unit_price = $2 WHERE customer_id = $3 AND item_id = $4`
	res, err := p.db.ExecContext(ctx, query, item.Quantity, item.UnitPrice, customerID, item.ItemID)
	if err != nil {
		return fmt.Errorf("failed to update item: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PostgresStore) GetItem(ctx context.Context, customerID, itemID string) (*model.Item, error) {
	query := `SELECT item_id, quantity, unit_price FROM cart.cart_items WHERE customer_id = $1 AND item_id = $2`
	var it model.Item
	if err := p.db.QueryRowContext(ctx, query, customerID, itemID).
		Scan(&it.ItemID, &it.Quantity, &it.UnitPrice); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get item: %w", err)
	}
	return &it, nil
}

func (p *PostgresStore) DeleteItem(ctx context.Context, customerID, itemID string) error {
	query := `DELETE FROM cart.cart_items WHERE customer_id = $1 AND item_id = $2`
	if _, err := p.db.ExecContext(ctx, query, customerID, itemID); err != nil {
		return fmt.Errorf("failed to delete item: %w", err)
	}
	return nil
}

func (p *PostgresStore) ProcessJob(ctx context.Context, job model.JobMessage) (bool, error) {
	if job.DBMode == "none" {
		return true, nil
	}

	if job.DBMode == "light" {
		query := `INSERT INTO cart.processed_jobs (job_id, customer_id, status, created_at)
			VALUES ($1, $2, 'COMPLETED', NOW())
			ON CONFLICT (job_id) DO NOTHING`
		res, err := p.db.ExecContext(ctx, query, job.JobID, job.CustomerID)
		if err != nil {
			return false, fmt.Errorf("failed to record light processed job: %w", err)
		}
		rowsAffected, err := res.RowsAffected()
		if err != nil {
			return false, err
		}
		return rowsAffected == 1, nil
	}

	// Full DB mode: in a single transaction, insert processed_jobs, and if rows_affected == 1, upsert cart and cart_items
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	insertJob := `INSERT INTO cart.processed_jobs (job_id, customer_id, status, created_at)
		VALUES ($1, $2, 'COMPLETED', NOW())
		ON CONFLICT (job_id) DO NOTHING`
	res, err := tx.ExecContext(ctx, insertJob, job.JobID, job.CustomerID)
	if err != nil {
		return false, fmt.Errorf("failed to record full processed job: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if rowsAffected == 0 {
		// Already processed duplicate job
		return false, tx.Commit()
	}

	if job.CustomerID != "" {
		upsertCart := `INSERT INTO cart.carts (customer_id, updated_at) VALUES ($1, NOW())
			ON CONFLICT (customer_id) DO UPDATE SET updated_at = NOW()`
		if _, err := tx.ExecContext(ctx, upsertCart, job.CustomerID); err != nil {
			return false, fmt.Errorf("failed to upsert cart in full mode: %w", err)
		}

		upsertItem := `INSERT INTO cart.cart_items (customer_id, item_id, quantity, unit_price)
			VALUES ($1, 'job-item', 1, 100)
			ON CONFLICT (customer_id, item_id)
			DO UPDATE SET quantity = cart.cart_items.quantity + EXCLUDED.quantity, unit_price = EXCLUDED.unit_price`
		if _, err := tx.ExecContext(ctx, upsertItem, job.CustomerID); err != nil {
			return false, fmt.Errorf("failed to upsert cart item in full mode: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (p *PostgresStore) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

func (p *PostgresStore) Close() error {
	return p.db.Close()
}
