package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// SqsMessagesPublishedTotal counts messages published by producer
	SqsMessagesPublishedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "app_sqs_messages_published_total",
			Help: "Total number of SQS messages published by producer",
		},
		[]string{"service", "queue"},
	)

	// WorkerTasksProcessedTotal counts processed tasks by status
	WorkerTasksProcessedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "worker_tasks_processed_total",
			Help: "Total number of tasks processed by the worker",
		},
		[]string{"service", "queue", "status"},
	)

	// WorkerTaskDurationSeconds measures execution time of worker tasks
	WorkerTaskDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "worker_task_duration_seconds",
			Help:    "Execution time of worker tasks in seconds",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"service", "queue"},
	)

	// WorkerJobQueueWaitSeconds measures queue waiting time
	WorkerJobQueueWaitSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "worker_job_queue_wait_seconds",
			Help:    "Time messages spent waiting in the queue before processing started in seconds",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
		},
		[]string{"service", "queue"},
	)

	// WorkerJobE2ELatencySeconds measures total end-to-end latency
	WorkerJobE2ELatencySeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "worker_job_e2e_latency_seconds",
			Help:    "End-to-end latency from message enqueue to task completion in seconds",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 15, 30, 60, 120},
		},
		[]string{"service", "queue", "is_first_job"},
	)

	// WorkerColdStartTimestampSeconds records the timestamp when pod is ready to poll
	WorkerColdStartTimestampSeconds = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "worker_cold_start_timestamp_seconds",
			Help: "Timestamp in epoch seconds when the worker pod became ready to poll",
		},
		[]string{"service", "pod"},
	)

	// WorkerInFlightTasks tracks concurrently active tasks
	WorkerInFlightTasks = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "worker_in_flight_tasks",
			Help: "Current number of in-flight tasks being processed concurrently",
		},
		[]string{"service", "queue"},
	)
)
