package db

import (
	"context"
	"database/sql"
	"fmt"
	appconfig "github.com/aws-containers/retail-store-sample-app/orders/pkg/config"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type PostgresRepository struct {
	db *sql.DB
}

const schemaDDL = `
CREATE SCHEMA IF NOT EXISTS orders;

CREATE TABLE IF NOT EXISTS orders.orders (
    id VARCHAR(255) PRIMARY KEY,
    job_id VARCHAR(64) UNIQUE,
    customer_id VARCHAR(255),
    status VARCHAR(64) DEFAULT 'COMPLETED',
    created_date TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS orders.order_items (
    id SERIAL PRIMARY KEY,
    product_id VARCHAR(255) NOT NULL,
    quantity INT NOT NULL,
    unit_cost INT NOT NULL,
    total_cost INT NOT NULL,
    order_id VARCHAR(255) REFERENCES orders.orders(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS orders.shipping_addresses (
    order_id VARCHAR(255) PRIMARY KEY REFERENCES orders.orders(id) ON DELETE CASCADE,
    first_name VARCHAR(255) NOT NULL,
    last_name VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL,
    address1 VARCHAR(255) NOT NULL,
    address2 VARCHAR(255),
    city VARCHAR(255) NOT NULL,
    zip_code VARCHAR(255) NOT NULL,
    state_id VARCHAR(255) NOT NULL
);
`

func NewPostgresRepository(connStr string) (*PostgresRepository, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Strictly set connection limits as specified
	db.SetMaxOpenConns(appconfig.EnvInt("DB_MAX_OPEN_CONNS", 3))
	db.SetMaxIdleConns(appconfig.EnvInt("DB_MAX_IDLE_CONNS", 2))
	db.SetConnMaxLifetime(appconfig.EnvSeconds("DB_CONN_MAX_LIFETIME_SECONDS", 300))

	ctx, cancel := context.WithTimeout(context.Background(), appconfig.EnvSeconds("DB_CONNECT_TIMEOUT_SECONDS", 10))
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	if _, err := db.ExecContext(ctx, schemaDDL); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to execute schema DDL: %w", err)
	}

	return &PostgresRepository{db: db}, nil
}

func (r *PostgresRepository) Close() error {
	return r.db.Close()
}

func (r *PostgresRepository) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

func (r *PostgresRepository) ListOrders(ctx context.Context) ([]ExistingOrder, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, created_date
		FROM orders.orders
		ORDER BY created_date DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query orders failed: %w", err)
	}
	defer rows.Close()

	orders := make([]ExistingOrder, 0)
	for rows.Next() {
		var o ExistingOrder
		var createdAt time.Time
		if err := rows.Scan(&o.ID, &createdAt); err != nil {
			return nil, err
		}
		o.CreatedDate = createdAt.UTC().Format(time.RFC3339)
		orders = append(orders, o)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fetch items and shipping address for each order
	for i := range orders {
		orderID := orders[i].ID

		// Items
		itemRows, err := r.db.QueryContext(ctx, `
			SELECT id, product_id, quantity, unit_cost, total_cost
			FROM orders.order_items
			WHERE order_id = $1
		`, orderID)
		if err == nil {
			items := make([]OrderItem, 0)
			for itemRows.Next() {
				var item OrderItem
				if err := itemRows.Scan(&item.ID, &item.ProductID, &item.Quantity, &item.UnitCost, &item.TotalCost); err == nil {
					items = append(items, item)
				}
			}
			itemRows.Close()
			orders[i].Items = items
		}

		// Shipping address
		var addr ShippingAddress
		err = r.db.QueryRowContext(ctx, `
			SELECT first_name, last_name, email, address1, COALESCE(address2, ''), city, zip_code, state_id
			FROM orders.shipping_addresses
			WHERE order_id = $1
		`, orderID).Scan(
			&addr.FirstName, &addr.LastName, &addr.Email,
			&addr.Address1, &addr.Address2, &addr.City,
			&addr.ZipCode, &addr.State,
		)
		if err == nil {
			orders[i].ShippingAddress = addr
		}
	}

	return orders, nil
}

func (r *PostgresRepository) CreateOrder(ctx context.Context, order Order) (ExistingOrder, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ExistingOrder{}, fmt.Errorf("begin tx failed: %w", err)
	}
	defer tx.Rollback()

	orderID := uuid.New().String()
	var createdAt time.Time

	err = tx.QueryRowContext(ctx, `
		INSERT INTO orders.orders (id, customer_id, status, created_date)
		VALUES ($1, $2, 'COMPLETED', NOW())
		RETURNING created_date
	`, orderID, "api-user").Scan(&createdAt)
	if err != nil {
		return ExistingOrder{}, fmt.Errorf("insert order failed: %w", err)
	}

	// Insert items
	for _, item := range order.Items {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO orders.order_items (product_id, quantity, unit_cost, total_cost, order_id)
			VALUES ($1, $2, $3, $4, $5)
		`, item.ProductID, item.Quantity, item.UnitCost, item.TotalCost, orderID)
		if err != nil {
			return ExistingOrder{}, fmt.Errorf("insert item failed: %w", err)
		}
	}

	// Insert shipping address
	addr := order.ShippingAddress
	_, err = tx.ExecContext(ctx, `
		INSERT INTO orders.shipping_addresses (order_id, first_name, last_name, email, address1, address2, city, zip_code, state_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, orderID, addr.FirstName, addr.LastName, addr.Email, addr.Address1, addr.Address2, addr.City, addr.ZipCode, addr.State)
	if err != nil {
		return ExistingOrder{}, fmt.Errorf("insert address failed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return ExistingOrder{}, fmt.Errorf("commit tx failed: %w", err)
	}

	existing := ExistingOrder{
		ID:              orderID,
		CreatedDate:     createdAt.UTC().Format(time.RFC3339),
		ShippingAddress: order.ShippingAddress,
		Items:           make([]OrderItem, len(order.Items)),
	}
	copy(existing.Items, order.Items)

	return existing, nil
}

func (r *PostgresRepository) ProcessLight(ctx context.Context, orderID, jobID, customerID string) (bool, error) {
	if orderID == "" {
		orderID = uuid.New().String()
	}
	if customerID == "" {
		customerID = "cust-1"
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin tx failed: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO orders.orders (id, job_id, customer_id, status, created_date)
		VALUES ($1, $2, $3, 'COMPLETED', NOW())
		ON CONFLICT (job_id) DO NOTHING
	`, orderID, jobID, customerID)
	if err != nil {
		return false, fmt.Errorf("insert light order failed: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit light tx failed: %w", err)
	}

	return rowsAffected == 1, nil
}

func (r *PostgresRepository) ProcessFull(ctx context.Context, orderID, jobID, customerID string, order Order) (bool, error) {
	if orderID == "" {
		orderID = uuid.New().String()
	}
	if customerID == "" {
		customerID = "cust-1"
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin tx failed: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO orders.orders (id, job_id, customer_id, status, created_date)
		VALUES ($1, $2, $3, 'COMPLETED', NOW())
		ON CONFLICT (job_id) DO NOTHING
	`, orderID, jobID, customerID)
	if err != nil {
		return false, fmt.Errorf("insert full order failed: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	if rowsAffected == 0 {
		// Idempotency: message was already processed
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}

	// Insert items
	items := order.Items
	if len(items) == 0 {
		items = []OrderItem{
			{ProductID: "default-item", Quantity: 1, UnitCost: 1000, TotalCost: 1000},
		}
	}
	for _, item := range items {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO orders.order_items (product_id, quantity, unit_cost, total_cost, order_id)
			VALUES ($1, $2, $3, $4, $5)
		`, item.ProductID, item.Quantity, item.UnitCost, item.TotalCost, orderID)
		if err != nil {
			return false, fmt.Errorf("insert full order items failed: %w", err)
		}
	}

	// Insert shipping address
	addr := order.ShippingAddress
	if addr.FirstName == "" {
		addr = ShippingAddress{
			FirstName: "Default",
			LastName:  "Customer",
			Email:     "customer@example.com",
			Address1:  "123 Main St",
			City:      "Seattle",
			ZipCode:   "98101",
			State:     "WA",
		}
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO orders.shipping_addresses (order_id, first_name, last_name, email, address1, address2, city, zip_code, state_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, orderID, addr.FirstName, addr.LastName, addr.Email, addr.Address1, addr.Address2, addr.City, addr.ZipCode, addr.State)
	if err != nil {
		return false, fmt.Errorf("insert full shipping address failed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit full tx failed: %w", err)
	}

	return true, nil
}
