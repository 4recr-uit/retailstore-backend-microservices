import os
import unittest
from unittest.mock import MagicMock, patch

from app.config import get_settings
from app.repository import create_repository
from app.worker import create_sqs_service


class RuntimeEnvironmentTests(unittest.TestCase):
    def test_database_credentials_tls_and_pool_come_from_environment(self):
        values = {
            "RETAIL_CHECKOUT_PERSISTENCE_PROVIDER": "postgres", "DB_HOST": "pg.internal",
            "DB_USER": "checkout_app", "DB_PASSWORD": "p a'ss:/?&", "DB_NAME": "retaildb",
            "DB_SSLMODE": "verify-full", "DB_SSLROOTCERT": "/certs/rds.pem",
            "DB_CONNECT_TIMEOUT_SECONDS": "7", "DB_POOL_SIZE": "4", "DB_MAX_OVERFLOW": "2",
            "DB_POOL_TIMEOUT_SECONDS": "8",
        }
        with patch.dict(os.environ, values, clear=True), patch("app.repository.create_engine") as factory:
            factory.return_value = MagicMock()
            create_repository()
            url = factory.call_args.args[0]
            options = factory.call_args.kwargs
        from sqlalchemy.engine import make_url
        self.assertEqual(make_url(url).password, values["DB_PASSWORD"])
        self.assertEqual(options["connect_args"], {"sslmode": "verify-full", "sslrootcert": "/certs/rds.pem", "connect_timeout": 7})
        self.assertEqual((options["pool_size"], options["max_overflow"], options["pool_timeout"]), (4, 2, 8))

    def test_url_owns_tls_parameters(self):
        url = "postgresql://u:p@pg.internal/db?sslmode=verify-full"
        with patch.dict(os.environ, {"DATABASE_URL": url, "DB_SSLMODE": "disable"}, clear=True), patch("app.repository.create_engine") as factory:
            factory.return_value = MagicMock()
            create_repository()
            self.assertEqual(factory.call_args.args[0], url)
            self.assertEqual(factory.call_args.kwargs["connect_args"], {})

    def test_explicit_postgres_does_not_use_embedded_credentials(self):
        with patch.dict(os.environ, {"RETAIL_CHECKOUT_PERSISTENCE_PROVIDER": "postgres", "DB_HOST": "pg.internal"}, clear=True):
            with self.assertRaisesRegex(ValueError, "DB_USER"):
                create_repository()

    def test_region_and_sqs_pool_override(self):
        values = {"AWS_REGION": "ap-southeast-1", "AWS_DEFAULT_REGION": "us-east-1", "SQS_QUEUE_URL": "https://example.test/queue", "SQS_MAX_POOL_CONNECTIONS": "12"}
        with patch.dict(os.environ, values, clear=True), patch("app.worker.boto3.client") as client:
            create_sqs_service()
            self.assertEqual(client.call_args.kwargs["region_name"], "ap-southeast-1")
            self.assertEqual(client.call_args.kwargs["config"].max_pool_connections, 12)


if __name__ == "__main__":
    unittest.main()
