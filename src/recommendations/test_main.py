import os
import unittest
from unittest.mock import patch
from urllib.parse import quote_plus

from fastapi.testclient import TestClient
from sqlalchemy import create_engine, text
from sqlalchemy.pool import StaticPool

from app.repository import (
    InMemoryRecommendationRepository,
    MySQLRecommendationRepository,
    RecommendationsRepository,
)
from main import create_app, get_persistence_config


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

    def test_topology_endpoint_in_memory(self) -> None:
        resp = self.client.get("/topology")
        self.assertEqual(resp.status_code, 200)
        self.assertEqual(
            resp.json(),
            {"persistenceProvider": "in-memory", "databaseEndpoint": "N/A"},
        )

    def test_topology_endpoint_mysql(self) -> None:
        engine = create_engine(
            "sqlite:///:memory:",
            connect_args={"check_same_thread": False},
            poolclass=StaticPool,
        )
        mysql_repo = RecommendationsRepository(
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
        client = TestClient(create_app(mysql_repo))
        resp = client.get("/topology")
        self.assertEqual(resp.status_code, 200)
        self.assertEqual(
            resp.json(),
            {"persistenceProvider": "mysql", "databaseEndpoint": "mysql:3306"},
        )

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

    def test_recommendations_internal_whitespace_returns_400(self) -> None:
        resp = self.client.get("/recommendations/prod%20123")
        self.assertEqual(resp.status_code, 400)
        self.assertEqual(resp.json(), {"code": 400, "message": "Invalid product ID"})

    def test_recommendations_leading_trailing_whitespace_stripped_returns_200(self) -> None:
        resp = self.client.get("/recommendations/%20cc789f85-1476-452a-8100-9e74502198e0%20")
        self.assertEqual(resp.status_code, 200)
        data = resp.json()
        self.assertIsInstance(data, list)
        self.assertGreater(len(data), 0)
        returned_ids = [item["productId"] for item in data]
        self.assertNotIn("cc789f85-1476-452a-8100-9e74502198e0", returned_ids)

    def test_recommendations_repository_error_returns_500(self) -> None:
        with patch.object(
            self.in_memory_repo,
            "get_recommendations",
            side_effect=RuntimeError("Database failure"),
        ):
            resp = self.client.get("/recommendations/cc789f85-1476-452a-8100-9e74502198e0")
            self.assertEqual(resp.status_code, 500)
            self.assertEqual(resp.json(), {"code": 500, "message": "Internal server error"})

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


class TestInMemoryRecommendationRepository(unittest.TestCase):
    def test_custom_products_shared_tags_max_10_sorted(self) -> None:
        custom_products = [
            {"id": f"p{i:02d}", "name": f"Product {i}", "tags": ["shared_tag"]}
            for i in range(15)
        ]
        repo = InMemoryRecommendationRepository(
            {"provider": "in-memory"},
            products=custom_products,
        )
        recs = repo.get_recommendations("p05")
        self.assertIsNotNone(recs)
        rec_ids = [r.productId for r in recs]
        self.assertNotIn("p05", rec_ids)
        self.assertEqual(len(rec_ids), 10)
        expected_ids = sorted([f"p{i:02d}" for i in range(15) if i != 5])[:10]
        self.assertEqual(rec_ids, expected_ids)

    def test_custom_products_fallback_max_4_sorted(self) -> None:
        custom_products = [
            {"id": "target", "name": "Target Product", "tags": ["solo_tag"]},
            {"id": "prod_d", "name": "Product D", "tags": ["group_tag"]},
            {"id": "prod_a", "name": "Product A", "tags": ["group_tag"]},
            {"id": "prod_c", "name": "Product C", "tags": ["group_tag"]},
            {"id": "prod_b", "name": "Product B", "tags": ["group_tag"]},
            {"id": "prod_e", "name": "Product E", "tags": ["group_tag"]},
        ]
        repo = InMemoryRecommendationRepository(
            {"provider": "in-memory"},
            products=custom_products,
        )
        recs = repo.get_recommendations("target")
        self.assertIsNotNone(recs)
        rec_ids = [r.productId for r in recs]
        self.assertNotIn("target", rec_ids)
        self.assertEqual(len(rec_ids), 4)
        self.assertEqual(rec_ids, ["prod_a", "prod_b", "prod_c", "prod_d"])

    def test_custom_products_product_not_found(self) -> None:
        repo = InMemoryRecommendationRepository(
            {"provider": "in-memory"},
            products=[{"id": "p1", "name": "P1", "tags": ["tag1"]}],
        )
        recs = repo.get_recommendations("nonexistent")
        self.assertIsNone(recs)


class TestMySQLRecommendationRepository(unittest.TestCase):
    @patch("app.repository.create_engine")
    def test_engine_creation_when_engine_none_with_special_characters(
        self, mock_create_engine
    ) -> None:
        raw_user = "user@domain#1!"
        raw_pass = "p@ssword:/test?&"
        config = {
            "provider": "mysql",
            "endpoint": "mysql.internal:3306",
            "db_name": "catalogdb",
            "db_user": raw_user,
            "db_password": raw_pass,
            "connect_timeout": 7,
        }
        MySQLRecommendationRepository(config, engine=None)

        mock_create_engine.assert_called_once()
        args, kwargs = mock_create_engine.call_args

        expected_url = (
            f"mysql+pymysql://{quote_plus(raw_user)}:{quote_plus(raw_pass)}"
            f"@mysql.internal:3306/catalogdb?charset=utf8mb4"
        )
        self.assertEqual(args[0], expected_url)
        self.assertEqual(kwargs.get("pool_size"), 3)
        self.assertEqual(kwargs.get("max_overflow"), 2)
        self.assertTrue(kwargs.get("pool_pre_ping"))
        self.assertEqual(kwargs.get("pool_recycle"), 1800)
        self.assertEqual(kwargs.get("connect_args"), {"connect_timeout": 7})


class TestPersistenceConfig(unittest.TestCase):
    def test_default_in_memory(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            cfg = get_persistence_config()
            self.assertEqual(cfg["provider"], "in-memory")
            self.assertEqual(cfg["endpoint"], "")
            self.assertEqual(cfg["db_name"], "catalogdb")
            self.assertEqual(cfg["db_user"], "catalog_user")
            self.assertEqual(cfg["db_password"], "")
            self.assertEqual(cfg["connect_timeout"], 5)

    def test_db_host_without_port(self) -> None:
        with patch.dict(os.environ, {"DB_HOST": "rds-mysql.internal"}, clear=True):
            cfg = get_persistence_config()
            self.assertEqual(cfg["provider"], "mysql")
            self.assertEqual(cfg["endpoint"], "rds-mysql.internal:3306")

    def test_db_host_already_containing_port(self) -> None:
        with patch.dict(os.environ, {"DB_HOST": "rds.internal:3307"}, clear=True):
            cfg = get_persistence_config()
            self.assertEqual(cfg["provider"], "mysql")
            self.assertEqual(cfg["endpoint"], "rds.internal:3307")

    def test_db_host_with_db_port(self) -> None:
        with patch.dict(
            os.environ,
            {"DB_HOST": "rds.internal", "DB_PORT": "3308"},
            clear=True,
        ):
            cfg = get_persistence_config()
            self.assertEqual(cfg["provider"], "mysql")
            self.assertEqual(cfg["endpoint"], "rds.internal:3308")

    def test_recommendations_persistence_env_vars(self) -> None:
        with patch.dict(
            os.environ,
            {
                "RETAIL_RECOMMENDATIONS_PERSISTENCE_PROVIDER": "mysql",
                "RETAIL_RECOMMENDATIONS_PERSISTENCE_ENDPOINT": "catalog-mysql:3306",
                "RETAIL_RECOMMENDATIONS_PERSISTENCE_DB_NAME": "custom_recs_db",
                "RETAIL_RECOMMENDATIONS_PERSISTENCE_USER": "custom_user",
                "RETAIL_RECOMMENDATIONS_PERSISTENCE_PASSWORD": "custom_pass",
                "RETAIL_RECOMMENDATIONS_PERSISTENCE_CONNECT_TIMEOUT": "10",
            },
            clear=True,
        ):
            cfg = get_persistence_config()
            self.assertEqual(cfg["provider"], "mysql")
            self.assertEqual(cfg["endpoint"], "catalog-mysql:3306")
            self.assertEqual(cfg["db_name"], "custom_recs_db")
            self.assertEqual(cfg["db_user"], "custom_user")
            self.assertEqual(cfg["db_password"], "custom_pass")
            self.assertEqual(cfg["connect_timeout"], 10)

    def test_catalog_fallback_env_vars(self) -> None:
        with patch.dict(
            os.environ,
            {
                "RETAIL_CATALOG_PERSISTENCE_ENDPOINT": "catalog-mysql:3306",
                "RETAIL_CATALOG_PERSISTENCE_DB_NAME": "catalog_fallback_db",
                "RETAIL_CATALOG_PERSISTENCE_USER": "catalog_fallback_user",
                "RETAIL_CATALOG_PERSISTENCE_PASSWORD": "catalog_fallback_pass",
            },
            clear=True,
        ):
            cfg = get_persistence_config()
            self.assertEqual(cfg["provider"], "mysql")
            self.assertEqual(cfg["endpoint"], "catalog-mysql:3306")
            self.assertEqual(cfg["db_name"], "catalog_fallback_db")
            self.assertEqual(cfg["db_user"], "catalog_fallback_user")
            self.assertEqual(cfg["db_password"], "catalog_fallback_pass")

    def test_recommendations_takes_precedence_over_catalog_fallbacks(self) -> None:
        with patch.dict(
            os.environ,
            {
                "RETAIL_RECOMMENDATIONS_PERSISTENCE_ENDPOINT": "primary-mysql:3306",
                "RETAIL_RECOMMENDATIONS_PERSISTENCE_DB_NAME": "primary_db",
                "RETAIL_RECOMMENDATIONS_PERSISTENCE_USER": "primary_user",
                "RETAIL_RECOMMENDATIONS_PERSISTENCE_PASSWORD": "primary_pass",
                "RETAIL_CATALOG_PERSISTENCE_ENDPOINT": "fallback-mysql:3306",
                "RETAIL_CATALOG_PERSISTENCE_DB_NAME": "fallback_db",
                "RETAIL_CATALOG_PERSISTENCE_USER": "fallback_user",
                "RETAIL_CATALOG_PERSISTENCE_PASSWORD": "fallback_pass",
            },
            clear=True,
        ):
            cfg = get_persistence_config()
            self.assertEqual(cfg["endpoint"], "primary-mysql:3306")
            self.assertEqual(cfg["db_name"], "primary_db")
            self.assertEqual(cfg["db_user"], "primary_user")
            self.assertEqual(cfg["db_password"], "primary_pass")

    def test_invalid_connect_timeout_falls_back_to_five(self) -> None:
        with patch.dict(
            os.environ,
            {"RETAIL_RECOMMENDATIONS_PERSISTENCE_CONNECT_TIMEOUT": "not-a-number"},
            clear=True,
        ):
            cfg = get_persistence_config()
            self.assertEqual(cfg["connect_timeout"], 5)


if __name__ == "__main__":
    unittest.main()
