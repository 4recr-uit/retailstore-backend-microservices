# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import logging
import secrets
import string
import threading
import time
from typing import Any, Optional

import boto3
from botocore.config import Config

from app.config import get_settings
from app.metrics import (
    WORKER_COLD_START_TIMESTAMP_SECONDS,
    WORKER_IN_FLIGHT_TASKS,
    WORKER_JOB_E2E_LATENCY_SECONDS,
    WORKER_JOB_QUEUE_WAIT_SECONDS,
    WORKER_TASK_DURATION_SECONDS,
    WORKER_TASKS_PROCESSED_TOTAL,
    init_metrics,
)
from app.repository import BaseCheckoutRepository

logger = logging.getLogger("checkout")


class BatchPublishError(RuntimeError):
    def __init__(self, successful_job_ids: list[str], failed_job_ids: list[str]) -> None:
        super().__init__("SQS did not acknowledge every batch entry")
        self.successful_job_ids = successful_job_ids
        self.failed_job_ids = failed_job_ids


def make_id(length: int) -> str:
    chars = string.ascii_letters + string.digits
    return "".join(secrets.choice(chars) for _ in range(length))


def burn_cpu(work_units: int) -> None:
    """
    Calibrated CPU burn: 1 workUnit = 1 ms active CPU burn.
    Uses time.perf_counter() loop hashing with hashlib.sha256 until
    workUnits / 1000.0 seconds have elapsed.
    """
    if work_units <= 0:
        return
    target_seconds = work_units / 1000.0
    start = time.perf_counter()
    buf = b"checkout-work-unit-calibration-seed"
    while (time.perf_counter() - start) < target_seconds:
        buf = hashlib.sha256(buf).digest()


class SQSService:
    def __init__(self, queue_url: str = "", client: Any = None) -> None:
        self.queue_url = queue_url
        self.client = client
        if queue_url:
            self.queue_name = queue_url.rstrip("/").split("/")[-1]
        else:
            settings = get_settings()
            self.queue_name = settings.sqs_queue_name

    def publish_single(self, message: dict[str, Any]) -> bool:
        if not self.queue_url or not self.client:
            return False
        from app.metrics import SQS_MESSAGES_PUBLISHED_TOTAL

        self.client.send_message(
            QueueUrl=self.queue_url,
            MessageBody=json.dumps(message),
        )
        SQS_MESSAGES_PUBLISHED_TOTAL.labels(service="checkout", queue=self.queue_name).inc()
        return True

    def publish_batch(self, messages: list[dict[str, Any]]) -> int:
        if not self.queue_url or not self.client or not messages:
            return 0
        from app.metrics import SQS_MESSAGES_PUBLISHED_TOTAL

        entries = [
            {
                "Id": str(idx),
                "MessageBody": json.dumps(msg),
            }
            for idx, msg in enumerate(messages)
        ]
        pending, accepted = entries, set()
        for attempt in range(3):
            if not pending:
                break
            if attempt:
                time.sleep(0.05 * attempt)
            try:
                response = self.client.send_message_batch(QueueUrl=self.queue_url, Entries=pending)
            except Exception:
                # Preserve known acknowledgements if a subsequent request fails.
                break
            accepted.update(item["Id"] for item in response.get("Successful", []))
            permanent = {item["Id"] for item in response.get("Failed", []) if item.get("SenderFault")}
            pending = [entry for entry in pending if entry["Id"] not in accepted | permanent]
        successful = [msg["jobId"] for i, msg in enumerate(messages) if str(i) in accepted]
        failed = [msg["jobId"] for i, msg in enumerate(messages) if str(i) not in accepted]
        SQS_MESSAGES_PUBLISHED_TOTAL.labels(service="checkout", queue=self.queue_name).inc(len(successful))
        if failed:
            raise BatchPublishError(successful, failed)
        return len(successful)

    def receive_messages(self, max_messages: int = 1, wait_time_seconds: int = 20) -> list[dict[str, Any]]:
        if not self.queue_url or not self.client:
            return []
        try:
            response = self.client.receive_message(
                QueueUrl=self.queue_url,
                MaxNumberOfMessages=max_messages,
                WaitTimeSeconds=wait_time_seconds,
            )
            return response.get("Messages", [])
        except Exception as exc:
            logger.error("SQS receive_message error: %s", exc)
            return []

    def delete_message(self, receipt_handle: str) -> bool:
        if not self.queue_url or not self.client or not receipt_handle:
            return False
        try:
            self.client.delete_message(
                QueueUrl=self.queue_url,
                ReceiptHandle=receipt_handle,
            )
            return True
        except Exception as exc:
            logger.error("SQS delete_message error: %s", exc)
            return False

    def reset_visibility(self, receipt_handle: str, visibility_timeout: int = 0) -> bool:
        if not self.queue_url or not self.client or not receipt_handle:
            return False
        try:
            self.client.change_message_visibility(
                QueueUrl=self.queue_url,
                ReceiptHandle=receipt_handle,
                VisibilityTimeout=visibility_timeout,
            )
            return True
        except Exception as exc:
            logger.warning("Failed to reset visibility timeout: %s", exc)
            return False


def create_sqs_service() -> SQSService:
    settings = get_settings()
    queue_url = settings.sqs_queue_url
    endpoint_url = settings.aws_endpoint_url
    region = settings.aws_default_region

    if not queue_url and not endpoint_url:
        logger.info("SQS_QUEUE_URL not configured. SQS publishing will be skipped in standalone mode.")
        return SQSService(queue_url="", client=None)

    try:
        cfg = Config(max_pool_connections=settings.sqs_max_pool_connections,
                     connect_timeout=5, read_timeout=max(10, settings.sqs_wait_time_seconds + 5),
                     retries={"mode": "standard", "total_max_attempts": 2})
        client = boto3.client("sqs", endpoint_url=endpoint_url, region_name=region, config=cfg)
        return SQSService(queue_url=queue_url, client=client)
    except Exception as exc:
        logger.warning("Could not initialize boto3 SQS client: %s", exc)
        return SQSService(queue_url=queue_url, client=None)


class CheckoutWorker:
    def __init__(
        self,
        sqs_service: SQSService,
        repository: BaseCheckoutRepository,
        concurrency: int = 1,
        max_messages: int = 1,
        wait_time_seconds: int = 20,
        default_db_mode: str = "light",
    ) -> None:
        self.sqs_service = sqs_service
        self.repository = repository
        self.concurrency = max(1, concurrency)
        self.max_messages = max(1, min(10, max_messages))
        self.wait_time_seconds = max(0, wait_time_seconds)
        self.default_db_mode = default_db_mode.lower()
        self.is_first_job: bool = True
        self._first_job_lock = threading.Lock()
        self.shutdown_event = threading.Event()
        self.executor: Optional[ThreadPoolExecutor] = None
        self.worker_thread: Optional[threading.Thread] = None

    def start(self) -> None:
        logger.info(
            "Starting checkout worker (concurrency=%d, max_messages=%d, queue=%s, db_mode=%s)",
            self.concurrency,
            self.max_messages,
            self.sqs_service.queue_name,
            self.default_db_mode,
        )
        init_metrics("checkout", self.sqs_service.queue_name)
        self.shutdown_event.clear()
        self.executor = ThreadPoolExecutor(max_workers=self.concurrency, thread_name_prefix="checkout-worker")
        self.worker_thread = threading.Thread(target=self._run_loop, daemon=True)
        self.worker_thread.start()

    def stop(self) -> None:
        logger.info("Stopping checkout worker gracefully...")
        self.shutdown_event.set()
        if self.worker_thread and self.worker_thread.is_alive():
            # The polling owner must exit before its executor can be closed.
            self.worker_thread.join()
        if self.executor:
            self.executor.shutdown(wait=True, cancel_futures=False)
        logger.info("Checkout worker stopped.")

    def process_message(self, message: dict[str, Any]) -> None:
        started_at_sec = time.perf_counter()
        started_at_ms = int(time.time() * 1000)

        try:
            body = json.loads(message.get("Body", ""))
            if not isinstance(body, dict):
                raise ValueError("Job must be an object")
            job_id = body.get("jobId") or message.get("MessageId")
            if not isinstance(job_id, str) or not job_id:
                raise ValueError("Job must have a stable ID")
            work_units = body.get("workUnits", 10)
            enqueued_at = body.get("enqueuedAt", started_at_ms)
            db_mode = body.get("dbMode", self.default_db_mode)
            if type(work_units) is not int or not 0 <= work_units <= 10000:
                raise ValueError("Invalid workUnits")
            if type(enqueued_at) is not int or enqueued_at < 0:
                raise ValueError("Invalid enqueuedAt")
            if db_mode not in ("none", "light", "full"):
                raise ValueError("Invalid dbMode")
        except (TypeError, ValueError):
            WORKER_TASKS_PROCESSED_TOTAL.labels(
                service="checkout", queue=self.sqs_service.queue_name, status="error").inc()
            logger.warning("Invalid job; retaining message for retry/DLQ")
            return
        order_id = body.get("orderId") or job_id
        customer_id = body.get("customerId")
        payload = body.get("payload")

        with self._first_job_lock:
            first_job_str = "true" if self.is_first_job else "false"
            self.is_first_job = False

        queue_wait_sec = max(0.0, (started_at_ms - enqueued_at) / 1000.0)
        WORKER_JOB_QUEUE_WAIT_SECONDS.labels(
            service="checkout", queue=self.sqs_service.queue_name
        ).observe(queue_wait_sec)

        WORKER_IN_FLIGHT_TASKS.labels(
            service="checkout", queue=self.sqs_service.queue_name
        ).inc()

        status_result = "COMPLETED"
        try:
            # Active CPU burn
            burn_cpu(work_units)

            # Database persistence mode
            if db_mode == "none":
                status_result = "COMPLETED"
            elif db_mode == "light":
                inserted = self.repository.record_processed_job(
                    job_id=job_id,
                    order_id=order_id,
                    customer_id=customer_id,
                    status_val="COMPLETED",
                    data={"workUnits": work_units},
                )
                status_result = "COMPLETED" if inserted else "DUPLICATE"
            elif db_mode == "full":
                inserted = self.repository.record_processed_job(
                    job_id=job_id,
                    order_id=order_id,
                    customer_id=customer_id,
                    status_val="COMPLETED",
                    data=payload if isinstance(payload, dict) else {"raw": payload},
                )
                status_result = "COMPLETED" if inserted else "DUPLICATE"

            completed_at_sec = time.perf_counter()
            completed_at_ms = int(time.time() * 1000)
            duration_sec = completed_at_sec - started_at_sec
            e2e_sec = max(0.0, (completed_at_ms - enqueued_at) / 1000.0)

            WORKER_TASK_DURATION_SECONDS.labels(
                service="checkout", queue=self.sqs_service.queue_name
            ).observe(duration_sec)

            WORKER_JOB_E2E_LATENCY_SECONDS.labels(
                service="checkout",
                queue=self.sqs_service.queue_name,
                is_first_job=first_job_str,
            ).observe(e2e_sec)

            metric_status = "success" if status_result == "COMPLETED" else "duplicate"
            WORKER_TASKS_PROCESSED_TOTAL.labels(
                service="checkout",
                queue=self.sqs_service.queue_name,
                status=metric_status,
            ).inc()

            log_entry = {
                "event": "job_completed",
                "service": "checkout",
                "queue": self.sqs_service.queue_name,
                "jobId": job_id,
                "orderId": order_id,
                "customerId": customer_id,
                "enqueuedAt": enqueued_at,
                "startedAt": started_at_ms,
                "completedAt": completed_at_ms,
                "queueWaitMs": int(queue_wait_sec * 1000),
                "durationMs": int(duration_sec * 1000),
                "e2eMs": int(e2e_sec * 1000),
                "isFirstJobOnPod": first_job_str,
                "status": status_result,
                "dbMode": db_mode,
            }
            logger.info(json.dumps(log_entry))

            receipt_handle = message.get("ReceiptHandle")
            if receipt_handle:
                self.sqs_service.delete_message(receipt_handle)
        except Exception as exc:
            WORKER_TASKS_PROCESSED_TOTAL.labels(
                service="checkout",
                queue=self.sqs_service.queue_name,
                status="error",
            ).inc()
            logger.exception("Error processing worker message %s: %s", job_id, exc)
        finally:
            WORKER_IN_FLIGHT_TASKS.labels(
                service="checkout", queue=self.sqs_service.queue_name
            ).dec()

    def _process_batch(self, messages: list[dict[str, Any]]) -> None:
        # At most concurrency tasks may be admitted; finish a wave before polling again.
        for start in range(0, len(messages), self.concurrency):
            futures = []
            for msg in messages[start:start + self.concurrency]:
                if self.shutdown_event.is_set():
                    receipt = msg.get("ReceiptHandle")
                    if receipt:
                        self.sqs_service.reset_visibility(receipt, 0)
                    continue
                if self.executor:
                    futures.append(self.executor.submit(self.process_message, msg))
                else:
                    self.process_message(msg)
            for future in futures:
                try:
                    future.result()
                except Exception:
                    logger.exception("Unexpected worker failure; message remains unacknowledged")

    def _poll_once(self) -> None:
        if not self.sqs_service.queue_url or not self.sqs_service.client:
            return

        messages = self.sqs_service.receive_messages(
            max_messages=min(self.max_messages, self.concurrency),
            wait_time_seconds=self.wait_time_seconds,
        )
        if messages:
            self._process_batch(messages)

    def _run_loop(self) -> None:
        while not self.shutdown_event.is_set():
            if not self.sqs_service.queue_url or not self.sqs_service.client:
                time.sleep(1)
                continue
            self._poll_once()
