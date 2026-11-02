package worker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	appconfig "github.com/aws-containers/retail-store-sample-app/orders/pkg/config"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqsTypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/aws-containers/retail-store-sample-app/orders/pkg/config"
	"github.com/aws-containers/retail-store-sample-app/orders/pkg/db"
	"github.com/aws-containers/retail-store-sample-app/orders/pkg/metrics"
)

// SQSClient interface covering required SQS operations
type SQSClient interface {
	ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
	ChangeMessageVisibility(ctx context.Context, params *sqs.ChangeMessageVisibilityInput, optFns ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error)
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
	SendMessageBatch(ctx context.Context, params *sqs.SendMessageBatchInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error)
}

type JobMessage struct {
	JobID      string          `json:"jobId"`
	EnqueuedAt int64           `json:"enqueuedAt"`
	WorkUnits  int             `json:"workUnits"`
	DBMode     string          `json:"dbMode"`
	CustomerID string          `json:"customerId"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

type Worker struct {
	cfg           *config.Config
	repo          db.Repository
	sqsClient     SQSClient
	queueURL      string
	queueName     string
	serviceName   string
	maxMessages   int
	concurrency   int
	defaultDBMode string

	firstJobFlag atomic.Bool
	stopOnce     sync.Once
	stopChan     chan struct{}
}

func NewWorker(cfg *config.Config, repo db.Repository, sqsClient SQSClient) *Worker {
	return &Worker{
		cfg:           cfg,
		repo:          repo,
		sqsClient:     sqsClient,
		queueURL:      cfg.SQSQueueURL,
		queueName:     cfg.QueueName,
		serviceName:   cfg.ServiceName,
		maxMessages:   cfg.SQSMaxMessages,
		concurrency:   cfg.WorkerConcurrency,
		defaultDBMode: cfg.WorkerDBMode,
		stopChan:      make(chan struct{}),
	}
}

// BurnCPU spins CPU for calibrated duration (1 workUnit = 1 ms active CPU spin)
func BurnCPU(workUnits int) {
	if workUnits <= 0 {
		return
	}
	targetDuration := time.Duration(workUnits) * time.Millisecond
	start := time.Now()
	data := []byte("retail-store-workunit-calibration-seed")
	for time.Since(start) < targetDuration {
		h := sha256.Sum256(data)
		data = h[:]
	}
}

// Start begins polling the SQS queue and processing messages
func (w *Worker) Start(ctx context.Context) {
	if w.sqsClient == nil || w.queueURL == "" {
		log.Printf("[worker] SQS client or queue URL is empty (queueURL=%q), worker will not poll", w.queueURL)
		return
	}

	// 1. Set worker cold start timestamp gauge upon startup when ready to poll
	coldStartTimestamp := float64(time.Now().Unix())
	metrics.WorkerColdStartTimestampSeconds.WithLabelValues(w.serviceName, w.cfg.PodName).Set(coldStartTimestamp)
	log.Printf("[worker] Started ready to poll on pod=%s, coldStart=%v, queueURL=%s, concurrency=%d, maxMessages=%d",
		w.cfg.PodName, coldStartTimestamp, w.queueURL, w.concurrency, w.maxMessages)

	// Polling loop
	for {
		select {
		case <-ctx.Done():
			log.Println("[worker] Polling loop exiting on context cancellation")
			return
		case <-w.stopChan:
			log.Println("[worker] Polling loop exiting on stop signal")
			return
		default:
		}

		// Use ctx for ReceiveMessage so long polling immediately terminates on SIGTERM/SIGINT
		out, err := w.sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(w.queueURL),
			MaxNumberOfMessages: int32(w.maxMessages),
			WaitTimeSeconds:     appconfig.SQSWaitTime(),
		})
		if err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				log.Println("[worker] ReceiveMessage canceled by context")
				return
			}
			log.Printf("[worker] ReceiveMessage error: %v (retrying after configured backoff)", err)
			select {
			case <-ctx.Done():
				return
			case <-w.stopChan:
				return
			case <-time.After(appconfig.EnvSeconds("WORKER_POLL_BACKOFF_SECONDS", 2)):
				continue
			}
		}

		if len(out.Messages) == 0 {
			continue
		}

		w.processBatch(ctx, out.Messages)
	}
}

// processBatch coordinates worker goroutines and graceful shutdown with ChangeMessageVisibility(0)
func (w *Worker) processBatch(ctx context.Context, msgs []sqsTypes.Message) {
	nMsgs := len(msgs)
	if nMsgs == 0 {
		return
	}

	batchChan := make(chan sqsTypes.Message, nMsgs)
	for _, m := range msgs {
		batchChan <- m
	}

	workersCount := w.concurrency
	if workersCount > nMsgs {
		workersCount = nMsgs
	}

	var wg sync.WaitGroup
	for i := 0; i < workersCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				// Prioritize checking cancellation before taking a new task
				if ctx.Err() != nil {
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-w.stopChan:
					return
				case msg, ok := <-batchChan:
					if !ok {
						return
					}
					w.handleMessage(msg)
				}
			}
		}()
	}

	// Wait for active worker goroutines to finish in-flight tasks
	wg.Wait()

	// Drain any unhandled messages left in batchChan (messages not started before shutdown)
	close(batchChan)
	for msg := range batchChan {
		w.returnMessageToQueue(msg)
	}
}

// returnMessageToQueue calls ChangeMessageVisibility with VisibilityTimeout = 0
func (w *Worker) returnMessageToQueue(msg sqsTypes.Message) {
	if msg.ReceiptHandle == nil {
		return
	}

	// Use background context with short timeout to ensure visibility reset completes during shutdown
	resetCtx, cancel := context.WithTimeout(context.Background(), appconfig.EnvSeconds("SQS_REQUEST_TIMEOUT_SECONDS", 5))
	defer cancel()

	_, err := w.sqsClient.ChangeMessageVisibility(resetCtx, &sqs.ChangeMessageVisibilityInput{
		QueueUrl:          aws.String(w.queueURL),
		ReceiptHandle:     msg.ReceiptHandle,
		VisibilityTimeout: 0,
	})
	if err != nil {
		log.Printf("[worker] Failed to reset message visibility for %v: %v", aws.ToString(msg.MessageId), err)
	} else {
		log.Printf("[worker] Reset visibility timeout to 0 for unstarted message %v", aws.ToString(msg.MessageId))
	}
}

// handleMessage processes a single message to completion: CPU burn, DB commit, DeleteMessage, metrics, JSON log
func (w *Worker) handleMessage(msg sqsTypes.Message) {
	metrics.WorkerInFlightTasks.WithLabelValues(w.serviceName, w.queueName).Inc()
	defer metrics.WorkerInFlightTasks.WithLabelValues(w.serviceName, w.queueName).Dec()

	startedAt := time.Now()

	var job JobMessage
	bodyStr := aws.ToString(msg.Body)
	if err := json.Unmarshal([]byte(bodyStr), &job); err != nil {
		log.Printf("[worker] Failed to unmarshal job message: %v (raw body: %s)", err, bodyStr)
		metrics.WorkerTasksProcessedTotal.WithLabelValues(w.serviceName, w.queueName, "error").Inc()
		return
	}

	// Fallback job ID if missing
	if job.JobID == "" {
		job.JobID = aws.ToString(msg.MessageId)
	}

	// 1. Calibrated CPU burn
	if job.WorkUnits > 0 {
		BurnCPU(job.WorkUnits)
	}

	// 2. Database mode resolution
	effectiveDBMode := w.defaultDBMode
	if job.DBMode != "" {
		effectiveDBMode = job.DBMode
	}

	status := "success"
	// Perform DB operation
	dbCtx, cancel := context.WithTimeout(context.Background(), appconfig.EnvSeconds("WORKER_DB_TIMEOUT_SECONDS", 10))
	defer cancel()

	switch effectiveDBMode {
	case "none":
		// Only CPU burn, no DB
		status = "success"

	case "light":
		isNew, err := w.repo.ProcessLight(dbCtx, "", job.JobID, job.CustomerID)
		if err != nil {
			log.Printf("[worker] ProcessLight error for job %s: %v", job.JobID, err)
			status = "error"
		} else if !isNew {
			status = "duplicate"
		} else {
			status = "success"
		}

	case "full":
		var order db.Order
		if len(job.Payload) > 0 {
			_ = json.Unmarshal(job.Payload, &order)
		}
		isNew, err := w.repo.ProcessFull(dbCtx, "", job.JobID, job.CustomerID, order)
		if err != nil {
			log.Printf("[worker] ProcessFull error for job %s: %v", job.JobID, err)
			status = "error"
		} else if !isNew {
			status = "duplicate"
		} else {
			status = "success"
		}

	default:
		// Default to light
		isNew, err := w.repo.ProcessLight(dbCtx, "", job.JobID, job.CustomerID)
		if err != nil {
			log.Printf("[worker] ProcessLight default error for job %s: %v", job.JobID, err)
			status = "error"
		} else if !isNew {
			status = "duplicate"
		} else {
			status = "success"
		}
	}

	// 3. DeleteMessage upon success or duplicate
	if status == "success" || status == "duplicate" {
		delCtx, delCancel := context.WithTimeout(context.Background(), appconfig.EnvSeconds("SQS_REQUEST_TIMEOUT_SECONDS", 5))
		_, err := w.sqsClient.DeleteMessage(delCtx, &sqs.DeleteMessageInput{
			QueueUrl:      aws.String(w.queueURL),
			ReceiptHandle: msg.ReceiptHandle,
		})
		delCancel()
		if err != nil {
			log.Printf("[worker] DeleteMessage failed for job %s: %v", job.JobID, err)
		}
	}

	completedAt := time.Now()

	// 4. Record Metrics
	durationSeconds := completedAt.Sub(startedAt).Seconds()
	metrics.WorkerTaskDurationSeconds.WithLabelValues(w.serviceName, w.queueName).Observe(durationSeconds)

	var queueWaitMs int64 = 0
	var e2eMs int64 = 0
	isFirst := false

	if job.EnqueuedAt > 0 {
		enqueuedTime := time.UnixMilli(job.EnqueuedAt)
		queueWaitDuration := startedAt.Sub(enqueuedTime)
		if queueWaitDuration < 0 {
			queueWaitDuration = 0
		}
		queueWaitMs = queueWaitDuration.Milliseconds()
		metrics.WorkerJobQueueWaitSeconds.WithLabelValues(w.serviceName, w.queueName).Observe(queueWaitDuration.Seconds())

		e2eDuration := completedAt.Sub(enqueuedTime)
		if e2eDuration < 0 {
			e2eDuration = 0
		}
		e2eMs = e2eDuration.Milliseconds()

		isFirst = w.firstJobFlag.CompareAndSwap(false, true)
		isFirstStr := "false"
		if isFirst {
			isFirstStr = "true"
		}
		metrics.WorkerJobE2ELatencySeconds.WithLabelValues(w.serviceName, w.queueName, isFirstStr).Observe(e2eDuration.Seconds())
	}

	metrics.WorkerTasksProcessedTotal.WithLabelValues(w.serviceName, w.queueName, status).Inc()

	// 5. 1-line Structured JSON log per completed message
	logEntry := map[string]interface{}{
		"level":           "info",
		"msg":             "task completed",
		"service":         w.serviceName,
		"queue":           w.queueName,
		"jobId":           job.JobID,
		"enqueuedAt":      job.EnqueuedAt,
		"startedAt":       startedAt.UnixMilli(),
		"completedAt":     completedAt.UnixMilli(),
		"e2eMs":           e2eMs,
		"queueWaitMs":     queueWaitMs,
		"durationMs":      completedAt.Sub(startedAt).Milliseconds(),
		"isFirstJobOnPod": isFirst,
		"dbMode":          effectiveDBMode,
		"status":          status,
	}
	jsonBytes, _ := json.Marshal(logEntry)
	fmt.Println(string(jsonBytes))
}

// Stop cleanly terminates the worker
func (w *Worker) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopChan)
	})
}
