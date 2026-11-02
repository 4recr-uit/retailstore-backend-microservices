# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

import os
from dataclasses import dataclass
from typing import Any, Optional


def env_int(name: str, default: int, minimum: int = 0) -> int:
    try:
        value = int(os.getenv(name, str(default)))
        return value if value >= minimum else default
    except ValueError:
        return default


@dataclass
class Settings:
    port: Optional[int] = None
    app_mode: Optional[str] = None

    # Worker & SQS settings
    worker_concurrency: Any = None
    sqs_max_messages: Any = None
    sqs_wait_time_seconds: Optional[int] = None
    worker_db_mode: Optional[str] = None
    sqs_queue_url: Optional[str] = None
    sqs_queue_name: Optional[str] = None
    aws_endpoint_url: Optional[str] = None
    aws_default_region: Optional[str] = None

    # Domain settings
    shipping_name_prefix: Optional[str] = None

    # Persistence settings
    persistence_provider: Optional[str] = None
    persistence_endpoint: Optional[str] = None
    db_host: Optional[str] = None
    db_port: Optional[str] = None
    db_name: Optional[str] = None
    db_user: Optional[str] = None
    db_password: Optional[str] = None
    database_url: Optional[str] = None
    db_sslmode: Optional[str] = None
    db_sslrootcert: Optional[str] = None
    db_connect_timeout: Optional[int] = None
    db_pool_size: Optional[int] = None
    db_max_overflow: Optional[int] = None
    db_pool_timeout: Optional[int] = None
    sqs_max_pool_connections: Optional[int] = None
    worker_shutdown_timeout: Optional[int] = None

    def __post_init__(self):
        if self.port is None:
            raw = os.getenv("PORT", "8080")
            try:
                object.__setattr__(self, "port", int(raw))
            except (ValueError, TypeError):
                object.__setattr__(self, "port", 8080)

        if self.app_mode is None:
            object.__setattr__(self, "app_mode", os.getenv("APP_MODE", "api").strip().lower())
        else:
            object.__setattr__(self, "app_mode", str(self.app_mode).strip().lower())

        # worker_concurrency: at least 1, default 1, handles invalid string
        raw_conc = self.worker_concurrency if self.worker_concurrency is not None else os.getenv("WORKER_CONCURRENCY", "1")
        try:
            val_conc = int(raw_conc)
        except (ValueError, TypeError):
            val_conc = 1
        object.__setattr__(self, "worker_concurrency", max(1, val_conc))

        # sqs_max_messages: clamped between 1 and 10, default 1, handles invalid string
        raw_max = self.sqs_max_messages if self.sqs_max_messages is not None else os.getenv("SQS_MAX_MESSAGES", "1")
        try:
            val_max = int(raw_max)
        except (ValueError, TypeError):
            val_max = 1
        object.__setattr__(self, "sqs_max_messages", max(1, min(10, val_max)))

        # sqs_wait_time_seconds
        if self.sqs_wait_time_seconds is None:
            raw = os.getenv("SQS_WAIT_TIME_SECONDS", "20")
            try:
                object.__setattr__(self, "sqs_wait_time_seconds", int(raw))
            except (ValueError, TypeError):
                object.__setattr__(self, "sqs_wait_time_seconds", 20)

        # worker_db_mode
        if self.worker_db_mode is None:
            object.__setattr__(self, "worker_db_mode", os.getenv("WORKER_DB_MODE", "light").strip().lower())
        else:
            object.__setattr__(self, "worker_db_mode", str(self.worker_db_mode).strip().lower())

        # sqs_queue_url
        if self.sqs_queue_url is None:
            object.__setattr__(self, "sqs_queue_url", os.getenv("SQS_QUEUE_URL", "").strip())
        else:
            object.__setattr__(self, "sqs_queue_url", str(self.sqs_queue_url).strip())

        # sqs_queue_name
        if self.sqs_queue_name is None:
            object.__setattr__(self, "sqs_queue_name", os.getenv("SQS_QUEUE_NAME", "checkout-queue").strip())
        else:
            object.__setattr__(self, "sqs_queue_name", str(self.sqs_queue_name).strip())

        # aws_endpoint_url
        if self.aws_endpoint_url is None:
            endpoint = os.getenv("AWS_ENDPOINT_URL", "").strip() or None
            object.__setattr__(self, "aws_endpoint_url", endpoint)

        # aws_default_region
        if self.aws_default_region is None:
            object.__setattr__(self, "aws_default_region", (os.getenv("AWS_REGION") or os.getenv("AWS_DEFAULT_REGION", "")).strip())

        # shipping_name_prefix
        if self.shipping_name_prefix is None:
            object.__setattr__(self, "shipping_name_prefix", os.getenv("RETAIL_CHECKOUT_SHIPPING_NAME_PREFIX", ""))

        # db fields
        if self.persistence_endpoint is None:
            object.__setattr__(self, "persistence_endpoint", os.getenv("RETAIL_CHECKOUT_PERSISTENCE_ENDPOINT", "").strip())
        if self.db_host is None:
            object.__setattr__(self, "db_host", os.getenv("DB_HOST", "").strip())
        if self.db_port is None:
            object.__setattr__(self, "db_port", os.getenv("DB_PORT", "5432").strip())
        if self.db_name is None:
            object.__setattr__(self, "db_name", os.getenv("RETAIL_CHECKOUT_PERSISTENCE_DB_NAME") or os.getenv("DB_NAME", "retaildb"))
        if self.db_user is None:
            object.__setattr__(self, "db_user", os.getenv("RETAIL_CHECKOUT_PERSISTENCE_USER") or os.getenv("DB_USER", ""))
        if self.db_password is None:
            object.__setattr__(self, "db_password", os.getenv("RETAIL_CHECKOUT_PERSISTENCE_PASSWORD") or os.getenv("DB_PASSWORD", ""))
        if self.database_url is None:
            object.__setattr__(self, "database_url", os.getenv("DATABASE_URL", "").strip())

        if self.db_sslmode is None:
            self.db_sslmode = os.getenv("DB_SSLMODE", "disable")
        if self.db_sslrootcert is None:
            self.db_sslrootcert = os.getenv("DB_SSLROOTCERT", "")
        for field, variable, default, minimum in (
            ("db_connect_timeout", "DB_CONNECT_TIMEOUT_SECONDS", 10, 1),
            ("db_pool_size", "DB_POOL_SIZE", 2, 1),
            ("db_max_overflow", "DB_MAX_OVERFLOW", 1, 0),
            ("db_pool_timeout", "DB_POOL_TIMEOUT_SECONDS", 30, 1),
            ("sqs_max_pool_connections", "SQS_MAX_POOL_CONNECTIONS", 50, 1),
            ("worker_shutdown_timeout", "WORKER_SHUTDOWN_TIMEOUT_SECONDS", 5, 1),
        ):
            if getattr(self, field) is None:
                setattr(self, field, env_int(variable, default, minimum))

        # persistence_provider
        raw_prov = self.persistence_provider if self.persistence_provider is not None else os.getenv("RETAIL_CHECKOUT_PERSISTENCE_PROVIDER", "")
        prov = str(raw_prov).strip().lower()
        if prov in ("postgres", "in-memory"):
            resolved_prov = prov
        elif self.database_url or self.db_host or self.persistence_endpoint:
            resolved_prov = "postgres"
        else:
            resolved_prov = "in-memory"
        object.__setattr__(self, "persistence_provider", resolved_prov)

    @property
    def queue_name(self) -> str:
        if self.sqs_queue_url:
            return self.sqs_queue_url.rstrip("/").split("/")[-1]
        return self.sqs_queue_name


def get_settings() -> Settings:
    return Settings()
