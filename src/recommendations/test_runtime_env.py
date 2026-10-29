import os
import unittest
from unittest.mock import patch

from app.config import get_persistence_config
from app.repository import MySQLRecommendationRepository


class RuntimeEnvironmentTests(unittest.TestCase):
    def test_pool_and_tls_from_environment(self):
        values = {"RETAIL_RECOMMENDATIONS_PERSISTENCE_PROVIDER": "mysql", "RETAIL_RECOMMENDATIONS_PERSISTENCE_ENDPOINT": "mysql.internal:3306", "RETAIL_RECOMMENDATIONS_PERSISTENCE_TLS": "true", "DB_POOL_SIZE": "4", "DB_MAX_OVERFLOW": "1", "DB_POOL_RECYCLE_SECONDS": "90", "DB_POOL_TIMEOUT_SECONDS": "7"}
        with patch.dict(os.environ, values, clear=True), patch("app.repository.create_engine") as factory:
            cfg = get_persistence_config()
            MySQLRecommendationRepository(cfg)
            options = factory.call_args.kwargs
        self.assertEqual((options["pool_size"], options["max_overflow"], options["pool_recycle"], options["pool_timeout"]), (4, 1, 90, 7))
        context = options["connect_args"]["ssl"]
        self.assertTrue(context.check_hostname)
        self.assertEqual(context.verify_mode.name, "CERT_REQUIRED")

    def test_mysql_requires_an_explicit_endpoint(self):
        with patch.dict(os.environ, {"RETAIL_RECOMMENDATIONS_PERSISTENCE_PROVIDER": "mysql"}, clear=True):
            with self.assertRaisesRegex(ValueError, "ENDPOINT"):
                get_persistence_config()


if __name__ == "__main__":
    unittest.main()
