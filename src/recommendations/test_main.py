import os
import unittest
from unittest.mock import patch

from fastapi.testclient import TestClient
from sqlalchemy import create_engine, text
from sqlalchemy.pool import StaticPool

from main import RecommendationsRepository, create_app, get_persistence_config


class TestRecommendationsService(unittest.TestCase):
    def setUp(self) -> None:
        self.in_memory_repo = RecommendationsRepository(
            {
                "provider": "in-memory",
                "endpoint": "",
                "db_name": "catalogdb",
                "db_user": "catalog_user",
                "db_password": "",
                "connect_timeout": 5,
            }
        )
        self.client = TestClient(create_app(self.in_memory_repo))

    def test_health_endpoint(self) -> None:
        resp = self.client.get("/health")
        self.assertEqual(resp.status_code, 200)
        self.assertEqual(resp.text, "OK")

    def test_metrics_endpoint(self) -> None:
        self.client.get("/recommendations/cc789f85-1476-452a-8100-9e74502198e0")
        resp = self.client.get("/metrics")
        self.assertEqual(resp.status_code, 200)
        self.assertIn("recommendations_http_requests_total", resp.text)
        self.assertIn("recommendations_http_request_duration_seconds", resp.text)

    def test_in_memory_recommendations_shared_tags(self) -> None:
        # "cc789f85-1476-452a-8100-9e74502198e0" has tag "accessories"
        product_id = "cc789f85-1476-452a-8100-9e74502198e0"
        resp = self.client.get(f"/recommendations/{product_id}")
        self.assertEqual(resp.status_code, 200)
        data = resp.json()
        self.assertIsInstance(data, list)
        returned_ids = [item["productId"] for item in data]
        self.assertNotIn(product_id, returned_ids)
        self.assertEqual(
            returned_ids,
            [
                "1ca35e86-4b4c-4124-b6b5-076ba4134d0d",
                "631a3db5-ac07-492c-a994-8cd56923c112",
                "d27cf49f-b689-4a75-a249-d373e0330bb5",
                "d4edfedb-dbe9-4dd9-aae8-009489394955",
            ],
        )

    def test_in_memory_recommendations_fallback_when_no_shared_tags(self) -> None:
        # "8757729a-c518-4356-8694-9e795a9b3237" ("Forget-Me-Pop") is the only "food" item
        product_id = "8757729a-c518-4356-8694-9e795a9b3237"
        resp = self.client.get(f"/recommendations/{product_id}")
        self.assertEqual(resp.status_code, 200)
        data = resp.json()
        self.assertEqual(len(data), 4)
        returned_ids = [item["productId"] for item in data]
        self.assertNotIn(product_id, returned_ids)

    def test_product_not_found_returns_404(self) -> None:
        resp = self.client.get("/recommendations/00000000-0000-0000-0000-000000000000")
        self.assertEqual(resp.status_code, 404)
        self.assertEqual(resp.json(), {"code": 404, "message": "Product not found"})

    def test_invalid_product_id_returns_400(self) -> None:
        resp = self.client.get("/recommendations/%20%20")
        self.assertEqual(resp.status_code, 400)
        body = resp.json()
        self.assertEqual(body["code"], 400)
        self.assertIn("message", body)

        too_long = "a" * 65
        resp2 = self.client.get(f"/recommendations/{too_long}")
        self.assertEqual(resp2.status_code, 400)
        self.assertEqual(resp2.json()["code"], 400)

    def test_sql_repository_queries(self) -> None:
        engine = create_engine(
            "sqlite:///:memory:",
            connect_args={"check_same_thread": False},
            poolclass=StaticPool,
        )
        with engine.begin() as conn:
            conn.execute(text("CREATE TABLE products (id VARCHAR(64) PRIMARY KEY, name VARCHAR(255))"))
            conn.execute(
                text("CREATE TABLE product_tags (product_id VARCHAR(64), tag_name VARCHAR(64))")
            )
            conn.execute(
                text(
                    """
                    INSERT INTO products (id, name) VALUES
                    ('p1', 'Prod 1'),
                    ('p2', 'Prod 2'),
                    ('p3', 'Prod 3'),
                    ('p4', 'Solo Prod')
                    """
                )
            )
            conn.execute(
                text(
                    """
                    INSERT INTO product_tags (product_id, tag_name) VALUES
                    ('p1', 'clothing'),
                    ('p2', 'clothing'),
                    ('p3', 'clothing'),
                    ('p4', 'unique_tag')
                    """
                )
            )

        sql_repo = RecommendationsRepository(
            {
                "provider": "mysql",
                "endpoint": "mysql:3306",
                "db_name": "catalogdb",
                "db_user": "catalog_user",
                "db_password": "secret",
                "connect_timeout": 5,
            },
            engine=engine,
        )
        client = TestClient(create_app(sql_repo))

        # Shared tag recommendations
        r1 = client.get("/recommendations/p1")
        self.assertEqual(r1.status_code, 200)
        self.assertEqual(r1.json(), [{"productId": "p2"}, {"productId": "p3"}])

        # Fallback recommendations when no shared tags
        r2 = client.get("/recommendations/p4")
        self.assertEqual(r2.status_code, 200)
        self.assertEqual(
            r2.json(),
            [{"productId": "p1"}, {"productId": "p2"}, {"productId": "p3"}],
        )

        # Missing product -> 404
        r3 = client.get("/recommendations/unknown")
        self.assertEqual(r3.status_code, 404)
        self.assertEqual(r3.json(), {"code": 404, "message": "Product not found"})

    def test_persistence_config_resolution(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            cfg = get_persistence_config()
            self.assertEqual(cfg["provider"], "in-memory")

        with patch.dict(os.environ, {"DB_HOST": "rds-mysql.internal"}, clear=True):
            cfg = get_persistence_config()
            self.assertEqual(cfg["provider"], "mysql")
            self.assertEqual(cfg["endpoint"], "rds-mysql.internal:3306")

        with patch.dict(
            os.environ,
            {
                "RETAIL_RECOMMENDATIONS_PERSISTENCE_PROVIDER": "mysql",
                "RETAIL_RECOMMENDATIONS_PERSISTENCE_ENDPOINT": "catalog-mysql:3306",
            },
            clear=True,
        ):
            cfg = get_persistence_config()
            self.assertEqual(cfg["provider"], "mysql")
            self.assertEqual(cfg["endpoint"], "catalog-mysql:3306")


if __name__ == "__main__":
    unittest.main()
