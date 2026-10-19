package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

var (
	mockMu       sync.Mutex
	mockRegistry = make(map[string]*mockConn)
)

type mockDriver struct{}

func (d *mockDriver) Open(name string) (driver.Conn, error) {
	mockMu.Lock()
	defer mockMu.Unlock()
	conn, ok := mockRegistry[name]
	if !ok {
		return nil, fmt.Errorf("mock connection not found for DSN: %s", name)
	}
	return conn, nil
}

func init() {
	sql.Register("mock_postgres", &mockDriver{})
}

type mockConn struct {
	queryFn    func(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error)
	execFn     func(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error)
	pingFn     func(ctx context.Context) error
	closeFn    func() error
	beginFn    func() error
	commitFn   func() error
	rollbackFn func() error
}

func (c *mockConn) Prepare(query string) (driver.Stmt, error) {
	return &mockStmt{conn: c, query: query}, nil
}

func (c *mockConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	return &mockStmt{conn: c, query: query}, nil
}

func (c *mockConn) Close() error {
	if c.closeFn != nil {
		return c.closeFn()
	}
	return nil
}

func (c *mockConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *mockConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if c.beginFn != nil {
		if err := c.beginFn(); err != nil {
			return nil, err
		}
	}
	return &mockTx{conn: c}, nil
}

func (c *mockConn) Ping(ctx context.Context) error {
	if c.pingFn != nil {
		return c.pingFn(ctx)
	}
	return nil
}

func (c *mockConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.execFn != nil {
		return c.execFn(ctx, query, args)
	}
	return driver.RowsAffected(1), nil
}

func (c *mockConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.queryFn != nil {
		return c.queryFn(ctx, query, args)
	}
	return nil, fmt.Errorf("unhandled mock query: %s", query)
}

type mockStmt struct {
	conn  *mockConn
	query string
}

func (s *mockStmt) Close() error  { return nil }
func (s *mockStmt) NumInput() int { return -1 }

func (s *mockStmt) Exec(args []driver.Value) (driver.Result, error) {
	named := make([]driver.NamedValue, len(args))
	for i, a := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: a}
	}
	return s.conn.ExecContext(context.Background(), s.query, named)
}

func (s *mockStmt) Query(args []driver.Value) (driver.Rows, error) {
	named := make([]driver.NamedValue, len(args))
	for i, a := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: a}
	}
	return s.conn.QueryContext(context.Background(), s.query, named)
}

func (s *mockStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.conn.ExecContext(ctx, s.query, args)
}

func (s *mockStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.conn.QueryContext(ctx, s.query, args)
}

type mockTx struct {
	conn *mockConn
}

func (tx *mockTx) Commit() error {
	if tx.conn.commitFn != nil {
		return tx.conn.commitFn()
	}
	return nil
}

func (tx *mockTx) Rollback() error {
	if tx.conn.rollbackFn != nil {
		return tx.conn.rollbackFn()
	}
	return nil
}

type mockRows struct {
	columns []string
	values  [][]driver.Value
	idx     int
}

func newMockRows(cols []string, rows ...[]driver.Value) *mockRows {
	return &mockRows{
		columns: cols,
		values:  rows,
	}
}

func (r *mockRows) Columns() []string {
	return r.columns
}

func (r *mockRows) Close() error {
	return nil
}

func (r *mockRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.values) {
		return io.EOF
	}
	row := r.values[r.idx]
	r.idx++
	for i, val := range row {
		dest[i] = val
	}
	return nil
}

func setupMockRepo(t *testing.T, conn *mockConn) (*PostgresRepository, func()) {
	id := uuid.New().String()
	mockMu.Lock()
	mockRegistry[id] = conn
	mockMu.Unlock()

	db, err := sql.Open("mock_postgres", id)
	if err != nil {
		t.Fatalf("failed to open mock db: %v", err)
	}

	repo := &PostgresRepository{db: db}
	cleanup := func() {
		db.Close()
		mockMu.Lock()
		delete(mockRegistry, id)
		mockMu.Unlock()
	}

	return repo, cleanup
}

func TestPostgresRepository_PingAndClose(t *testing.T) {
	pingCalled := false
	closeCalled := false

	conn := &mockConn{
		pingFn: func(ctx context.Context) error {
			pingCalled = true
			return nil
		},
		closeFn: func() error {
			closeCalled = true
			return nil
		},
	}

	repo, cleanup := setupMockRepo(t, conn)
	defer cleanup()

	ctx := context.Background()
	if err := repo.Ping(ctx); err != nil {
		t.Errorf("Ping failed: %v", err)
	}
	if !pingCalled {
		t.Errorf("expected Ping to be called on underlying connection")
	}

	if err := repo.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
	if !closeCalled {
		t.Errorf("expected Close to be called on underlying connection")
	}
}

func TestPostgresRepository_PingError(t *testing.T) {
	expectedErr := errors.New("db unreachable")
	conn := &mockConn{
		pingFn: func(ctx context.Context) error {
			return expectedErr
		},
	}

	repo, cleanup := setupMockRepo(t, conn)
	defer cleanup()

	if err := repo.Ping(context.Background()); !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

func TestPostgresRepository_ListOrders(t *testing.T) {
	fixedTime := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	conn := &mockConn{
		queryFn: func(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
			if strings.Contains(query, "orders.orders") {
				return newMockRows(
					[]string{"id", "created_date"},
					[]driver.Value{"ord-1", fixedTime},
				), nil
			}
			if strings.Contains(query, "orders.order_items") {
				return newMockRows(
					[]string{"id", "product_id", "quantity", "unit_cost", "total_cost"},
					[]driver.Value{int64(1), "prod-100", int64(2), int64(50), int64(100)},
				), nil
			}
			if strings.Contains(query, "orders.shipping_addresses") {
				return newMockRows(
					[]string{"first_name", "last_name", "email", "address1", "address2", "city", "zip_code", "state_id"},
					[]driver.Value{"John", "Doe", "john@example.com", "456 Elm St", "Suite 2", "Portland", "97201", "OR"},
				), nil
			}
			return nil, fmt.Errorf("unexpected query: %s", query)
		},
	}

	repo, cleanup := setupMockRepo(t, conn)
	defer cleanup()

	orders, err := repo.ListOrders(context.Background())
	if err != nil {
		t.Fatalf("ListOrders failed: %v", err)
	}

	if len(orders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(orders))
	}

	o := orders[0]
	if o.ID != "ord-1" {
		t.Errorf("expected ID 'ord-1', got %q", o.ID)
	}
	if o.CreatedDate != fixedTime.Format(time.RFC3339) {
		t.Errorf("expected created_date %s, got %s", fixedTime.Format(time.RFC3339), o.CreatedDate)
	}

	if len(o.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(o.Items))
	}
	if o.Items[0].ProductID != "prod-100" || o.Items[0].Quantity != 2 || o.Items[0].TotalCost != 100 {
		t.Errorf("unexpected item contents: %+v", o.Items[0])
	}

	if o.ShippingAddress.FirstName != "John" || o.ShippingAddress.State != "OR" || o.ShippingAddress.Address2 != "Suite 2" {
		t.Errorf("unexpected shipping address contents: %+v", o.ShippingAddress)
	}
}

func TestPostgresRepository_ListOrders_QueryError(t *testing.T) {
	conn := &mockConn{
		queryFn: func(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("query failed")
		},
	}

	repo, cleanup := setupMockRepo(t, conn)
	defer cleanup()

	_, err := repo.ListOrders(context.Background())
	if err == nil {
		t.Fatalf("expected error from ListOrders when query fails, got nil")
	}
}

func TestPostgresRepository_CreateOrder(t *testing.T) {
	fixedTime := time.Date(2026, 10, 19, 11, 0, 0, 0, time.UTC)
	var executedQueries []string
	var committed bool

	conn := &mockConn{
		queryFn: func(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
			executedQueries = append(executedQueries, query)
			if strings.Contains(query, "INSERT INTO orders.orders") {
				return newMockRows(
					[]string{"created_date"},
					[]driver.Value{fixedTime},
				), nil
			}
			return nil, fmt.Errorf("unexpected query in CreateOrder: %s", query)
		},
		execFn: func(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
			executedQueries = append(executedQueries, query)
			return driver.RowsAffected(1), nil
		},
		commitFn: func() error {
			committed = true
			return nil
		},
	}

	repo, cleanup := setupMockRepo(t, conn)
	defer cleanup()

	newOrder := Order{
		ShippingAddress: ShippingAddress{
			FirstName: "Jane",
			LastName:  "Doe",
			Email:     "jane@example.com",
			Address1:  "789 Oak Ave",
			City:      "Austin",
			ZipCode:   "73301",
			State:     "TX",
		},
		Items: []OrderItem{
			{ProductID: "item-1", Quantity: 3, UnitCost: 200, TotalCost: 600},
			{ProductID: "item-2", Quantity: 1, UnitCost: 400, TotalCost: 400},
		},
	}

	created, err := repo.CreateOrder(context.Background(), newOrder)
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}

	if created.ID == "" {
		t.Errorf("expected created order to have non-empty ID")
	}
	if created.CreatedDate != fixedTime.Format(time.RFC3339) {
		t.Errorf("expected created_date %s, got %s", fixedTime.Format(time.RFC3339), created.CreatedDate)
	}
	if len(created.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(created.Items))
	}
	if created.ShippingAddress.City != "Austin" {
		t.Errorf("expected shipping address city 'Austin', got %q", created.ShippingAddress.City)
	}
	if !committed {
		t.Errorf("expected transaction to be committed")
	}
}

func TestPostgresRepository_CreateOrder_InsertError(t *testing.T) {
	var rolledBack bool
	conn := &mockConn{
		queryFn: func(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
			return nil, errors.New("insert failed")
		},
		rollbackFn: func() error {
			rolledBack = true
			return nil
		},
	}

	repo, cleanup := setupMockRepo(t, conn)
	defer cleanup()

	_, err := repo.CreateOrder(context.Background(), Order{})
	if err == nil {
		t.Fatalf("expected error from CreateOrder when insert fails, got nil")
	}
	if !rolledBack {
		t.Errorf("expected transaction rollback on error")
	}
}

func TestPostgresRepository_ProcessLight(t *testing.T) {
	t.Run("RowsAffected == 1 returns true", func(t *testing.T) {
		committed := false
		conn := &mockConn{
			execFn: func(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
				return driver.RowsAffected(1), nil
			},
			commitFn: func() error {
				committed = true
				return nil
			},
		}

		repo, cleanup := setupMockRepo(t, conn)
		defer cleanup()

		isNew, err := repo.ProcessLight(context.Background(), "ord-1", "job-1", "cust-1")
		if err != nil {
			t.Fatalf("ProcessLight failed: %v", err)
		}
		if !isNew {
			t.Errorf("expected isNew=true when RowsAffected==1")
		}
		if !committed {
			t.Errorf("expected transaction commit")
		}
	})

	t.Run("RowsAffected == 0 returns false (duplicate)", func(t *testing.T) {
		committed := false
		conn := &mockConn{
			execFn: func(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
				return driver.RowsAffected(0), nil
			},
			commitFn: func() error {
				committed = true
				return nil
			},
		}

		repo, cleanup := setupMockRepo(t, conn)
		defer cleanup()

		isNew, err := repo.ProcessLight(context.Background(), "ord-1", "job-1", "cust-1")
		if err != nil {
			t.Fatalf("ProcessLight failed: %v", err)
		}
		if isNew {
			t.Errorf("expected isNew=false when RowsAffected==0 (duplicate)")
		}
		if !committed {
			t.Errorf("expected transaction commit even on duplicate")
		}
	})

	t.Run("Exec error returns error", func(t *testing.T) {
		rolledBack := false
		conn := &mockConn{
			execFn: func(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
				return nil, errors.New("exec error")
			},
			rollbackFn: func() error {
				rolledBack = true
				return nil
			},
		}

		repo, cleanup := setupMockRepo(t, conn)
		defer cleanup()

		isNew, err := repo.ProcessLight(context.Background(), "ord-1", "job-1", "cust-1")
		if err == nil {
			t.Fatalf("expected error from ProcessLight on exec failure")
		}
		if isNew {
			t.Errorf("expected isNew=false on error")
		}
		if !rolledBack {
			t.Errorf("expected transaction rollback on error")
		}
	})
}

func TestPostgresRepository_ProcessFull(t *testing.T) {
	t.Run("RowsAffected == 0 returns false duplicate without inserting items", func(t *testing.T) {
		execCount := 0
		committed := false

		conn := &mockConn{
			execFn: func(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
				execCount++
				return driver.RowsAffected(0), nil
			},
			commitFn: func() error {
				committed = true
				return nil
			},
		}

		repo, cleanup := setupMockRepo(t, conn)
		defer cleanup()

		isNew, err := repo.ProcessFull(context.Background(), "ord-1", "job-dup", "cust-1", Order{})
		if err != nil {
			t.Fatalf("ProcessFull duplicate failed: %v", err)
		}
		if isNew {
			t.Errorf("expected isNew=false for duplicate")
		}
		if execCount != 1 {
			t.Errorf("expected only 1 exec (order insert) for duplicate, got %d", execCount)
		}
		if !committed {
			t.Errorf("expected commit on duplicate")
		}
	})

	t.Run("RowsAffected == 1 with default items and default address", func(t *testing.T) {
		var executedQueries []string
		committed := false

		conn := &mockConn{
			execFn: func(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
				executedQueries = append(executedQueries, query)
				return driver.RowsAffected(1), nil
			},
			commitFn: func() error {
				committed = true
				return nil
			},
		}

		repo, cleanup := setupMockRepo(t, conn)
		defer cleanup()

		isNew, err := repo.ProcessFull(context.Background(), "ord-1", "job-new", "cust-1", Order{})
		if err != nil {
			t.Fatalf("ProcessFull failed: %v", err)
		}
		if !isNew {
			t.Errorf("expected isNew=true when RowsAffected==1")
		}
		// Expect 3 execs: order insert, default item insert, default address insert
		if len(executedQueries) != 3 {
			t.Errorf("expected 3 exec queries, got %d: %v", len(executedQueries), executedQueries)
		}
		if !committed {
			t.Errorf("expected transaction commit")
		}
	})

	t.Run("RowsAffected == 1 with custom items and shipping address", func(t *testing.T) {
		var executedQueries []string
		committed := false

		conn := &mockConn{
			execFn: func(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
				executedQueries = append(executedQueries, query)
				return driver.RowsAffected(1), nil
			},
			commitFn: func() error {
				committed = true
				return nil
			},
		}

		repo, cleanup := setupMockRepo(t, conn)
		defer cleanup()

		customOrder := Order{
			ShippingAddress: ShippingAddress{
				FirstName: "Alice",
				LastName:  "Wonder",
				Email:     "alice@example.com",
				Address1:  "1 Rabbit Hole",
				City:      "Wonderland",
				ZipCode:   "00000",
				State:     "WL",
			},
			Items: []OrderItem{
				{ProductID: "tea", Quantity: 1, UnitCost: 50, TotalCost: 50},
				{ProductID: "cake", Quantity: 2, UnitCost: 100, TotalCost: 200},
			},
		}

		isNew, err := repo.ProcessFull(context.Background(), "ord-custom", "job-custom", "cust-alice", customOrder)
		if err != nil {
			t.Fatalf("ProcessFull custom failed: %v", err)
		}
		if !isNew {
			t.Errorf("expected isNew=true")
		}
		// Expect 4 execs: order insert, 2 item inserts, 1 address insert
		if len(executedQueries) != 4 {
			t.Errorf("expected 4 exec queries (order + 2 items + address), got %d: %v", len(executedQueries), executedQueries)
		}
		if !committed {
			t.Errorf("expected transaction commit")
		}
	})
}
