import json
import threading
import unittest
from unittest.mock import MagicMock, patch

from fastapi.testclient import TestClient
from app.worker import BatchPublishError, CheckoutWorker, SQSService
from app.repository import InMemoryCheckoutRepository
from main import create_app


class BatchTests(unittest.TestCase):
    def test_retries_only_retryable_entries_with_same_ids(self):
        client = MagicMock()
        client.send_message_batch.side_effect = [
            {"Successful": [{"Id": "0"}], "Failed": [
                {"Id": "1", "SenderFault": False}, {"Id": "2", "SenderFault": True}]},
            {"Successful": [{"Id": "1"}], "Failed": []}]
        sqs = SQSService("https://queue.test", client)
        with patch("app.worker.time.sleep"), self.assertRaises(BatchPublishError) as caught:
            sqs.publish_batch([{"jobId": "a"}, {"jobId": "b"}, {"jobId": "c"}])
        self.assertEqual(caught.exception.successful_job_ids, ["a", "b"])
        self.assertEqual(caught.exception.failed_job_ids, ["c"])
        second = client.send_message_batch.call_args_list[1].kwargs["Entries"]
        self.assertEqual([entry["Id"] for entry in second], ["1"])
        self.assertEqual(json.loads(second[0]["MessageBody"])["jobId"], "b")

    def test_retries_are_bounded_and_api_reports_only_confirmed_jobs(self):
        client = MagicMock()
        client.send_message_batch.return_value = {"Successful": [{"Id": "0"}],
                                                    "Failed": [{"Id": "1", "SenderFault": False}]}
        sqs = SQSService("https://queue.test", client)
        app = create_app(InMemoryCheckoutRepository(), sqs, app_mode="api")
        with patch("app.worker.time.sleep"), TestClient(app) as api:
            response = api.post("/jobs", json={"batchSize": 2, "workUnits": 1})
        self.assertEqual(response.status_code, 502)
        self.assertEqual(response.json()["enqueued"], 1)
        self.assertEqual(len(response.json()["failedJobIds"]), 1)
        self.assertEqual(client.send_message_batch.call_count, 3)

    def test_transport_error_after_partial_success_preserves_acknowledgements(self):
        client = MagicMock()
        client.send_message_batch.side_effect = [
            {"Successful": [{"Id": "0"}], "Failed": [{"Id": "1", "SenderFault": False}]},
            RuntimeError("connection lost")]
        with patch("app.worker.time.sleep"), self.assertRaises(BatchPublishError) as caught:
            SQSService("https://queue.test", client).publish_batch([{"jobId": "a"}, {"jobId": "b"}])
        self.assertEqual(caught.exception.successful_job_ids, ["a"])


class WorkerTests(unittest.TestCase):
    def worker(self, concurrency=1):
        client = MagicMock()
        sqs = SQSService("https://queue.test", client)
        return CheckoutWorker(sqs, InMemoryCheckoutRepository(), concurrency=concurrency), client

    def test_invalid_messages_are_neither_persisted_nor_deleted(self):
        worker, client = self.worker()
        worker.repository = MagicMock()
        for body in ("not-json", "null", "[]", '{"jobId":"x","workUnits":-1}',
                     '{"jobId":"x","dbMode":"invalid"}', '{"jobId":"x","enqueuedAt":"bad"}'):
            worker.process_message({"Body": body, "ReceiptHandle": "r"})
        worker.repository.record_processed_job.assert_not_called()
        client.delete_message.assert_not_called()

    def test_missing_job_id_uses_stable_sqs_message_id_for_duplicates(self):
        worker, client = self.worker()
        message = {"Body": '{"workUnits":0}', "MessageId": "stable", "ReceiptHandle": "r"}
        worker.process_message(message)
        worker.process_message(message)
        self.assertEqual(len(worker.repository._processed_jobs), 1)
        self.assertEqual(client.delete_message.call_count, 2)

    def test_poll_waits_for_capacity_and_stop_drains_before_closing_executor(self):
        worker, client = self.worker()
        entered, release, polled_again = threading.Event(), threading.Event(), threading.Event()
        def receive(**kwargs):
            if entered.is_set():
                polled_again.set()
                return {"Messages": []}
            return {"Messages": [{"Body": '{"jobId":"a"}', "ReceiptHandle": "r"}]}
        client.receive_message.side_effect = receive
        def process(message):
            entered.set()
            if not release.wait(3): raise RuntimeError("test release timed out")
        worker.process_message = process
        stopped = threading.Event()
        stopper = None
        try:
            worker.start()
            self.assertTrue(entered.wait(2))
            self.assertFalse(polled_again.wait(0.1))
            stopper = threading.Thread(target=lambda: (worker.stop(), stopped.set()))
            stopper.start()
            self.assertFalse(stopped.wait(0.1))
        finally:
            release.set()
            if stopper: stopper.join(3)
            else: worker.stop()
        self.assertTrue(stopped.is_set())
        self.assertFalse(worker.worker_thread.is_alive())
        self.assertEqual(client.receive_message.call_args.kwargs["MaxNumberOfMessages"], 1)


if __name__ == "__main__": unittest.main()
