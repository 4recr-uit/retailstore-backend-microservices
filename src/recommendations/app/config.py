# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

import os
from typing import Any


def env_int(name: str, default: int, minimum: int = 0) -> int:
    try:
        value = int(os.getenv(name, str(default)))
        return value if value >= minimum else default
    except ValueError:
        return default


def get_persistence_config() -> dict[str, Any]:
    provider_env = (
        os.getenv("RETAIL_RECOMMENDATIONS_PERSISTENCE_PROVIDER")
        or os.getenv("RETAIL_CATALOG_PERSISTENCE_PROVIDER", "")
    ).strip().lower()
    endpoint = (
        os.getenv("RETAIL_RECOMMENDATIONS_PERSISTENCE_ENDPOINT")
        or os.getenv("RETAIL_CATALOG_PERSISTENCE_ENDPOINT", "")
    ).strip()
    db_host = os.getenv("DB_HOST", "").strip()
    db_port = os.getenv("DB_PORT", "3306").strip()

    if not endpoint and db_host:
        endpoint = f"{db_host}:{db_port}" if ":" not in db_host else db_host

    if provider_env in ("mysql", "in-memory"):
        provider = provider_env
    elif db_host or endpoint:
        provider = "mysql"
    else:
        provider = "in-memory"

    if provider == "mysql" and not endpoint:
        raise ValueError("MySQL requires RETAIL_RECOMMENDATIONS_PERSISTENCE_ENDPOINT or DB_HOST")

    db_name = (
        os.getenv("RETAIL_RECOMMENDATIONS_PERSISTENCE_DB_NAME")
        or os.getenv("RETAIL_CATALOG_PERSISTENCE_DB_NAME")
        or os.getenv("DB_NAME", "catalogdb")
    )
    db_user = (
        os.getenv("RETAIL_RECOMMENDATIONS_PERSISTENCE_USER")
        or os.getenv("RETAIL_CATALOG_PERSISTENCE_USER")
        or os.getenv("DB_USER", "catalog_user")
    )
    db_password = (
        os.getenv("RETAIL_RECOMMENDATIONS_PERSISTENCE_PASSWORD")
        or os.getenv("RETAIL_CATALOG_PERSISTENCE_PASSWORD")
        or os.getenv("DB_PASSWORD", "")
    )
    try:
        connect_timeout = int(
            os.getenv("RETAIL_RECOMMENDATIONS_PERSISTENCE_CONNECT_TIMEOUT")
            or os.getenv("RETAIL_CATALOG_PERSISTENCE_CONNECT_TIMEOUT", "5")
        )
    except ValueError:
        connect_timeout = 5

    return {
        "provider": provider,
        "endpoint": endpoint,
        "db_name": db_name,
        "db_user": db_user,
        "db_password": db_password,
        "connect_timeout": connect_timeout,
        "pool_size": env_int("DB_POOL_SIZE", 3, 1),
        "max_overflow": env_int("DB_MAX_OVERFLOW", 2),
        "pool_recycle": env_int("DB_POOL_RECYCLE_SECONDS", 1800, 1),
        "pool_timeout": env_int("DB_POOL_TIMEOUT_SECONDS", 30, 1),
        "tls": os.getenv("RETAIL_RECOMMENDATIONS_PERSISTENCE_TLS", "false").lower() == "true",
        "ssl_ca": os.getenv("DB_SSL_CA", ""),
    }


def get_server_port() -> int:
    return int(os.getenv("PORT", "8080"))
