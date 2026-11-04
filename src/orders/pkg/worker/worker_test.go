package worker

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqsTypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/aws-containers/retail-store-sample-app/orders/pkg/config"
	"github.com/aws-containers/retail-store-sample-app/orders/pkg/db"
)

type mockSQSClient struct {
	mu                sync.Mutex
	messages          []sqsTypes.Message
	deleted           []string
	visibilityChanged []string
	completed         chan string
}

func (m *mockSQSClient) ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		return &sqs.ReceiveMessageOutput{Messages: []sqsTypes.Message{}}, nil
	}
	n := len(m.messages)
	if params.MaxNumberOfMessages > 0 && n > int(params.MaxNumberOfMessages) {
		n = int(params.MaxNumberOfMessages)
	}
	msgs := m.messages[:n]
	m.messages = m.messages[n:]
	return &sqs.ReceiveMessageOutput{Messages: msgs}, nil
}

func (m *mockSQSClient) DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleted = append(m.deleted, aws.ToString(params.ReceiptHandle))
	if m.completed != nil {
		m.completed <- aws.ToString(params.ReceiptHandle)
	}
	return &sqs.DeleteMessageOutput{}, nil
}

func (m *mockSQSClient) ChangeMessageVisibility(ctx context.Context, params *sqs.ChangeMessageVisibilityInput, optFns ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.visibilityChanged = append(m.visibilityChanged, aws.ToString(params.ReceiptHandle))
	return &sqs.ChangeMessageVisibilityOutput{}, nil
}

func (m *mockSQSClient) SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	return &sqs.SendMessageOutput{}, nil
}

func (m *mockSQSClient) SendMessageBatch(ctx context.Context, params *sqs.SendMessageBatchInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
	return &sqs.SendMessageBatchOutput{}, nil
}

func TestWorkerProcessesSuccessiveBatches(t *testing.T) {
	for _, tc := range []struct {
		name        string
		batchSize   int
		concurrency int
	}{
		{name: "single message", batchSize: 1, concurrency: 1},
		{name: "parallel batch", batchSize: 2, concurrency: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &mockSQSClient{completed: make(chan string, 5)}
			for _, id := range []string{"job1", "job2", "job3", "job4", "job5"} {
				body, err := json.Marshal(JobMessage{JobID: id, DBMode: "none"})
				if err != nil {
					t.Fatal(err)
				}
				client.messages = append(client.messages, sqsTypes.Message{
					MessageId: aws.String(id), ReceiptHandle: aws.String(id), Body: aws.String(string(body)),
				})
			}
			cfg := &config.Config{
				ServiceName: "orders", QueueName: "orders-queue", PodName: "batch-regression",
				SQSQueueURL: "http://localhost/orders-queue", SQSMaxMessages: tc.batchSize,
				WorkerConcurrency: tc.concurrency, WorkerDBMode: "none",
			}
			w := NewWorker(cfg, db.NewInMemoryRepository(), client)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				defer close(done)
				w.Start(ctx)
			}()
			t.Cleanup(func() {
				cancel()
				w.Stop()
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Error("worker failed to stop")
				}
			})
			seen := make(map[string]bool)
			deadline := time.NewTimer(2 * time.Second)
			defer deadline.Stop()
			for len(seen) < 5 {
				select {
				case id := <-client.completed:
					if seen[id] {
						t.Fatalf("job %s processed twice", id)
					}
					seen[id] = true
				case <-deadline.C:
					t.Fatalf("worker stopped polling after completing %d of 5 messages", len(seen))
				}
			}
		})
	}
}

func TestWorkerUnstartedMessagesVisibilityReset(t *testing.T) {
	cfg := &config.Config{
		AppMode:           "worker",
		ServiceName:       "orders",
		QueueName:         "orders-queue",
		SQSQueueURL:       "http://localhost:4566/000000000000/orders-queue",
		SQSMaxMessages:    3,
		WorkerConcurrency: 1, // Only 1 message at a time
		WorkerDBMode:      "none",
		PodName:           "pod-vis-test",
	}

	repo := db.NewInMemoryRepository()
	mock := &mockSQSClient{}

	msg1, _ := json.Marshal(JobMessage{JobID: "j1", WorkUnits: 40, DBMode: "none"})
	msg2, _ := json.Marshal(JobMessage{JobID: "j2", WorkUnits: 40, DBMode: "none"})
	msg3, _ := json.Marshal(JobMessage{JobID: "j3", WorkUnits: 40, DBMode: "none"})

	mock.messages = []sqsTypes.Message{
		{MessageId: aws.String("id1"), ReceiptHandle: aws.String("handle1"), Body: aws.String(string(msg1))},
		{MessageId: aws.String("id2"), ReceiptHandle: aws.String("handle2"), Body: aws.String(string(msg2))},
		{MessageId: aws.String("id3"), ReceiptHandle: aws.String("handle3"), Body: aws.String(string(msg3))},
	}

	w := NewWorker(cfg, repo, mock)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		w.Start(ctx)
		close(done)
	}()

	// Wait 10ms (j1 has started but not finished, j2 and j3 are waiting in batchChan)
	time.Sleep(10 * time.Millisecond)
	cancel()
	w.Stop()

	<-done

	mock.mu.Lock()
	defer mock.mu.Unlock()

	// handle1 should be deleted (in-flight finished)
	if len(mock.deleted) != 1 || mock.deleted[0] != "handle1" {
		t.Errorf("Expected only handle1 to be processed and deleted, got %v", mock.deleted)
	}

	// handle2 and handle3 should have their visibility reset to 0
	if len(mock.visibilityChanged) != 2 {
		t.Fatalf("Expected 2 messages to have visibility reset, got %d: %v", len(mock.visibilityChanged), mock.visibilityChanged)
	}
	if mock.visibilityChanged[0] != "handle2" || mock.visibilityChanged[1] != "handle3" {
		t.Errorf("Expected handle2 and handle3 visibility reset, got %v", mock.visibilityChanged)
	}
}
