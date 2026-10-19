# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

import os
from dataclasses import dataclass
from typing import Optional


@dataclass(frozen=True)
class Settings:
    port: int = int(os.getenv("PORT", "8080"))
    app_mode: str = os.getenv("APP_MODE", "api").strip().lower()

    # Worker & SQS settings
    worker_concurrency: int = int(os.getenv("WORKER_CONCURRENCY", "1"))
    sqs_max_messages: int = int(os.getenv("SQS_MAX_MESSAGES", "1"))
    sqs_wait_time_seconds: int = int(os.getenv("SQS_WAIT_TIME_SECONDS", "20"))
    worker_db_mode: str = os.getenv("WORKER_DB_MODE", "light").strip().lower()
    sqs_queue_url: str = os.getenv("SQS_QUEUE_URL", "").strip()
    sqs_queue_name: str = os.getenv("SQS_QUEUE_NAME", "checkout-queue").strip()
    aws_endpoint_url: Optional[str] = os.getenv("AWS_ENDPOINT_URL", "").strip() or None
    aws_default_region: str = os.getenv("AWS_DEFAULT_REGION", "us-east-1").strip()

    # Domain settings
    shipping_name_prefix: str = os.getenv("RETAIL_CHECKOUT_SHIPPING_NAME_PREFIX", "")

    # Persistence settings
    persistence_provider: str = os.getenv("RETAIL_CHECKOUT_PERSISTENCE_PROVIDER", "").strip().lower()
    persistence_endpoint: str = os.getenv("RETAIL_CHECKOUT_PERSISTENCE_ENDPOINT", "").strip()
    db_host: str = os.getenv("DB_HOST", "").strip()
    db_port: str = os.getenv("DB_PORT", "5432").strip()
    db_name: str = os.getenv("RETAIL_CHECKOUT_PERSISTENCE_DB_NAME") or os.getenv("DB_NAME", "retaildb")
    db_user: str = os.getenv("RETAIL_CHECKOUT_PERSISTENCE_USER") or os.getenv("DB_USER", "postgres")
    db_password: str = os.getenv("RETAIL_CHECKOUT_PERSISTENCE_PASSWORD") or os.getenv("DB_PASSWORD", "postgres")
    database_url: str = os.getenv("DATABASE_URL", "").strip()


def get_settings() -> Settings:
    return Settings(
        port=int(os.getenv("PORT", "8080")),
        app_mode=os.getenv("APP_MODE", "api").strip().lower(),
        worker_concurrency=int(os.getenv("WORKER_CONCURRENCY", "1")),
        sqs_max_messages=int(os.getenv("SQS_MAX_MESSAGES", "1")),
        sqs_wait_time_seconds=int(os.getenv("SQS_WAIT_TIME_SECONDS", "20")),
        worker_db_mode=os.getenv("WORKER_DB_MODE", "light").strip().lower(),
        sqs_queue_url=os.getenv("SQS_QUEUE_URL", "").strip(),
        sqs_queue_name=os.getenv("SQS_QUEUE_NAME", "checkout-queue").strip(),
        aws_endpoint_url=os.getenv("AWS_ENDPOINT_URL", "").strip() or None,
        aws_default_region=os.getenv("AWS_DEFAULT_REGION", "us-east-1").strip(),
        shipping_name_prefix=os.getenv("RETAIL_CHECKOUT_SHIPPING_NAME_PREFIX", ""),
        persistence_provider=os.getenv("RETAIL_CHECKOUT_PERSISTENCE_PROVIDER", "").strip().lower(),
        persistence_endpoint=os.getenv("RETAIL_CHECKOUT_PERSISTENCE_ENDPOINT", "").strip(),
        db_host=os.getenv("DB_HOST", "").strip(),
        db_port=os.getenv("DB_PORT", "5432").strip(),
        db_name=os.getenv("RETAIL_CHECKOUT_PERSISTENCE_DB_NAME") or os.getenv("DB_NAME", "retaildb"),
        db_user=os.getenv("RETAIL_CHECKOUT_PERSISTENCE_USER") or os.getenv("DB_USER", "postgres"),
        db_password=os.getenv("RETAIL_CHECKOUT_PERSISTENCE_PASSWORD") or os.getenv("DB_PASSWORD", "postgres"),
        database_url=os.getenv("DATABASE_URL", "").strip(),
    )
