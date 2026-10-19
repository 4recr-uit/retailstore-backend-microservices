package metrics

import (
	"errors"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds the 7 standard benchmark & operational metrics.
type Metrics struct {
	SQSMessagesPublished  *prometheus.CounterVec
	TasksProcessed        *prometheus.CounterVec
	TaskDuration          *prometheus.HistogramVec
	JobQueueWait          *prometheus.HistogramVec
	JobE2ELatency         *prometheus.HistogramVec
	ColdStartTimestamp    *prometheus.GaugeVec
	InFlightTasks         *prometheus.GaugeVec
}

var (
	globalMetrics *Metrics
	initOnce      sync.Once
)

// GetMetrics returns the singleton Metrics instance registered with the default registry.
func GetMetrics() *Metrics {
	initOnce.Do(func() {
		globalMetrics = NewMetrics(prometheus.DefaultRegisterer)
	})
	return globalMetrics
}

// NewMetrics creates and registers all 7 Prometheus metrics on the given registerer.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		SQSMessagesPublished: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "app_sqs_messages_published_total",
				Help: "Total number of messages published to SQS by producer.",
			},
			[]string{"service", "queue"},
		),
		TasksProcessed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "worker_tasks_processed_total",
				Help: "Total number of tasks processed by the worker.",
			},
			[]string{"service", "queue", "status"},
		),
		TaskDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "worker_task_duration_seconds",
				Help:    "Execution duration of individual worker tasks.",
				Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"service", "queue"},
		),
		JobQueueWait: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "worker_job_queue_wait_seconds",
				Help:    "Time jobs spent waiting in the SQS queue before processing.",
				Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
			},
			[]string{"service", "queue"},
		),
		JobE2ELatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "worker_job_e2e_latency_seconds",
				Help:    "End-to-end latency from job enqueueing to completion.",
				Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
			},
			[]string{"service", "queue", "is_first_job"},
		),
		ColdStartTimestamp: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "worker_cold_start_timestamp_seconds",
				Help: "Timestamp when worker pod finished initialization and began polling.",
			},
			[]string{"service", "pod"},
		),
		InFlightTasks: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "worker_in_flight_tasks",
				Help: "Current number of tasks actively being processed.",
			},
			[]string{"service", "queue"},
		),
	}

	if reg != nil {
		collectors := []prometheus.Collector{
			m.SQSMessagesPublished,
			m.TasksProcessed,
			m.TaskDuration,
			m.JobQueueWait,
			m.JobE2ELatency,
			m.ColdStartTimestamp,
			m.InFlightTasks,
		}
		for _, c := range collectors {
			if err := reg.Register(c); err != nil {
				var are prometheus.AlreadyRegisteredError
				if !errors.As(err, &are) {
					// In case of any registration error other than already registered, proceed safely
				}
			}
		}
	}

	return m
}
