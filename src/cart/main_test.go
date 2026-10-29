package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aws-containers/retail-store-sample-app/cart/pkg/chaos"
	"github.com/aws-containers/retail-store-sample-app/cart/pkg/metrics"
	"github.com/aws-containers/retail-store-sample-app/cart/pkg/model"
	"github.com/aws-containers/retail-store-sample-app/cart/pkg/store"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSQSClient struct {
	batchResponse *sqs.SendMessageBatchOutput
	mu            sync.Mutex
	sentMessages  []*sqs.SendMessageInput
	sentBatches   []*sqs.SendMessageBatchInput
}

func (m *mockSQSClient) SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentMessages = append(m.sentMessages, params)
	return &sqs.SendMessageOutput{}, nil
}

func (m *mockSQSClient) SendMessageBatch(ctx context.Context, params *sqs.SendMessageBatchInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentBatches = append(m.sentBatches, params)
	if m.batchResponse != nil {
		return m.batchResponse, nil
	}
	out := &sqs.SendMessageBatchOutput{}
	for _, entry := range params.Entries {
		out.Successful = append(out.Successful, sqstypes.SendMessageBatchResultEntry{Id: entry.Id})
	}
	return out, nil
}

func (m *mockSQSClient) ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	return &sqs.ReceiveMessageOutput{}, nil
}

func (m *mockSQSClient) DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	return &sqs.DeleteMessageOutput{}, nil
}

func (m *mockSQSClient) ChangeMessageVisibility(ctx context.Context, params *sqs.ChangeMessageVisibilityInput, optFns ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error) {
	return &sqs.ChangeMessageVisibilityOutput{}, nil
}

func setupTestApp() (*store.MemoryStore, *mockSQSClient, http.Handler, *chaos.Controller) {
	st := store.NewMemoryStore()
	mockSQS := &mockSQSClient{}
	cfg := AppConfig{
		AppMode:   "all",
		Port:      "8080",
		Service:   "cart",
		QueueName: "cart-queue",
		QueueURL:  "http://test-queue/cart-queue",
	}
	reg := prometheus.NewRegistry()
	m := metrics.NewMetrics(reg)
	cc := chaos.NewController()

	r := SetupRouter(st, mockSQS, cfg, m, cc)
	return st, mockSQS, r, cc
}

func TestCartEndpoints(t *testing.T) {
	_, _, router, _ := setupTestApp()

	// 1. GET /carts/user-1 -> 200 OK (empty)
	req := httptest.NewRequest(http.MethodGet, "/carts/user-1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var cart model.Cart
	err := json.Unmarshal(w.Body.Bytes(), &cart)
	require.NoError(t, err)
	assert.Equal(t, "user-1", cart.CustomerID)
	assert.Empty(t, cart.Items)

	// 2. POST /carts/user-1/items -> 201 Created
	itemPayload, _ := json.Marshal(model.Item{
		ItemID:    "item-100",
		Quantity:  2,
		UnitPrice: 250,
	})
	req = httptest.NewRequest(http.MethodPost, "/carts/user-1/items", bytes.NewBuffer(itemPayload))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	var createdItem model.Item
	err = json.Unmarshal(w.Body.Bytes(), &createdItem)
	require.NoError(t, err)
	assert.Equal(t, "item-100", createdItem.ItemID)
	assert.Equal(t, 2, createdItem.Quantity)

	// 3. GET /carts/user-1/items -> 200 OK array
	req = httptest.NewRequest(http.MethodGet, "/carts/user-1/items", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var items []model.Item
	err = json.Unmarshal(w.Body.Bytes(), &items)
	require.NoError(t, err)
	assert.Len(t, items, 1)

	// 4. GET /carts/user-1/items/item-100 -> 200 OK
	req = httptest.NewRequest(http.MethodGet, "/carts/user-1/items/item-100", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 5. GET /carts/user-1/items/non-existent -> 404 Not Found
	req = httptest.NewRequest(http.MethodGet, "/carts/user-1/items/unknown", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 6. PATCH /carts/user-1/items -> 202 Accepted
	patchPayload, _ := json.Marshal(model.Item{
		ItemID:    "item-100",
		Quantity:  5,
		UnitPrice: 300,
	})
	req = httptest.NewRequest(http.MethodPatch, "/carts/user-1/items", bytes.NewBuffer(patchPayload))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)

	// Verify update took effect
	req = httptest.NewRequest(http.MethodGet, "/carts/user-1/items/item-100", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var updatedItem model.Item
	json.Unmarshal(w.Body.Bytes(), &updatedItem)
	assert.Equal(t, 5, updatedItem.Quantity)
	assert.Equal(t, 300, updatedItem.UnitPrice)

	// 7. Test Merge: Add item to session cart
	sessionItem, _ := json.Marshal(model.Item{
		ItemID:    "item-200",
		Quantity:  1,
		UnitPrice: 99,
	})
	req = httptest.NewRequest(http.MethodPost, "/carts/session-temp/items", bytes.NewBuffer(sessionItem))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	// Merge session-temp into user-1 -> 202 Accepted
	req = httptest.NewRequest(http.MethodGet, "/carts/user-1/merge?sessionId=session-temp", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)

	// Verify user-1 now has 2 items
	req = httptest.NewRequest(http.MethodGet, "/carts/user-1/items", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	json.Unmarshal(w.Body.Bytes(), &items)
	assert.Len(t, items, 2)

	// 8. DELETE /carts/user-1/items/item-200 -> 202 Accepted
	req = httptest.NewRequest(http.MethodDelete, "/carts/user-1/items/item-200", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)

	// 9. DELETE /carts/user-1 -> 202 Accepted
	req = httptest.NewRequest(http.MethodDelete, "/carts/user-1", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)

	var emptyCart model.Cart
	json.Unmarshal(w.Body.Bytes(), &emptyCart)
	assert.Empty(t, emptyCart.Items)
}

func TestJobsProducer(t *testing.T) {
	_, mockSQS, router, _ := setupTestApp()

	// 1. Validation error: workUnits > 10000
	invalidUnits, _ := json.Marshal(model.JobRequest{
		WorkUnits: 20000,
		BatchSize: 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBuffer(invalidUnits))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 2. Validation error: batchSize > 10
	invalidBatch, _ := json.Marshal(model.JobRequest{
		WorkUnits: 100,
		BatchSize: 15,
	})
	req = httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBuffer(invalidBatch))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 3. Validation error: invalid dbMode
	invalidDBMode, _ := json.Marshal(model.JobRequest{
		WorkUnits: 100,
		BatchSize: 1,
		DBMode:    "unsupported",
	})
	req = httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBuffer(invalidDBMode))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 4. Valid single job (batchSize == 1)
	validSingle, _ := json.Marshal(model.JobRequest{
		WorkUnits:  50,
		BatchSize:  1,
		DBMode:     "light",
		CustomerID: "cust-benchmark",
	})
	req = httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBuffer(validSingle))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	var resp model.JobResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "published", resp.Status)
	assert.Equal(t, 1, resp.Count)
	assert.Len(t, resp.JobIDs, 1)

	mockSQS.mu.Lock()
	assert.Len(t, mockSQS.sentMessages, 1)
	mockSQS.mu.Unlock()

	// 5. Valid batch job (batchSize == 3)
	validBatch, _ := json.Marshal(model.JobRequest{
		WorkUnits:  20,
		BatchSize:  3,
		DBMode:     "full",
		CustomerID: "cust-batch",
	})
	req = httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBuffer(validBatch))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, 3, resp.Count)
	assert.Len(t, resp.JobIDs, 3)

	mockSQS.mu.Lock()
	assert.Len(t, mockSQS.sentBatches, 1)
	assert.Len(t, mockSQS.sentBatches[0].Entries, 3)
	mockSQS.mu.Unlock()
}

func TestHealthActuatorAndChaos(t *testing.T) {
	_, _, router, cc := setupTestApp()

	// 1. Health checks
	healthEndpoints := []string{"/health", "/actuator/health", "/actuator/health/liveness", "/actuator/health/readiness"}
	for _, ep := range healthEndpoints {
		req := httptest.NewRequest(http.MethodGet, ep, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"UP"`)
	}

	// 2. Metrics endpoint
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 3. Chaos routes
	// Disable health
	req = httptest.NewRequest(http.MethodPost, "/chaos/health", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, cc.IsHealthy())

	// Verify health endpoint now returns 503
	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	// Restore health
	req = httptest.NewRequest(http.MethodDelete, "/chaos/health", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Inject error status 418
	req = httptest.NewRequest(http.MethodPost, "/chaos/status/418", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/carts/user-test", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTeapot, w.Code)

	// Clear error status
	req = httptest.NewRequest(http.MethodDelete, "/chaos/status", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/carts/user-test", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Latency endpoints
	req = httptest.NewRequest(http.MethodPost, "/chaos/latency/20", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/chaos/status", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest(http.MethodDelete, "/chaos/latency", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestPostgresStoreWithSqlmock(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

	// 1. Initial migrations expectations
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

	pgStore, err := store.NewPostgresStore(db)
	require.NoError(t, err)

	ctx := context.Background()

	// 2. AddItem
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO cart.carts (customer_id, updated_at) VALUES ($1, NOW())
		ON CONFLICT (customer_id) DO UPDATE SET updated_at = NOW()`).
		WithArgs("cust-pg").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`INSERT INTO cart.cart_items (customer_id, item_id, quantity, unit_price)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (customer_id, item_id)
		DO UPDATE SET quantity = cart.cart_items.quantity + EXCLUDED.quantity, unit_price = EXCLUDED.unit_price
		RETURNING quantity, unit_price`).
		WithArgs("cust-pg", "item-1", 2, 100).
		WillReturnRows(sqlmock.NewRows([]string{"quantity", "unit_price"}).AddRow(2, 100))
	mock.ExpectCommit()

	item, err := pgStore.AddItem(ctx, "cust-pg", model.Item{
		ItemID:    "item-1",
		Quantity:  2,
		UnitPrice: 100,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, item.Quantity)

	// 3. GetItem
	mock.ExpectQuery(`SELECT item_id, quantity, unit_price FROM cart.cart_items WHERE customer_id = $1 AND item_id = $2`).
		WithArgs("cust-pg", "item-1").
		WillReturnRows(sqlmock.NewRows([]string{"item_id", "quantity", "unit_price"}).AddRow("item-1", 2, 100))

	it, err := pgStore.GetItem(ctx, "cust-pg", "item-1")
	require.NoError(t, err)
	assert.Equal(t, "item-1", it.ItemID)

	// 4. UpdateItem
	mock.ExpectExec(`UPDATE cart.cart_items SET quantity = $1, unit_price = $2 WHERE customer_id = $3 AND item_id = $4`).
		WithArgs(4, 150, "cust-pg", "item-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = pgStore.UpdateItem(ctx, "cust-pg", model.Item{
		ItemID:    "item-1",
		Quantity:  4,
		UnitPrice: 150,
	})
	require.NoError(t, err)

	// 5. DeleteItem
	mock.ExpectExec(`DELETE FROM cart.cart_items WHERE customer_id = $1 AND item_id = $2`).
		WithArgs("cust-pg", "item-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = pgStore.DeleteItem(ctx, "cust-pg", "item-1")
	require.NoError(t, err)

	// 6. DeleteCart
	mock.ExpectExec(`DELETE FROM cart.carts WHERE customer_id = $1`).
		WithArgs("cust-pg").
		WillReturnResult(sqlmock.NewResult(0, 1))

	deletedCart, err := pgStore.DeleteCart(ctx, "cust-pg")
	require.NoError(t, err)
	assert.Equal(t, "cust-pg", deletedCart.CustomerID)

	// 7. ProcessJob light mode
	mock.ExpectExec(`INSERT INTO cart.processed_jobs (job_id, customer_id, status, created_at)
			VALUES ($1, $2, 'COMPLETED', NOW())
			ON CONFLICT (job_id) DO NOTHING`).
		WithArgs("job-pg-1", "cust-pg").
		WillReturnResult(sqlmock.NewResult(1, 1))

	isNew, err := pgStore.ProcessJob(ctx, model.JobMessage{
		JobID:      "job-pg-1",
		CustomerID: "cust-pg",
		DBMode:     "light",
	})
	require.NoError(t, err)
	assert.True(t, isNew)

	// Verify all mock expectations were met
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLoadConfig(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		t.Setenv("APP_MODE", "")
		t.Setenv("PORT", "")
		t.Setenv("SERVICE_NAME", "")
		t.Setenv("SQS_QUEUE_NAME", "")
		t.Setenv("SQS_QUEUE_URL", "")
		t.Setenv("SQS_MAX_MESSAGES", "")
		t.Setenv("WORKER_CONCURRENCY", "")
		t.Setenv("WORKER_DB_MODE", "")
		t.Setenv("AWS_REGION", "")
		t.Setenv("AWS_DEFAULT_REGION", "")
		t.Setenv("AWS_ENDPOINT_URL", "")
		t.Setenv("LOCALSTACK_ENDPOINT", "")

		cfg := loadConfig()
		assert.Equal(t, "api", cfg.AppMode)
		assert.Equal(t, "8080", cfg.Port)
		assert.Equal(t, "cart", cfg.Service)
		assert.Equal(t, "cart-queue", cfg.QueueName)
		assert.Empty(t, cfg.QueueURL)
		assert.Equal(t, int32(1), cfg.MaxMessages)
		assert.Equal(t, 1, cfg.WorkerConcurrency)
		assert.Equal(t, "light", cfg.DBMode)
		assert.Empty(t, cfg.AWSRegion)
		assert.Equal(t, "", cfg.AWSEndpointURL)
	})

	t.Run("custom environment variables", func(t *testing.T) {
		t.Setenv("APP_MODE", "worker")
		t.Setenv("PORT", "9090")
		t.Setenv("SERVICE_NAME", "custom-cart")
		t.Setenv("SQS_QUEUE_NAME", "custom-queue")
		t.Setenv("SQS_QUEUE_URL", "https://sqs.us-west-2.amazonaws.com/12345/custom-queue")
		t.Setenv("SQS_MAX_MESSAGES", "8")
		t.Setenv("WORKER_CONCURRENCY", "4")
		t.Setenv("WORKER_DB_MODE", "full")
		t.Setenv("AWS_REGION", "us-west-2")
		t.Setenv("LOCALSTACK_ENDPOINT", "http://localhost:4566")

		cfg := loadConfig()
		assert.Equal(t, "worker", cfg.AppMode)
		assert.Equal(t, "9090", cfg.Port)
		assert.Equal(t, "custom-cart", cfg.Service)
		assert.Equal(t, "custom-queue", cfg.QueueName)
		assert.Equal(t, "https://sqs.us-west-2.amazonaws.com/12345/custom-queue", cfg.QueueURL)
		assert.Equal(t, int32(8), cfg.MaxMessages)
		assert.Equal(t, 4, cfg.WorkerConcurrency)
		assert.Equal(t, "full", cfg.DBMode)
		assert.Equal(t, "us-west-2", cfg.AWSRegion)
		assert.Equal(t, "http://localhost:4566", cfg.AWSEndpointURL)

		// AWS_ENDPOINT_URL takes precedence over LOCALSTACK_ENDPOINT
		t.Setenv("AWS_ENDPOINT_URL", "http://custom-endpoint:4566")
		cfg = loadConfig()
		assert.Equal(t, "http://custom-endpoint:4566", cfg.AWSEndpointURL)

		// Invalid SQS_MAX_MESSAGES falls back to default 1
		t.Setenv("SQS_MAX_MESSAGES", "99")
		cfg = loadConfig()
		assert.Equal(t, int32(1), cfg.MaxMessages)

		// Invalid WORKER_CONCURRENCY falls back to default 1
		t.Setenv("WORKER_CONCURRENCY", "0")
		cfg = loadConfig()
		assert.Equal(t, 1, cfg.WorkerConcurrency)
	})
}

func TestCartRoutesEdgeCases(t *testing.T) {
	// 1. GET /carts/user-1/merge without sessionId query param -> 400 Bad Request
	_, _, router, _ := setupTestApp()

	req := httptest.NewRequest(http.MethodGet, "/carts/user-1/merge", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "sessionId query parameter required")

	// 2. POST /carts/user-1/items with invalid JSON -> 400 Bad Request
	req = httptest.NewRequest(http.MethodPost, "/carts/user-1/items", bytes.NewBufferString("not-a-valid-json"))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 3. PATCH /carts/user-1/items with invalid JSON -> 400 Bad Request
	req = httptest.NewRequest(http.MethodPatch, "/carts/user-1/items", bytes.NewBufferString("not-a-valid-json"))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 4. PATCH /carts/user-1/items for non-existent item -> 404 Not Found
	nonExistentItem, _ := json.Marshal(model.Item{
		ItemID:    "item-does-not-exist",
		Quantity:  1,
		UnitPrice: 100,
	})
	req = httptest.NewRequest(http.MethodPatch, "/carts/user-1/items", bytes.NewBuffer(nonExistentItem))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "item not found")

	// 5. SetupRouter in AppMode: "worker" -> /health returns 200, /carts/user-1 returns 404
	workerCfg := AppConfig{
		AppMode: "worker",
		Port:    "8080",
		Service: "cart",
	}
	st := store.NewMemoryStore()
	reg := prometheus.NewRegistry()
	m := metrics.NewMetrics(reg)
	cc := chaos.NewController()
	workerRouter := SetupRouter(st, nil, workerCfg, m, cc)

	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	w = httptest.NewRecorder()
	workerRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/carts/user-1", nil)
	w = httptest.NewRecorder()
	workerRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 6. POST /jobs when sqsClient is nil -> 500 Internal Server Error
	apiCfg := AppConfig{
		AppMode:   "api",
		Port:      "8080",
		Service:   "cart",
		QueueName: "cart-queue",
		QueueURL:  "http://test-queue/cart-queue",
	}
	nilSqsRouter := SetupRouter(st, nil, apiCfg, m, cc)

	jobReq, _ := json.Marshal(model.JobRequest{
		BatchSize: 1,
		WorkUnits: 10,
		DBMode:    "light",
	})
	req = httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBuffer(jobReq))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	nilSqsRouter.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "sqs client not available")
}

func TestInitStore(t *testing.T) {
	t.Run("default to MemoryStore", func(t *testing.T) {
		t.Setenv("RETAIL_CART_PERSISTENCE_PROVIDER", "")
		t.Setenv("DB_HOST", "")
		t.Setenv("DATABASE_URL", "")

		st, err := InitStore()
		require.NoError(t, err)
		assert.IsType(t, &store.MemoryStore{}, st)
	})
}

func TestBatchAPIReportsConfirmedAndFailedJobs(t *testing.T) {
	_, client, router, _ := setupTestApp()
	client.batchResponse = &sqs.SendMessageBatchOutput{Successful: []sqstypes.SendMessageBatchResultEntry{{Id: ptr("msg-0")}}, Failed: []sqstypes.BatchResultErrorEntry{{Id: ptr("msg-1"), SenderFault: true}}}
	req := httptest.NewRequest("POST", "/jobs", bytes.NewBufferString(`{"batchSize":2,"workUnits":1}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 502, w.Code)
	var out struct {
		Count  int      `json:"count"`
		JobIDs []string `json:"jobIds"`
		Failed []string `json:"failedJobIds"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Equal(t, 1, out.Count)
	require.Len(t, out.JobIDs, 1)
	require.Len(t, out.Failed, 1)
}
func ptr(s string) *string { return &s }
