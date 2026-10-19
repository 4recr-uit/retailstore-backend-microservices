# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

import json
import logging
import os
import time
import unittest
from unittest.mock import MagicMock, patch

from fastapi.testclient import TestClient

from main import (
    ChaosController,
    CheckoutWorker,
    InMemoryCheckoutRepository,
    PostgresCheckoutRepository,
    SQSService,
    Settings,
    burn_cpu,
    create_app,
    create_repository,
    get_settings,
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

    def test_get_session(self):
        mock_engine = MagicMock()
        mock_conn = MagicMock()
        mock_engine.connect.return_value.__enter__.return_value = mock_conn
        mock_engine.begin.return_value.__enter__.return_value = MagicMock()

        repo = PostgresCheckoutRepository(engine=mock_engine)

        # 1. When row is None -> returns None
        mock_conn.execute.return_value.fetchone.return_value = None
        self.assertIsNone(repo.get_session("user-none"))

        # 2. When row[0] is a JSON string -> returns parsed dict
        mock_conn.execute.return_value.fetchone.return_value = ('{"subtotal": 100, "items": []}',)
        result = repo.get_session("user-json")
        self.assertEqual(result, {"subtotal": 100, "items": []})

        # 3. When row[0] is already a dict -> returns dict
        mock_conn.execute.return_value.fetchone.return_value = ({"subtotal": 200, "items": ["item1"]},)
        result2 = repo.get_session("user-dict")
        self.assertEqual(result2, {"subtotal": 200, "items": ["item1"]})

    def test_save_session(self):
        mock_engine = MagicMock()
        mock_conn = MagicMock()
        mock_engine.begin.return_value.__enter__.return_value = mock_conn

        repo = PostgresCheckoutRepository(engine=mock_engine)
        mock_conn.reset_mock()

        data = {"subtotal": 150, "tax": 5}
        repo.save_session("user-123", data)

        mock_conn.execute.assert_called_once()
        stmt_called, params_called = mock_conn.execute.call_args[0]
        sql_text = str(stmt_called)
        self.assertIn("INSERT INTO checkout.checkout_sessions", sql_text)
        self.assertIn("ON CONFLICT", sql_text)
        self.assertEqual(params_called["cid"], "user-123")
        self.assertEqual(json.loads(params_called["data"]), data)

    def test_delete_session(self):
        mock_engine = MagicMock()
        mock_conn = MagicMock()
        mock_engine.begin.return_value.__enter__.return_value = mock_conn

        repo = PostgresCheckoutRepository(engine=mock_engine)
        mock_res = MagicMock()
        mock_conn.execute.return_value = mock_res

        # rowcount == 1 -> returns True
        mock_res.rowcount = 1
        self.assertTrue(repo.delete_session("user-123"))
        stmt_called, params_called = mock_conn.execute.call_args[0]
        self.assertIn("DELETE FROM checkout.checkout_sessions", str(stmt_called))
        self.assertEqual(params_called["cid"], "user-123")

        # rowcount == 0 -> returns False
        mock_res.rowcount = 0
        self.assertFalse(repo.delete_session("user-unknown"))

    def test_record_processed_job(self):
        mock_engine = MagicMock()
        mock_conn = MagicMock()
        mock_engine.begin.return_value.__enter__.return_value = mock_conn

        repo = PostgresCheckoutRepository(engine=mock_engine)
        mock_res = MagicMock()
        mock_conn.execute.return_value = mock_res

        # rowcount == 1 -> returns True
        mock_res.rowcount = 1
        result = repo.record_processed_job(
            job_id="job-1",
            order_id="order-1",
            customer_id="cust-1",
            status_val="COMPLETED",
            data={"workUnits": 5},
        )
        self.assertTrue(result)
        stmt_called, params_called = mock_conn.execute.call_args[0]
        self.assertIn("INSERT INTO checkout.processed_jobs", str(stmt_called))
        self.assertIn("ON CONFLICT (job_id) DO NOTHING", str(stmt_called))
        self.assertEqual(params_called["jid"], "job-1")
        self.assertEqual(params_called["oid"], "order-1")
        self.assertEqual(params_called["cid"], "cust-1")
        self.assertEqual(params_called["status"], "COMPLETED")
        self.assertEqual(json.loads(params_called["data"]), {"workUnits": 5})

        # rowcount == 0 -> duplicate, returns False
        mock_res.rowcount = 0
        self.assertFalse(
            repo.record_processed_job(
                job_id="job-1",
                order_id="order-1",
                customer_id="cust-1",
                status_val="COMPLETED",
                data=None,
            )
        )

    def test_create_repository_default(self):
        with patch.dict(os.environ, {}, clear=True):
            repo = create_repository()
            self.assertIsInstance(repo, InMemoryCheckoutRepository)
            self.assertEqual(repo.provider, "in-memory")

    def test_create_repository_postgres_url_normalization(self):
        env = {"DATABASE_URL": "postgres://user:pass@host:5432/db"}
        with patch.dict(os.environ, env, clear=True):
            with patch("app.repository.create_engine") as mock_ce:
                mock_engine = MagicMock()
                mock_ce.return_value = mock_engine
                repo = create_repository()
                self.assertIsInstance(repo, PostgresCheckoutRepository)
                mock_ce.assert_called_once()
                called_url = mock_ce.call_args[0][0]
                self.assertEqual(called_url, "postgresql://user:pass@host:5432/db")

    def test_create_repository_fallback_on_exception(self):
        env = {"DATABASE_URL": "postgres://user:pass@host:5432/db"}
        # Case 1: create_engine raises exception
        with patch.dict(os.environ, env, clear=True):
            with patch("app.repository.create_engine", side_effect=Exception("Connection refused")):
                repo = create_repository()
                self.assertIsInstance(repo, InMemoryCheckoutRepository)

        # Case 2: _init_db raises exception
        with patch.dict(os.environ, env, clear=True):
            with patch("app.repository.create_engine") as mock_ce:
                mock_engine = MagicMock()
                mock_engine.begin.side_effect = Exception("Migration failed")
                mock_ce.return_value = mock_engine
                repo = create_repository()
                self.assertIsInstance(repo, InMemoryCheckoutRepository)


class TestConfigSettings(unittest.TestCase):
    def test_sqs_max_messages_clamping(self):
        # 0 -> 1
        with patch.dict(os.environ, {"SQS_MAX_MESSAGES": "0"}, clear=True):
            self.assertEqual(get_settings().sqs_max_messages, 1)
        # 99 -> 10
        with patch.dict(os.environ, {"SQS_MAX_MESSAGES": "99"}, clear=True):
            self.assertEqual(get_settings().sqs_max_messages, 10)
        # "invalid" -> 1
        with patch.dict(os.environ, {"SQS_MAX_MESSAGES": "invalid"}, clear=True):
            self.assertEqual(get_settings().sqs_max_messages, 1)
        # "5" -> 5
        with patch.dict(os.environ, {"SQS_MAX_MESSAGES": "5"}, clear=True):
            self.assertEqual(get_settings().sqs_max_messages, 5)

        # Also test Settings constructor directly
        self.assertEqual(Settings(sqs_max_messages=0).sqs_max_messages, 1)
        self.assertEqual(Settings(sqs_max_messages=99).sqs_max_messages, 10)
        self.assertEqual(Settings(sqs_max_messages="invalid").sqs_max_messages, 1)
        self.assertEqual(Settings(sqs_max_messages="5").sqs_max_messages, 5)

    def test_worker_concurrency_clamping(self):
        # 0 -> 1
        with patch.dict(os.environ, {"WORKER_CONCURRENCY": "0"}, clear=True):
            self.assertEqual(get_settings().worker_concurrency, 1)
        # "invalid" -> 1
        with patch.dict(os.environ, {"WORKER_CONCURRENCY": "invalid"}, clear=True):
            self.assertEqual(get_settings().worker_concurrency, 1)
        # "4" -> 4
        with patch.dict(os.environ, {"WORKER_CONCURRENCY": "4"}, clear=True):
            self.assertEqual(get_settings().worker_concurrency, 4)

        # Also test Settings constructor directly
        self.assertEqual(Settings(worker_concurrency=0).worker_concurrency, 1)
        self.assertEqual(Settings(worker_concurrency="invalid").worker_concurrency, 1)
        self.assertEqual(Settings(worker_concurrency="4").worker_concurrency, 4)

    def test_queue_name_extraction(self):
        with patch.dict(
            os.environ,
            {"SQS_QUEUE_URL": "https://sqs.us-east-1.amazonaws.com/123456789012/my-test-queue"},
            clear=True,
        ):
            self.assertEqual(get_settings().queue_name, "my-test-queue")

        with patch.dict(
            os.environ,
            {"SQS_QUEUE_URL": "https://sqs.us-east-1.amazonaws.com/123456789012/my-test-queue/"},
            clear=True,
        ):
            self.assertEqual(get_settings().queue_name, "my-test-queue")

        with patch.dict(
            os.environ,
            {"SQS_QUEUE_URL": "", "SQS_QUEUE_NAME": "custom-queue"},
            clear=True,
        ):
            self.assertEqual(get_settings().queue_name, "custom-queue")

    def test_persistence_provider_resolution(self):
        # Empty env -> in-memory
        with patch.dict(os.environ, {}, clear=True):
            self.assertEqual(get_settings().persistence_provider, "in-memory")

        # DB_HOST set -> postgres
        with patch.dict(os.environ, {"DB_HOST": "postgres.local"}, clear=True):
            self.assertEqual(get_settings().persistence_provider, "postgres")

        # DATABASE_URL set -> postgres
        with patch.dict(os.environ, {"DATABASE_URL": "postgres://user:pass@host/db"}, clear=True):
            self.assertEqual(get_settings().persistence_provider, "postgres")

        # Explicit RETAIL_CHECKOUT_PERSISTENCE_PROVIDER="in-memory" overrides DB_HOST
        with patch.dict(
            os.environ,
            {"RETAIL_CHECKOUT_PERSISTENCE_PROVIDER": "in-memory", "DB_HOST": "postgres.local"},
            clear=True,
        ):
            self.assertEqual(get_settings().persistence_provider, "in-memory")

        # Explicit RETAIL_CHECKOUT_PERSISTENCE_PROVIDER="postgres"
        with patch.dict(
            os.environ,
            {"RETAIL_CHECKOUT_PERSISTENCE_PROVIDER": "postgres"},
            clear=True,
        ):
            self.assertEqual(get_settings().persistence_provider, "postgres")


class TestChaosAndActuatorEndpoints(unittest.TestCase):
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

    def test_actuator_health_endpoints_healthy(self):
        endpoints = ["/actuator/health", "/actuator/health/liveness", "/actuator/health/readiness"]
        for ep in endpoints:
            resp = self.client.get(ep)
            self.assertEqual(resp.status_code, 200, f"Expected 200 for {ep}")
            self.assertEqual(resp.json(), {"status": "UP"})

    def test_actuator_health_endpoints_unhealthy_via_chaos(self):
        # Toggle chaos health off
        post_resp = self.client.post("/chaos/health")
        self.assertEqual(post_resp.status_code, 200)

        endpoints = ["/actuator/health", "/actuator/health/liveness", "/actuator/health/readiness"]
        for ep in endpoints:
            resp = self.client.get(ep)
            self.assertEqual(resp.status_code, 503, f"Expected 503 for {ep}")
            self.assertEqual(resp.json(), {"status": "DOWN"})

        # Toggle back to healthy
        del_resp = self.client.delete("/chaos/health")
        self.assertEqual(del_resp.status_code, 200)

        for ep in endpoints:
            resp = self.client.get(ep)
            self.assertEqual(resp.status_code, 200, f"Expected 200 for {ep}")
            self.assertEqual(resp.json(), {"status": "UP"})

    def test_invalid_chaos_inputs(self):
        # Status code < 100 -> 400
        resp_99 = self.client.post("/chaos/status/99")
        self.assertEqual(resp_99.status_code, 400)

        # Status code > 599 -> 400
        resp_600 = self.client.post("/chaos/status/600")
        self.assertEqual(resp_600.status_code, 400)

        # Latency < 0 -> 400
        resp_neg = self.client.post("/chaos/latency/-5")
        self.assertEqual(resp_neg.status_code, 400)

    def test_worker_mode_router_isolation(self):
        worker_app = create_app(
            repository=self.repo,
            sqs_service=self.sqs,
            chaos=self.chaos,
            app_mode="worker",
        )
        worker_client = TestClient(worker_app)

        # System and chaos endpoints return 200
        self.assertEqual(worker_client.get("/health").status_code, 200)
        self.assertEqual(worker_client.get("/topology").status_code, 200)
        self.assertEqual(worker_client.get("/metrics").status_code, 200)
        self.assertEqual(worker_client.get("/chaos/status").status_code, 200)
        self.assertEqual(worker_client.get("/actuator/health").status_code, 200)

        # API endpoint returns 404 because api_router is not mounted in worker mode
        self.assertEqual(worker_client.get("/checkout/user-1").status_code, 404)
        self.assertEqual(worker_client.post("/jobs", json={"workUnits": 10}).status_code, 404)


class TestSQSServiceUnit(unittest.TestCase):
    def test_publish_single_empty_queue_url_or_client(self):
        # Empty queue_url
        sqs_empty_url = SQSService(queue_url="", client=MagicMock())
        self.assertFalse(sqs_empty_url.publish_single({"jobId": "test"}))

        # None client
        sqs_none_client = SQSService(queue_url="http://queue", client=None)
        self.assertFalse(sqs_none_client.publish_single({"jobId": "test"}))

    def test_publish_batch_empty_queue_url_or_empty_messages(self):
        # Empty queue_url
        sqs_empty_url = SQSService(queue_url="", client=MagicMock())
        self.assertEqual(sqs_empty_url.publish_batch([{"jobId": "1"}]), 0)

        # None client
        sqs_none_client = SQSService(queue_url="http://queue", client=None)
        self.assertEqual(sqs_none_client.publish_batch([{"jobId": "1"}]), 0)

        # Empty messages list
        sqs_empty_msgs = SQSService(queue_url="http://queue", client=MagicMock())
        self.assertEqual(sqs_empty_msgs.publish_batch([]), 0)

    def test_receive_messages_empty_queue_url_or_client(self):
        # Empty queue_url
        sqs_empty_url = SQSService(queue_url="", client=MagicMock())
        self.assertEqual(sqs_empty_url.receive_messages(), [])

        # None client
        sqs_none_client = SQSService(queue_url="http://queue", client=None)
        self.assertEqual(sqs_none_client.receive_messages(), [])

    def test_receive_messages_client_exception(self):
        mock_client = MagicMock()
        mock_client.receive_message.side_effect = Exception("SQS network error")
        sqs = SQSService(queue_url="http://queue", client=mock_client)
        # Should catch exception, log it, and return empty list without crashing
        result = sqs.receive_messages()
        self.assertEqual(result, [])

    def test_receive_messages_success(self):
        mock_client = MagicMock()
        mock_client.receive_message.return_value = {
            "Messages": [{"ReceiptHandle": "h-123", "Body": '{"jobId": "1"}'}]
        }
        sqs = SQSService(queue_url="http://queue", client=mock_client)
        result = sqs.receive_messages(max_messages=5, wait_time_seconds=10)
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0]["ReceiptHandle"], "h-123")
        mock_client.receive_message.assert_called_once_with(
            QueueUrl="http://queue",
            MaxNumberOfMessages=5,
            WaitTimeSeconds=10,
        )

    def test_delete_message_empty_queue_url_or_client(self):
        # Empty queue_url
        sqs_empty_url = SQSService(queue_url="", client=MagicMock())
        self.assertFalse(sqs_empty_url.delete_message("handle-1"))

        # None client
        sqs_none_client = SQSService(queue_url="http://queue", client=None)
        self.assertFalse(sqs_none_client.delete_message("handle-1"))

        # Empty receipt_handle
        sqs = SQSService(queue_url="http://queue", client=MagicMock())
        self.assertFalse(sqs.delete_message(""))

    def test_delete_message_client_exception(self):
        mock_client = MagicMock()
        mock_client.delete_message.side_effect = Exception("Receipt handle invalid")
        sqs = SQSService(queue_url="http://queue", client=mock_client)
        # Caught and returns False without crashing
        self.assertFalse(sqs.delete_message("handle-1"))

    def test_delete_message_success(self):
        mock_client = MagicMock()
        sqs = SQSService(queue_url="http://queue", client=mock_client)
        self.assertTrue(sqs.delete_message("handle-1"))
        mock_client.delete_message.assert_called_once_with(
            QueueUrl="http://queue",
            ReceiptHandle="handle-1",
        )

    def test_reset_visibility_empty_queue_url_or_client(self):
        # Empty queue_url
        sqs_empty_url = SQSService(queue_url="", client=MagicMock())
        self.assertFalse(sqs_empty_url.reset_visibility("handle-1"))

        # None client
        sqs_none_client = SQSService(queue_url="http://queue", client=None)
        self.assertFalse(sqs_none_client.reset_visibility("handle-1"))

        # Empty receipt_handle
        sqs = SQSService(queue_url="http://queue", client=MagicMock())
        self.assertFalse(sqs.reset_visibility(""))

    def test_reset_visibility_client_exception(self):
        mock_client = MagicMock()
        mock_client.change_message_visibility.side_effect = Exception("Throttled")
        sqs = SQSService(queue_url="http://queue", client=mock_client)
        # Caught and returns False without crashing
        self.assertFalse(sqs.reset_visibility("handle-1", 0))

    def test_reset_visibility_success(self):
        mock_client = MagicMock()
        sqs = SQSService(queue_url="http://queue", client=mock_client)
        self.assertTrue(sqs.reset_visibility("handle-1", 0))
        mock_client.change_message_visibility.assert_called_once_with(
            QueueUrl="http://queue",
            ReceiptHandle="handle-1",
            VisibilityTimeout=0,
        )


if __name__ == "__main__":
    unittest.main()
