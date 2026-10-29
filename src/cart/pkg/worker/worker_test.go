package worker

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/aws-containers/retail-store-sample-app/cart/pkg/metrics"
	"github.com/aws-containers/retail-store-sample-app/cart/pkg/model"
	"github.com/aws-containers/retail-store-sample-app/cart/pkg/store"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSQS struct {
	mu               sync.Mutex
	receiveFunc      func(ctx context.Context, params *sqs.ReceiveMessageInput) (*sqs.ReceiveMessageOutput, error)
	deletedHandles   []string
	visibilityResets []string
}

func (m *mockSQS) SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	return &sqs.SendMessageOutput{}, nil
}

func (m *mockSQS) SendMessageBatch(ctx context.Context, params *sqs.SendMessageBatchInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
	return &sqs.SendMessageBatchOutput{}, nil
}

func (m *mockSQS) ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	if m.receiveFunc != nil {
		return m.receiveFunc(ctx, params)
	}
	return &sqs.ReceiveMessageOutput{}, nil
}

func (m *mockSQS) DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if params.ReceiptHandle != nil {
		m.deletedHandles = append(m.deletedHandles, *params.ReceiptHandle)
	}
	return &sqs.DeleteMessageOutput{}, nil
}

func (m *mockSQS) ChangeMessageVisibility(ctx context.Context, params *sqs.ChangeMessageVisibilityInput, optFns ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if params.ReceiptHandle != nil && params.VisibilityTimeout == 0 {
		m.visibilityResets = append(m.visibilityResets, *params.ReceiptHandle)
	}
	return &sqs.ChangeMessageVisibilityOutput{}, nil
}

func TestBurnCPU(t *testing.T) {
	// Calibrated burn: 10 work units should burn at least 10ms
	start := time.Now()
	BurnCPU(10)
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, 10*time.Millisecond)

	// Zero or negative should return immediately
	start = time.Now()
	BurnCPU(0)
	BurnCPU(-5)
	assert.Less(t, time.Since(start), 5*time.Millisecond)
}

func TestWorkerProcessing(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.NewMetrics(reg)
	st := store.NewMemoryStore()

	mock := &mockSQS{}
	cfg := Config{
		QueueURL:    "http://test-queue",
		QueueName:   "cart-queue",
		Service:     "cart",
		PodName:     "test-pod",
		MaxMessages: 1,
		Concurrency: 1,
		DBMode:      "light",
	}

	wkr := NewWorker(mock, st, cfg, m)

	// Test 1: Single light job
	jobMsg := model.JobMessage{
		JobID:      "job-test-1",
		EnqueuedAt: time.Now().UnixMilli() - 50,
		WorkUnits:  5,
		DBMode:     "light",
		CustomerID: "user-1",
	}
	bodyBytes, _ := json.Marshal(jobMsg)

	msg := sqstypes.Message{
		MessageId:     aws.String("job-test-1"),
		ReceiptHandle: aws.String("receipt-1"),
		Body:          aws.String(string(bodyBytes)),
	}

	wkr.processMessage(msg)

	mock.mu.Lock()
	require.Len(t, mock.deletedHandles, 1)
	assert.Equal(t, "receipt-1", mock.deletedHandles[0])
	mock.mu.Unlock()

	// Test 2: Duplicate job
	wkr.processMessage(msg)
	mock.mu.Lock()
	require.Len(t, mock.deletedHandles, 2)
	mock.mu.Unlock()

	// Test 3: Full mode job
	fullJobMsg := model.JobMessage{
		JobID:      "job-test-2",
		EnqueuedAt: time.Now().UnixMilli() - 20,
		WorkUnits:  2,
		DBMode:     "full",
		CustomerID: "user-2",
	}
	fullBytes, _ := json.Marshal(fullJobMsg)
	msg2 := sqstypes.Message{
		MessageId:     aws.String("job-test-2"),
		ReceiptHandle: aws.String("receipt-2"),
		Body:          aws.String(string(fullBytes)),
	}
	wkr.processMessage(msg2)

	cart, err := st.GetCart(context.Background(), "user-2")
	require.NoError(t, err)
	assert.Len(t, cart.Items, 1)
	assert.Equal(t, "job-item", cart.Items[0].ItemID)
}

func TestWorkerUnstartedMessagesVisibility(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.NewMetrics(reg)
	st := store.NewMemoryStore()
	mock := &mockSQS{}

	cfg := Config{
		QueueURL:    "http://test-queue",
		QueueName:   "cart-queue",
		Service:     "cart",
		PodName:     "test-pod",
		MaxMessages: 2,
		Concurrency: 1,
	}

	wkr := NewWorker(mock, st, cfg, m)

	unstarted := []sqstypes.Message{
		{ReceiptHandle: aws.String("h-1")},
		{ReceiptHandle: aws.String("h-2")},
	}

	wkr.returnUnstartedMessages(unstarted)

	mock.mu.Lock()
	require.Len(t, mock.visibilityResets, 2)
	assert.Contains(t, mock.visibilityResets, "h-1")
	assert.Contains(t, mock.visibilityResets, "h-2")
	mock.mu.Unlock()
}

func TestInvalidJobsRetainMessages(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.NewMetrics(reg)
	mock := &mockSQS{}
	w := NewWorker(mock, store.NewMemoryStore(), Config{DBMode: "light"}, m)
	for _, body := range []string{"not-json", " null ", "[]", `{"jobId":"x","workUnits":-1}`, `{"jobId":"x","dbMode":"invalid"}`} {
		w.processMessage(sqstypes.Message{MessageId: aws.String("stable"), Body: aws.String(body), ReceiptHandle: aws.String("r")})
	}
	if len(mock.deletedHandles) != 0 {
		t.Fatal("invalid message was acknowledged")
	}
}
