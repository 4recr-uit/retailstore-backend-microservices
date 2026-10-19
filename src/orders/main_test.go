package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqsTypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/aws-containers/retail-store-sample-app/orders/pkg/api"
	"github.com/aws-containers/retail-store-sample-app/orders/pkg/config"
	"github.com/aws-containers/retail-store-sample-app/orders/pkg/db"
	"github.com/aws-containers/retail-store-sample-app/orders/pkg/worker"
)

// MockSQS implements worker.SQSClient
type MockSQS struct {
	mu                   sync.Mutex
	Messages             []sqsTypes.Message
	DeletedReceipts      []string
	VisibilityChanged    []string
	VisibilityTimeoutVal int32
	SentMessages         []string
	SentBatchCount       int
}

func NewMockSQS() *MockSQS {
	return &MockSQS{
		Messages:          make([]sqsTypes.Message, 0),
		DeletedReceipts:   make([]string, 0),
		VisibilityChanged: make([]string, 0),
		SentMessages:      make([]string, 0),
	}
}

func (m *MockSQS) ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if len(m.Messages) == 0 {
		return &sqs.ReceiveMessageOutput{Messages: []sqsTypes.Message{}}, nil
	}

	max := int(params.MaxNumberOfMessages)
	if max > len(m.Messages) {
		max = len(m.Messages)
	}

	res := m.Messages[:max]
	m.Messages = m.Messages[max:]

	return &sqs.ReceiveMessageOutput{Messages: res}, nil
}

func (m *MockSQS) DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DeletedReceipts = append(m.DeletedReceipts, aws.ToString(params.ReceiptHandle))
	return &sqs.DeleteMessageOutput{}, nil
}

func (m *MockSQS) ChangeMessageVisibility(ctx context.Context, params *sqs.ChangeMessageVisibilityInput, optFns ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.VisibilityChanged = append(m.VisibilityChanged, aws.ToString(params.ReceiptHandle))
	m.VisibilityTimeoutVal = params.VisibilityTimeout
	return &sqs.ChangeMessageVisibilityOutput{}, nil
}

func (m *MockSQS) SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SentMessages = append(m.SentMessages, aws.ToString(params.MessageBody))
	return &sqs.SendMessageOutput{MessageId: aws.String("msg-1")}, nil
}

func (m *MockSQS) SendMessageBatch(ctx context.Context, params *sqs.SendMessageBatchInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SentBatchCount += len(params.Entries)
	for _, entry := range params.Entries {
		m.SentMessages = append(m.SentMessages, aws.ToString(entry.MessageBody))
	}
	return &sqs.SendMessageBatchOutput{}, nil
}

// Test CPU burn calibration
func TestBurnCPU(t *testing.T) {
	start := time.Now()
	worker.BurnCPU(20) // 20ms
	elapsed := time.Since(start)

	if elapsed < 18*time.Millisecond {
		t.Errorf("BurnCPU(20) elapsed too quickly: %v", elapsed)
	}
}

// Test InMemoryRepository CRUD and idempotency
func TestInMemoryRepository(t *testing.T) {
	ctx := context.Background()
	repo := db.NewInMemoryRepository()

	// 1. Create order
	created, err := repo.CreateOrder(ctx, db.Order{
		ShippingAddress: db.ShippingAddress{FirstName: "Alice", City: "Wonderland"},
		Items: []db.OrderItem{
			{ProductID: "prod-1", Quantity: 2, UnitCost: 50, TotalCost: 100},
		},
	})
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}
	if created.ID == "" || created.CreatedDate == "" {
		t.Fatalf("Created order missing fields: %+v", created)
	}

	// 2. List orders
	orders, err := repo.ListOrders(ctx)
	if err != nil {
		t.Fatalf("ListOrders failed: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("Expected 1 order, got %d", len(orders))
	}

	// 3. ProcessLight idempotency
	isNew1, err := repo.ProcessLight(ctx, "", "job-101", "cust-1")
	if err != nil || !isNew1 {
		t.Fatalf("ProcessLight first time should be new: isNew=%v, err=%v", isNew1, err)
	}
	isNew2, err := repo.ProcessLight(ctx, "", "job-101", "cust-1")
	if err != nil || isNew2 {
		t.Fatalf("ProcessLight duplicate job should return isNew=false: isNew=%v, err=%v", isNew2, err)
	}

	// 4. ProcessFull idempotency
	isNewFull1, err := repo.ProcessFull(ctx, "", "job-202", "cust-2", db.Order{})
	if err != nil || !isNewFull1 {
		t.Fatalf("ProcessFull first time should be new: isNew=%v, err=%v", isNewFull1, err)
	}
	isNewFull2, err := repo.ProcessFull(ctx, "", "job-202", "cust-2", db.Order{})
	if err != nil || isNewFull2 {
		t.Fatalf("ProcessFull duplicate job should return isNew=false: isNew=%v, err=%v", isNewFull2, err)
	}
}

// Test API Endpoints
func TestAPIEndpoints(t *testing.T) {
	cfg := &config.Config{
		AppMode:        "api",
		ServiceName:    "orders",
		QueueName:      "orders-queue",
		SQSQueueURL:    "http://localhost:4566/000000000000/orders-queue",
		WorkerDBMode:   "light",
	}
	repo := db.NewInMemoryRepository()
	mockSQS := NewMockSQS()

	server := api.NewServer(cfg, repo, mockSQS)
	router := server.Engine()

	// 1. Health check
	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "OK" {
		t.Errorf("GET /health failed: code=%d body=%s", w.Code, w.Body.String())
	}

	// 2. Actuator liveness/readiness
	req, _ = http.NewRequest("GET", "/actuator/health/liveness", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET /actuator/health/liveness failed: %d", w.Code)
	}

	// 3. Chaos toggle
	req, _ = http.NewRequest("POST", "/chaos/health", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("POST /chaos/health failed: %d", w.Code)
	}

	// Should now fail health check
	req, _ = http.NewRequest("GET", "/health", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected /health to return 503 during chaos, got %d", w.Code)
	}

	// Restore health
	req, _ = http.NewRequest("DELETE", "/chaos/health", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 4. Metrics endpoint
	req, _ = http.NewRequest("GET", "/metrics", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET /metrics failed: %d", w.Code)
	}

	// 5. POST /orders
	orderBody := `{"shippingAddress":{"firstName":"Bob","city":"Metropolis","state":"NY"},"items":[{"productId":"p1","quantity":1,"unitCost":10,"totalCost":10}]}`
	req, _ = http.NewRequest("POST", "/orders", bytes.NewBufferString(orderBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("POST /orders failed: %d body=%s", w.Code, w.Body.String())
	}

	// 6. GET /orders
	req, _ = http.NewRequest("GET", "/orders", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET /orders failed: %d body=%s", w.Code, w.Body.String())
	}
	var existing []db.ExistingOrder
	if err := json.Unmarshal(w.Body.Bytes(), &existing); err != nil || len(existing) != 1 {
		t.Errorf("Failed to parse orders response or count mismatch: %v", err)
	}

	// 7. POST /jobs validation
	// workUnits < 1
	req, _ = http.NewRequest("POST", "/jobs", bytes.NewBufferString(`{"workUnits": 0}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for workUnits=0, got %d", w.Code)
	}

	// workUnits > 10000
	req, _ = http.NewRequest("POST", "/jobs", bytes.NewBufferString(`{"workUnits": 10001}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for workUnits=10001, got %d", w.Code)
	}

	// batchSize > 10
	req, _ = http.NewRequest("POST", "/jobs", bytes.NewBufferString(`{"workUnits": 50, "batchSize": 15}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for batchSize=15, got %d", w.Code)
	}

	// Valid single job
	req, _ = http.NewRequest("POST", "/jobs", bytes.NewBufferString(`{"workUnits": 10, "batchSize": 1, "dbMode": "light"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("POST /jobs single failed: %d body=%s", w.Code, w.Body.String())
	}
	if len(mockSQS.SentMessages) != 1 {
		t.Errorf("Expected 1 sent message, got %d", len(mockSQS.SentMessages))
	}

	// Valid batch job (batchSize=3)
	req, _ = http.NewRequest("POST", "/jobs", bytes.NewBufferString(`{"workUnits": 10, "batchSize": 3, "dbMode": "light"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("POST /jobs batch failed: %d body=%s", w.Code, w.Body.String())
	}
	if mockSQS.SentBatchCount != 3 {
		t.Errorf("Expected 3 batch items sent, got %d", mockSQS.SentBatchCount)
	}
}

// Test Worker execution, DB modes, and Graceful Shutdown with Visibility Reset
func TestWorkerProcessingAndGracefulShutdown(t *testing.T) {
	cfg := &config.Config{
		AppMode:           "worker",
		ServiceName:       "orders",
		QueueName:         "orders-queue",
		SQSQueueURL:       "http://localhost:4566/000000000000/orders-queue",
		SQSMaxMessages:    5,
		WorkerConcurrency: 1, // 1 concurrency to verify unhandled batch message visibility reset
		WorkerDBMode:      "light",
		PodName:           "test-worker-pod",
	}
	repo := db.NewInMemoryRepository()
	mockSQS := NewMockSQS()

	// Seed 2 messages into mock queue
	now := time.Now().UnixMilli()
	msg1 := worker.JobMessage{
		JobID:      "job-test-1",
		EnqueuedAt: now - 50,
		WorkUnits:  10,
		DBMode:     "light",
	}
	b1, _ := json.Marshal(msg1)

	msg2 := worker.JobMessage{
		JobID:      "job-test-2",
		EnqueuedAt: now - 30,
		WorkUnits:  50,
		DBMode:     "full",
	}
	b2, _ := json.Marshal(msg2)

	mockSQS.Messages = []sqsTypes.Message{
		{
			MessageId:     aws.String("m1"),
			ReceiptHandle: aws.String("receipt-m1"),
			Body:          aws.String(string(b1)),
		},
		{
			MessageId:     aws.String("m2"),
			ReceiptHandle: aws.String("receipt-m2"),
			Body:          aws.String(string(b2)),
		},
	}

	wrk := worker.NewWorker(cfg, repo, mockSQS)

	ctx, cancel := context.WithCancel(context.Background())

	// Run worker in goroutine
	done := make(chan struct{})
	go func() {
		wrk.Start(ctx)
		close(done)
	}()

	// Wait 25ms (first message takes ~10ms to complete) then cancel context while message 2 might be unstarted or in-flight
	time.Sleep(25 * time.Millisecond)
	cancel()
	wrk.Stop()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Worker did not shut down in time")
	}

	// Verify message 1 was deleted
	if len(mockSQS.DeletedReceipts) < 1 {
		t.Errorf("Expected at least 1 deleted message, got %d", len(mockSQS.DeletedReceipts))
	}
	if len(mockSQS.DeletedReceipts) > 0 && mockSQS.DeletedReceipts[0] != "receipt-m1" {
		t.Errorf("Expected receipt-m1 deleted, got %s", mockSQS.DeletedReceipts[0])
	}

	// If message 2 was not started, visibility was changed to 0
	if len(mockSQS.DeletedReceipts) == 1 {
		if len(mockSQS.VisibilityChanged) != 1 || mockSQS.VisibilityChanged[0] != "receipt-m2" {
			t.Errorf("Expected receipt-m2 visibility reset to 0, got %v", mockSQS.VisibilityChanged)
		}
		if mockSQS.VisibilityTimeoutVal != 0 {
			t.Errorf("Expected visibility timeout 0, got %d", mockSQS.VisibilityTimeoutVal)
		}
	}
}
