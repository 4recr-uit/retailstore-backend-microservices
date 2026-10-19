# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

import os
import socket
import time
from prometheus_client import Counter, Gauge, Histogram

# ---------------------------------------------------------
# Prometheus Metrics (All 7 Golden Metrics for RQ1 -> RQ5)
# ---------------------------------------------------------
SQS_MESSAGES_PUBLISHED_TOTAL = Counter(
    "app_sqs_messages_published_total",
    "Total SQS messages published by producer",
    ["service", "queue"],
)

WORKER_TASKS_PROCESSED_TOTAL = Counter(
    "worker_tasks_processed_total",
    "Total worker tasks processed",
    ["service", "queue", "status"],
)

WORKER_TASK_DURATION_SECONDS = Histogram(
    "worker_task_duration_seconds",
    "Worker task processing duration",
    ["service", "queue"],
    buckets=(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0),
)

WORKER_JOB_QUEUE_WAIT_SECONDS = Histogram(
    "worker_job_queue_wait_seconds",
    "Job wait time in SQS queue",
    ["service", "queue"],
    buckets=(0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0),
)

WORKER_JOB_E2E_LATENCY_SECONDS = Histogram(
    "worker_job_e2e_latency_seconds",
    "Job end-to-end latency",
    ["service", "queue", "is_first_job"],
    buckets=(0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0, 120.0),
)

WORKER_COLD_START_TIMESTAMP_SECONDS = Gauge(
    "worker_cold_start_timestamp_seconds",
    "Worker pod cold start timestamp",
    ["service", "pod"],
)

WORKER_IN_FLIGHT_TASKS = Gauge(
    "worker_in_flight_tasks",
    "Current worker in-flight tasks",
    ["service", "queue"],
)


def get_pod_name() -> str:
    return os.getenv("HOSTNAME") or os.getenv("COMPUTERNAME") or socket.gethostname()


def init_metrics(service: str = "checkout", queue: str = "checkout-queue") -> None:
    pod = get_pod_name()
    WORKER_IN_FLIGHT_TASKS.labels(service=service, queue=queue).set(0)
    WORKER_COLD_START_TIMESTAMP_SECONDS.labels(service=service, pod=pod).set(time.time())
