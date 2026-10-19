package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws-containers/retail-store-sample-app/cart/pkg/metrics"
	"github.com/aws-containers/retail-store-sample-app/cart/pkg/model"
	"github.com/aws-containers/retail-store-sample-app/cart/pkg/store"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// SQSAPI defines the SQS interface required by Worker and Producer.
type SQSAPI interface {
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
	SendMessageBatch(ctx context.Context, params *sqs.SendMessageBatchInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error)
	ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
	ChangeMessageVisibility(ctx context.Context, params *sqs.ChangeMessageVisibilityInput, optFns ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error)
}

// Config holds configuration parameters for the Worker.
type Config struct {
	QueueURL    string
	QueueName   string
	Service     string
	PodName     string
	MaxMessages int32
	Concurrency int
	DBMode      string
}

// Worker polls SQS and executes event-driven jobs with calibrated CPU burns and idempotency.
type Worker struct {
	cfg             Config
	sqsClient       SQSAPI
	store           store.Store
	metrics         *metrics.Metrics
	isFirstJobPod   int32
	inFlightWg      sync.WaitGroup
	cancelPolling   context.CancelFunc
	shutdownDone    chan struct{}
}

// NewWorker initializes a new Worker.
func NewWorker(sqsClient SQSAPI, st store.Store, cfg Config, m *metrics.Metrics) *Worker {
	if cfg.Service == "" {
		cfg.Service = "cart"
	}
	if cfg.QueueName == "" {
		cfg.QueueName = "cart-queue"
	}
	if cfg.PodName == "" {
		cfg.PodName, _ = os.Hostname()
		if cfg.PodName == "" {
			cfg.PodName = "cart-worker"
		}
	}
	if cfg.MaxMessages < 1 || cfg.MaxMessages > 10 {
		cfg.MaxMessages = 1
	}
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}
	if cfg.DBMode == "" {
		cfg.DBMode = "light"
	}

	return &Worker{
		cfg:           cfg,
		sqsClient:     sqsClient,
		store:         st,
		metrics:       m,
		isFirstJobPod: 1,
		shutdownDone:  make(chan struct{}),
	}
}

// BurnCPU spins the CPU for 1ms per workUnit.
func BurnCPU(workUnits int) {
	if workUnits <= 0 {
		return
	}
	target := time.Duration(workUnits) * time.Millisecond
	start := time.Now()
	var x uint64 = 1
	for time.Since(start) < target {
		x = (x*1664525 + 1013904223) ^ (x >> 16)
	}
	_ = x
}

// Start begins the worker polling routines.
func (w *Worker) Start(ctx context.Context) {
	pollCtx, cancel := context.WithCancel(ctx)
	w.cancelPolling = cancel

	// Record cold start timestamp gauge
	nowSec := float64(time.Now().Unix())
	w.metrics.ColdStartTimestamp.WithLabelValues(w.cfg.Service, w.cfg.PodName).Set(nowSec)

	log.Printf("[Worker] Started (service=%s, queue=%s, pod=%s, concurrency=%d, maxMessages=%d, dbMode=%s)",
		w.cfg.Service, w.cfg.QueueName, w.cfg.PodName, w.cfg.Concurrency, w.cfg.MaxMessages, w.cfg.DBMode)

	var workerWg sync.WaitGroup
	for i := 0; i < w.cfg.Concurrency; i++ {
		workerWg.Add(1)
		go func(workerID int) {
			defer workerWg.Done()
			w.pollLoop(pollCtx, workerID)
		}(i)
	}

	go func() {
		workerWg.Wait()
		close(w.shutdownDone)
	}()
}

// Stop initiates graceful shutdown, cancelling long polls and waiting for in-flight tasks.
func (w *Worker) Stop() {
	if w.cancelPolling != nil {
		w.cancelPolling()
	}
	// Wait for polling routines to exit
	select {
	case <-w.shutdownDone:
	case <-time.After(10 * time.Second):
	}
	// Wait for in-flight tasks
	w.inFlightWg.Wait()
	log.Printf("[Worker] Graceful shutdown complete")
}

func (w *Worker) pollLoop(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if w.cfg.QueueURL == "" {
			time.Sleep(1 * time.Second)
			continue
		}

		input := &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(w.cfg.QueueURL),
			MaxNumberOfMessages: w.cfg.MaxMessages,
			WaitTimeSeconds:     20,
		}

		output, err := w.sqsClient.ReceiveMessage(ctx, input)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(1 * time.Second)
			continue
		}

		if len(output.Messages) == 0 {
			continue
		}

		// Process batch
		for i, msg := range output.Messages {
			// If shutdown requested during batch processing, return unstarted messages immediately
			if ctx.Err() != nil {
				w.returnUnstartedMessages(output.Messages[i:])
				return
			}

			w.inFlightWg.Add(1)
			w.processMessage(msg)
			w.inFlightWg.Done()
		}
	}
}

func (w *Worker) returnUnstartedMessages(messages []sqstypes.Message) {
	for _, msg := range messages {
		if msg.ReceiptHandle == nil {
			continue
		}
		// Use fresh context with 5s timeout to reset visibility
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := w.sqsClient.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
			QueueUrl:          aws.String(w.cfg.QueueURL),
			ReceiptHandle:     msg.ReceiptHandle,
			VisibilityTimeout: 0,
		})
		cancel()
		if err != nil {
			log.Printf("[Worker] Failed to reset message visibility: %v", err)
		}
	}
}

func (w *Worker) processMessage(msg sqstypes.Message) {
	w.metrics.InFlightTasks.WithLabelValues(w.cfg.Service, w.cfg.QueueName).Inc()
	defer w.metrics.InFlightTasks.WithLabelValues(w.cfg.Service, w.cfg.QueueName).Dec()

	startedAt := time.Now()
	var job model.JobMessage

	if msg.Body != nil {
		if err := json.Unmarshal([]byte(*msg.Body), &job); err != nil {
			log.Printf("[Worker] Error unmarshalling message: %v", err)
		}
	}

	if job.JobID == "" {
		if msg.MessageId != nil {
			job.JobID = *msg.MessageId
		} else {
			job.JobID = fmt.Sprintf("fallback-%d", time.Now().UnixNano())
		}
	}

	// 1. Observe queue wait time
	if job.EnqueuedAt > 0 {
		queueWaitSec := float64(startedAt.UnixMilli()-job.EnqueuedAt) / 1000.0
		if queueWaitSec >= 0 {
			w.metrics.JobQueueWait.WithLabelValues(w.cfg.Service, w.cfg.QueueName).Observe(queueWaitSec)
		}
	}

	// 2. Calibrated CPU burn
	if job.WorkUnits > 0 {
		BurnCPU(job.WorkUnits)
	}

	// 3. Database operation
	dbMode := job.DBMode
	if dbMode == "" {
		dbMode = w.cfg.DBMode
	}
	job.DBMode = dbMode

	// Process job in Store with isolated timeout
	dbCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	isNew, err := w.store.ProcessJob(dbCtx, job)
	cancel()

	completedAt := time.Now()
	durationSec := completedAt.Sub(startedAt).Seconds()
	w.metrics.TaskDuration.WithLabelValues(w.cfg.Service, w.cfg.QueueName).Observe(durationSec)

	// Determine if first job on this pod
	isFirst := atomic.CompareAndSwapInt32(&w.isFirstJobPod, 1, 0)
	isFirstStr := "false"
	if isFirst {
		isFirstStr = "true"
	}

	if job.EnqueuedAt > 0 {
		e2eSec := float64(completedAt.UnixMilli()-job.EnqueuedAt) / 1000.0
		if e2eSec >= 0 {
			w.metrics.JobE2ELatency.WithLabelValues(w.cfg.Service, w.cfg.QueueName, isFirstStr).Observe(e2eSec)
		}
	}

	status := "success"
	if err != nil {
		status = "error"
		log.Printf("[Worker] Error processing job %s: %v", job.JobID, err)
	} else if !isNew {
		status = "duplicate"
	}
	w.metrics.TasksProcessed.WithLabelValues(w.cfg.Service, w.cfg.QueueName, status).Inc()

	// 4. Delete message from SQS on success or duplicate
	if err == nil && msg.ReceiptHandle != nil {
		delCtx, delCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, delErr := w.sqsClient.DeleteMessage(delCtx, &sqs.DeleteMessageInput{
			QueueUrl:      aws.String(w.cfg.QueueURL),
			ReceiptHandle: msg.ReceiptHandle,
		})
		delCancel()
		if delErr != nil {
			log.Printf("[Worker] Failed to delete message %s from SQS: %v", job.JobID, delErr)
		}
	}

	// 5. Structured JSON log per completed message
	var queueWaitMs int64
	var e2eMs int64
	if job.EnqueuedAt > 0 {
		queueWaitMs = startedAt.UnixMilli() - job.EnqueuedAt
		e2eMs = completedAt.UnixMilli() - job.EnqueuedAt
	}

	logEntry := model.JobLogEntry{
		Level:       "info",
		Service:     w.cfg.Service,
		Queue:       w.cfg.QueueName,
		JobID:       job.JobID,
		CustomerID:  job.CustomerID,
		EnqueuedAt:  job.EnqueuedAt,
		StartedAt:   startedAt.UnixMilli(),
		CompletedAt: completedAt.UnixMilli(),
		QueueWaitMs: queueWaitMs,
		DurationMs:  completedAt.Sub(startedAt).Milliseconds(),
		E2EMs:       e2eMs,
		IsFirstJob:  isFirst,
		Status:      status,
		DBMode:      dbMode,
	}

	logBytes, _ := json.Marshal(logEntry)
	fmt.Println(string(logBytes))
}
