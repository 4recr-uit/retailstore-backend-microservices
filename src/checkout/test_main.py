# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

import json
import logging
import time
import unittest
from unittest.mock import MagicMock

from fastapi.testclient import TestClient

from main import (
    ChaosController,
    CheckoutWorker,
    InMemoryCheckoutRepository,
    PostgresCheckoutRepository,
    SQSService,
    burn_cpu,
    create_app,
    make_id,
)


class TestCheckoutAPI(unittest.TestCase):
    def setUp(self):
        self.repo = InMemoryCheckoutRepository()
        self.mock_sqs_client = MagicMock()
        self.sqs = SQSService(
            queue_url="http://localhost:4566/000000000000/checkout-queue",
            client=self.mock_sqs_client,
        )
        self.chaos = ChaosController()
        self.app = create_app(
            repository=self.repo,
            sqs_service=self.sqs,
            chaos=self.chaos,
            app_mode="api",
        )
        self.client = TestClient(self.app)

    def test_health_check(self):
        resp = self.client.get("/health")
        self.assertEqual(resp.status_code, 200)
        self.assertEqual(resp.text, "OK")

    def test_topology(self):
        resp = self.client.get("/topology")
        self.assertEqual(resp.status_code, 200)
        data = resp.json()
        self.assertEqual(data["service"], "checkout")
        self.assertEqual(data["mode"], "api")
        self.assertEqual(data["persistenceProvider"], "in-memory")

    def test_get_checkout_not_found(self):
        resp = self.client.get("/checkout/non-existent-user")
        self.assertEqual(resp.status_code, 404)
        self.assertIn("not found", resp.json()["message"].lower())

    def test_update_checkout_without_shipping_address(self):
        payload = {
            "items": [
                {"id": "item-1", "name": "Item 1", "quantity": 2, "price": 100},
                {"id": "item-2", "name": "Item 2", "quantity": 1, "price": 50},
            ],
            "shippingAddress": None,
            "deliveryOptionToken": None,
        }
        resp = self.client.post("/checkout/user-123/update", json=payload)
        self.assertEqual(resp.status_code, 201)
        data = resp.json()
        self.assertEqual(len(data["items"]), 2)
        self.assertEqual(data["items"][0]["totalCost"], 200)
        self.assertEqual(data["items"][1]["totalCost"], 50)
        self.assertEqual(data["subtotal"], 250)
        self.assertEqual(data["tax"], -1)
        self.assertEqual(data["shipping"], -1)
        self.assertEqual(data["total"], 250)
        self.assertIsNotNone(data["paymentId"])
        self.assertIsNotNone(data["paymentToken"])
        self.assertIsNone(data["shippingRates"])

    def test_update_checkout_with_shipping_address_and_rates(self):
        payload = {
            "items": [
                {"id": "item-1", "name": "Item 1", "quantity": 1, "price": 100},
            ],
            "shippingAddress": {
                "firstName": "John",
                "lastName": "Doe",
                "address1": "123 Main St",
                "city": "Seattle",
                "state": "WA",
                "zip": "98101",
                "email": "john.doe@example.com",
            },
            "deliveryOptionToken": "priority-mail",
        }
        resp = self.client.post("/checkout/user-456/update", json=payload)
        self.assertEqual(resp.status_code, 201)
        data = resp.json()
        self.assertEqual(data["subtotal"], 100)
        self.assertEqual(data["tax"], 5)
        self.assertEqual(data["shipping"], 10)
        self.assertEqual(data["total"], 115)  # 100 + 5 + 10
        self.assertIsNotNone(data["shippingRates"])
        self.assertEqual(len(data["shippingRates"]["rates"]), 2)

        # Also verify GET returns the updated checkout session
        get_resp = self.client.get("/checkout/user-456")
        self.assertEqual(get_resp.status_code, 200)
        self.assertEqual(get_resp.json()["total"], 115)

    def test_submit_checkout_flow(self):
        # 1. Update session
        payload = {
            "items": [
                {"id": "item-1", "name": "Item 1", "quantity": 1, "price": 200},
            ],
            "shippingAddress": {
                "firstName": "Alice",
                "lastName": "Smith",
                "address1": "456 Market St",
                "city": "San Francisco",
                "state": "CA",
                "zip": "94105",
                "email": "alice@example.com",
            },
            "deliveryOptionToken": "priority-mail-express",
        }
        update_resp = self.client.post("/checkout/cust-789/update", json=payload)
        self.assertEqual(update_resp.status_code, 201)

        # 2. Submit checkout
        submit_resp = self.client.post("/checkout/cust-789/submit")
        self.assertEqual(submit_resp.status_code, 201)
        submitted = submit_resp.json()

        self.assertTrue(len(submitted["orderId"]) > 0)
        self.assertEqual(submitted["email"], "alice@example.com")
        self.assertEqual(submitted["subtotal"], 200)
        self.assertEqual(submitted["shipping"], 25)
        self.assertEqual(submitted["tax"], 5)
        self.assertEqual(submitted["total"], 230)

        # Verify SQS send_message was called with orderId and payload
        self.mock_sqs_client.send_message.assert_called_once()
        call_kwargs = self.mock_sqs_client.send_message.call_args.kwargs
        self.assertEqual(call_kwargs["QueueUrl"], "http://localhost:4566/000000000000/checkout-queue")
        sent_body = json.loads(call_kwargs["MessageBody"])
        self.assertEqual(sent_body["orderId"], submitted["orderId"])
        self.assertEqual(sent_body["jobId"], submitted["orderId"])
        self.assertEqual(sent_body["customerId"], "cust-789")

        # Verify session was deleted
        get_after = self.client.get("/checkout/cust-789")
        self.assertEqual(get_after.status_code, 404)

    def test_submit_checkout_not_found(self):
        resp = self.client.post("/checkout/unknown-cust/submit")
        self.assertEqual(resp.status_code, 404)

    def test_jobs_endpoint_single(self):
        payload = {
            "batchSize": 1,
            "workUnits": 50,
            "dbMode": "light",
            "customerId": "cust-job-1",
            "payload": {"key": "value"},
        }
        resp = self.client.post("/jobs", json=payload)
        self.assertEqual(resp.status_code, 201)
        data = resp.json()
        self.assertEqual(data["enqueued"], 1)
        self.assertEqual(len(data["jobIds"]), 1)
        self.mock_sqs_client.send_message.assert_called()

    def test_jobs_endpoint_batch(self):
        payload = {
            "batchSize": 5,
            "workUnits": 20,
            "dbMode": "none",
            "customerId": "cust-batch",
            "payload": {"batch": True},
        }
        resp = self.client.post("/jobs", json=payload)
        self.assertEqual(resp.status_code, 201)
        data = resp.json()
        self.assertEqual(data["enqueued"], 5)
        self.assertEqual(len(data["jobIds"]), 5)
        self.mock_sqs_client.send_message_batch.assert_called()

    def test_jobs_endpoint_validation(self):
        # Invalid workUnits < 1
        resp = self.client.post("/jobs", json={"workUnits": 0})
        self.assertEqual(resp.status_code, 400)

        # Invalid workUnits > 10000
        resp = self.client.post("/jobs", json={"workUnits": 10001})
        self.assertEqual(resp.status_code, 400)

        # Invalid batchSize < 1
        resp = self.client.post("/jobs", json={"batchSize": 0})
        self.assertEqual(resp.status_code, 400)

        # Invalid batchSize > 10
        resp = self.client.post("/jobs", json={"batchSize": 11})
        self.assertEqual(resp.status_code, 400)

        # Invalid dbMode
        resp = self.client.post("/jobs", json={"dbMode": "unsupported"})
        self.assertEqual(resp.status_code, 400)

    def test_chaos_controls(self):
        # 1. Error status
        self.client.post("/chaos/status/503")
        status_resp = self.client.get("/chaos/status")
        self.assertTrue(status_resp.json()["error_status"]["enabled"])
        self.assertEqual(status_resp.json()["error_status"]["code"], 503)

        # Health should still return OK because /chaos and /health are unblocked
        # But normal API endpoints return 503
        resp = self.client.get("/checkout/foo")
        self.assertEqual(resp.status_code, 503)

        # Disable error status
        self.client.delete("/chaos/status")
        resp = self.client.get("/checkout/foo")
        self.assertEqual(resp.status_code, 404)

        # 2. Latency
        self.client.post("/chaos/latency/20")
        status_resp = self.client.get("/chaos/status")
        self.assertTrue(status_resp.json()["latency"]["enabled"])
        self.assertEqual(status_resp.json()["latency"]["value"], 20)
        self.client.delete("/chaos/latency")

        # 3. Health toggle
        self.client.post("/chaos/health")
        resp = self.client.get("/health")
        self.assertEqual(resp.status_code, 500)
        self.assertEqual(resp.text, "Unhealthy")

        self.client.delete("/chaos/health")
        resp = self.client.get("/health")
        self.assertEqual(resp.status_code, 200)
        self.assertEqual(resp.text, "OK")

    def test_metrics_endpoint(self):
        resp = self.client.get("/metrics")
        self.assertEqual(resp.status_code, 200)
        text_content = resp.text

        # Verify all 7 Prometheus metrics are present
        required_metrics = [
            "app_sqs_messages_published_total",
            "worker_tasks_processed_total",
            "worker_task_duration_seconds",
            "worker_job_queue_wait_seconds",
            "worker_job_e2e_latency_seconds",
            "worker_cold_start_timestamp_seconds",
            "worker_in_flight_tasks",
        ]
        for metric in required_metrics:
            self.assertIn(metric, text_content, f"Metric {metric} missing from /metrics")


class TestWorkerAndUtilities(unittest.TestCase):
    def test_make_id(self):
        id_16 = make_id(16)
        id_32 = make_id(32)
        self.assertEqual(len(id_16), 16)
        self.assertEqual(len(id_32), 32)
        self.assertNotEqual(id_16, make_id(16))

    def test_cpu_burn_calibration(self):
        # 1 workUnit = 1 ms. 25 workUnits should take >= 20ms and <= 100ms
        t0 = time.perf_counter()
        burn_cpu(25)
        elapsed_ms = (time.perf_counter() - t0) * 1000
        self.assertGreaterEqual(elapsed_ms, 20.0)
        self.assertLess(elapsed_ms, 120.0)

    def test_worker_process_message_modes(self):
        repo = InMemoryCheckoutRepository()
        mock_sqs = MagicMock()
        sqs_svc = SQSService(queue_url="http://localhost:4566/000000000000/checkout-queue", client=mock_sqs)
        worker = CheckoutWorker(sqs_service=sqs_svc, repository=repo, concurrency=1)

        # 1. dbMode=light
        msg_light = {
            "ReceiptHandle": "receipt-1",
            "Body": json.dumps({
                "jobId": "job-light-1",
                "orderId": "order-1",
                "enqueuedAt": int(time.time() * 1000) - 100,
                "workUnits": 5,
                "dbMode": "light",
                "customerId": "cust-1",
            }),
        }
        worker.process_message(msg_light)
        self.assertIn("job-light-1", repo._processed_jobs)
        mock_sqs.delete_message.assert_called_with(
            QueueUrl="http://localhost:4566/000000000000/checkout-queue",
            ReceiptHandle="receipt-1",
        )

        # 2. Duplicate detection
        worker.process_message(msg_light)  # duplicate
        # Still deletes message from queue
        self.assertEqual(mock_sqs.delete_message.call_count, 2)

        # 3. dbMode=none
        msg_none = {
            "ReceiptHandle": "receipt-2",
            "Body": json.dumps({
                "jobId": "job-none-1",
                "orderId": "order-2",
                "workUnits": 2,
                "dbMode": "none",
            }),
        }
        worker.process_message(msg_none)
        self.assertNotIn("job-none-1", repo._processed_jobs)

        # 4. dbMode=full
        msg_full = {
            "ReceiptHandle": "receipt-3",
            "Body": json.dumps({
                "jobId": "job-full-1",
                "orderId": "order-3",
                "workUnits": 2,
                "dbMode": "full",
                "payload": {"fullData": "saved"},
            }),
        }
        worker.process_message(msg_full)
        self.assertIn("job-full-1", repo._processed_jobs)
        self.assertEqual(repo._processed_jobs["job-full-1"]["data"]["fullData"], "saved")

    def test_worker_is_first_job_flag(self):
        repo = InMemoryCheckoutRepository()
        mock_sqs = MagicMock()
        sqs_svc = SQSService(queue_url="http://localhost:4566/000000000000/checkout-queue", client=mock_sqs)
        worker = CheckoutWorker(sqs_service=sqs_svc, repository=repo)

        self.assertTrue(worker.is_first_job)
        msg1 = {"ReceiptHandle": "r1", "Body": json.dumps({"jobId": "j1", "workUnits": 1})}
        worker.process_message(msg1)
        self.assertFalse(worker.is_first_job)

        msg2 = {"ReceiptHandle": "r2", "Body": json.dumps({"jobId": "j2", "workUnits": 1})}
        worker.process_message(msg2)
        self.assertFalse(worker.is_first_job)

    def test_worker_graceful_shutdown_visibility_reset(self):
        repo = InMemoryCheckoutRepository()
        mock_sqs = MagicMock()
        sqs_svc = SQSService(queue_url="http://localhost:4566/000000000000/checkout-queue", client=mock_sqs)
        worker = CheckoutWorker(sqs_service=sqs_svc, repository=repo, concurrency=1, max_messages=5)

        # Set shutdown event
        worker.shutdown_event.set()

        messages = [
            {"ReceiptHandle": "h1", "Body": "{}"},
            {"ReceiptHandle": "h2", "Body": "{}"},
        ]

        # Process batch after shutdown has been signaled
        worker._process_batch(messages)

        # Verify change_message_visibility was called for unstarted messages
        self.assertEqual(mock_sqs.change_message_visibility.call_count, 2)
        mock_sqs.change_message_visibility.assert_any_call(
            QueueUrl="http://localhost:4566/000000000000/checkout-queue",
            ReceiptHandle="h1",
            VisibilityTimeout=0,
        )

    def test_structured_json_logging(self):
        repo = InMemoryCheckoutRepository()
        mock_sqs = MagicMock()
        sqs_svc = SQSService(queue_url="http://localhost:4566/000000000000/checkout-queue", client=mock_sqs)
        worker = CheckoutWorker(sqs_service=sqs_svc, repository=repo)

        msg = {
            "ReceiptHandle": "r-log",
            "Body": json.dumps({
                "jobId": "job-log-123",
                "orderId": "order-log-456",
                "customerId": "cust-log",
                "enqueuedAt": int(time.time() * 1000) - 50,
                "workUnits": 5,
                "dbMode": "light",
            }),
        }

        with self.assertLogs("checkout", level="INFO") as cm:
            worker.process_message(msg)

        found = False
        for rec in cm.output:
            try:
                # rec is formatted as "INFO:checkout:{...}"
                log_text = rec.split(":", 2)[-1].strip()
                entry = json.loads(log_text)
                if entry.get("event") == "job_completed" and entry.get("jobId") == "job-log-123":
                    found = True
                    self.assertEqual(entry["orderId"], "order-log-456")
                    self.assertEqual(entry["customerId"], "cust-log")
                    self.assertIn("durationMs", entry)
                    self.assertIn("e2eMs", entry)
                    self.assertIn("queueWaitMs", entry)
                    self.assertEqual(entry["status"], "COMPLETED")
                    break
            except Exception:
                continue
        self.assertTrue(found, f"Structured JSON log not found in logger output: {cm.output}")


class TestPostgresRepositorySchema(unittest.TestCase):
    def test_postgres_repository_initialization(self):
        mock_engine = MagicMock()
        mock_conn = MagicMock()
        mock_engine.begin.return_value.__enter__.return_value = mock_conn

        repo = PostgresCheckoutRepository(engine=mock_engine)
        self.assertEqual(repo.provider, "postgres")

        # Verify SQL migration executed
        mock_conn.execute.assert_called_once()
        sql_called = str(mock_conn.execute.call_args[0][0])
        self.assertIn("CREATE SCHEMA IF NOT EXISTS checkout", sql_called)
        self.assertIn("CREATE TABLE IF NOT EXISTS checkout.checkout_sessions", sql_called)
        self.assertIn("CREATE TABLE IF NOT EXISTS checkout.processed_jobs", sql_called)


if __name__ == "__main__":
    unittest.main()
