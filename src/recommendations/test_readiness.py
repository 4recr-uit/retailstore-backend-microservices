import unittest
from unittest.mock import patch
from fastapi.testclient import TestClient
from sqlalchemy import create_engine, text
from sqlalchemy.pool import StaticPool
from app.repository import RecommendationsRepository
from main import create_app


class ReadinessTests(unittest.TestCase):
    def test_memory_is_ready(self):
        client = TestClient(create_app(RecommendationsRepository({"provider": "in-memory"})))
        self.assertEqual(client.get("/ready").status_code, 200)

    def test_missing_schema_is_not_ready_but_process_is_alive(self):
        engine = create_engine("sqlite://", connect_args={"check_same_thread": False}, poolclass=StaticPool)
        self.addCleanup(engine.dispose)
        repo = RecommendationsRepository({"provider": "mysql"}, engine=engine)
        client = TestClient(create_app(repo))
        self.assertEqual(client.get("/health").status_code, 200)
        self.assertEqual(client.get("/ready").status_code, 503)
        with engine.begin() as conn:
            conn.execute(text("CREATE TABLE products (id TEXT)"))
            conn.execute(text("CREATE TABLE product_tags (product_id TEXT, tag_name TEXT)"))
        self.assertEqual(client.get("/ready").status_code, 200)
        with patch.object(engine, "connect", side_effect=RuntimeError("offline")):
            self.assertEqual(client.get("/ready").status_code, 503)


if __name__ == "__main__": unittest.main()
