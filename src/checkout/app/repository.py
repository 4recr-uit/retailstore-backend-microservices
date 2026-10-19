# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

import json
import logging
import threading
import time
from typing import Any, Optional
from urllib.parse import quote_plus

from sqlalchemy import create_engine, text
from sqlalchemy.engine import Engine

from app.config import get_settings

logger = logging.getLogger("checkout.repository")


class BaseCheckoutRepository:
    provider: str

    def get_session(self, customer_id: str) -> Optional[dict[str, Any]]:
        raise NotImplementedError

    def save_session(self, customer_id: str, data: dict[str, Any]) -> None:
        raise NotImplementedError

    def delete_session(self, customer_id: str) -> bool:
        raise NotImplementedError

    def record_processed_job(
        self,
        job_id: str,
        order_id: str,
        customer_id: Optional[str],
        status_val: str,
        data: Optional[dict[str, Any]],
    ) -> bool:
        raise NotImplementedError


class InMemoryCheckoutRepository(BaseCheckoutRepository):
    def __init__(self) -> None:
        self.provider = "in-memory"
        self._lock = threading.Lock()
        self._sessions: dict[str, dict[str, Any]] = {}
        self._processed_jobs: dict[str, dict[str, Any]] = {}
        logger.info("Initialized in-memory checkout repository")

    def get_session(self, customer_id: str) -> Optional[dict[str, Any]]:
        with self._lock:
            val = self._sessions.get(customer_id)
            return json.loads(json.dumps(val)) if val is not None else None

    def save_session(self, customer_id: str, data: dict[str, Any]) -> None:
        with self._lock:
            self._sessions[customer_id] = json.loads(json.dumps(data))

    def delete_session(self, customer_id: str) -> bool:
        with self._lock:
            return self._sessions.pop(customer_id, None) is not None

    def record_processed_job(
        self,
        job_id: str,
        order_id: str,
        customer_id: Optional[str],
        status_val: str,
        data: Optional[dict[str, Any]],
    ) -> bool:
        with self._lock:
            if job_id in self._processed_jobs:
                return False
            self._processed_jobs[job_id] = {
                "job_id": job_id,
                "order_id": order_id,
                "customer_id": customer_id,
                "status": status_val,
                "data": json.loads(json.dumps(data)) if data is not None else None,
                "created_at": time.time(),
            }
            return True


class PostgresCheckoutRepository(BaseCheckoutRepository):
    def __init__(self, engine: Engine) -> None:
        self.provider = "postgres"
        self.engine = engine
        self._init_db()
        logger.info("Initialized PostgreSQL checkout repository with schema auto-migration")

    def _init_db(self) -> None:
        migration_sql = """
        CREATE SCHEMA IF NOT EXISTS checkout;
        CREATE TABLE IF NOT EXISTS checkout.checkout_sessions (
            customer_id VARCHAR(255) PRIMARY KEY,
            data JSONB NOT NULL,
            updated_at TIMESTAMPTZ DEFAULT NOW()
        );
        CREATE TABLE IF NOT EXISTS checkout.processed_jobs (
            job_id VARCHAR(64) PRIMARY KEY,
            order_id VARCHAR(255) NOT NULL,
            customer_id VARCHAR(255),
            status VARCHAR(64) DEFAULT 'COMPLETED',
            data JSONB,
            created_at TIMESTAMPTZ DEFAULT NOW()
        );
        """
        with self.engine.begin() as conn:
            conn.execute(text(migration_sql))

    def get_session(self, customer_id: str) -> Optional[dict[str, Any]]:
        stmt = text("SELECT data FROM checkout.checkout_sessions WHERE customer_id = :cid")
        with self.engine.connect() as conn:
            row = conn.execute(stmt, {"cid": customer_id}).fetchone()
            if not row:
                return None
            val = row[0]
            if isinstance(val, str):
                return json.loads(val)
            return val

    def save_session(self, customer_id: str, data: dict[str, Any]) -> None:
        stmt = text("""
        INSERT INTO checkout.checkout_sessions (customer_id, data, updated_at)
        VALUES (:cid, :data, NOW())
        ON CONFLICT (customer_id)
        DO UPDATE SET data = EXCLUDED.data, updated_at = NOW();
        """)
        with self.engine.begin() as conn:
            conn.execute(stmt, {"cid": customer_id, "data": json.dumps(data)})

    def delete_session(self, customer_id: str) -> bool:
        stmt = text("DELETE FROM checkout.checkout_sessions WHERE customer_id = :cid")
        with self.engine.begin() as conn:
            res = conn.execute(stmt, {"cid": customer_id})
            return bool(res.rowcount and res.rowcount > 0)

    def record_processed_job(
        self,
        job_id: str,
        order_id: str,
        customer_id: Optional[str],
        status_val: str,
        data: Optional[dict[str, Any]],
    ) -> bool:
        stmt = text("""
        INSERT INTO checkout.processed_jobs (job_id, order_id, customer_id, status, data, created_at)
        VALUES (:jid, :oid, :cid, :status, :data, NOW())
        ON CONFLICT (job_id) DO NOTHING;
        """)
        with self.engine.begin() as conn:
            res = conn.execute(
                stmt,
                {
                    "jid": job_id,
                    "oid": order_id,
                    "cid": customer_id,
                    "status": status_val,
                    "data": json.dumps(data) if data is not None else None,
                },
            )
            return bool(res.rowcount and res.rowcount > 0)


# Aliases for convenience
InMemoryRepository = InMemoryCheckoutRepository
PostgresRepository = PostgresCheckoutRepository


def create_repository() -> BaseCheckoutRepository:
    settings = get_settings()
    endpoint = settings.persistence_endpoint
    db_host = settings.db_host
    db_port = settings.db_port
    database_url = settings.database_url

    if not endpoint and db_host:
        endpoint = f"{db_host}:{db_port}" if ":" not in db_host else db_host

    if settings.persistence_provider == "postgres":
        provider = "postgres"
    elif settings.persistence_provider == "in-memory":
        provider = "in-memory"
    elif database_url or db_host or endpoint:
        provider = "postgres"
    else:
        provider = "in-memory"

    if provider == "postgres":
        if database_url:
            url = database_url
            if url.startswith("postgres://"):
                url = "postgresql://" + url[len("postgres://"):]
        else:
            host_port = endpoint or "localhost:5432"
            db_name = settings.db_name
            db_user = settings.db_user
            db_password = settings.db_password
            url = f"postgresql://{quote_plus(db_user)}:{quote_plus(db_password)}@{host_port}/{db_name}"

        try:
            engine = create_engine(
                url,
                pool_size=2,
                max_overflow=1,
                pool_pre_ping=True,
            )
            return PostgresCheckoutRepository(engine)
        except Exception as exc:
            logger.error("Failed to connect to PostgreSQL at %s (%s). Falling back to in-memory.", url, exc)
            return InMemoryCheckoutRepository()

    return InMemoryCheckoutRepository()
