# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

import logging
import ssl
from typing import Any, Protocol
from urllib.parse import quote

from sqlalchemy import create_engine, text
from sqlalchemy.engine import Engine

from app.models import Recommendation

logger = logging.getLogger("recommendations.repository")

SEED_PRODUCTS: list[dict[str, Any]] = [
    {
        "id": "cc789f85-1476-452a-8100-9e74502198e0",
        "name": "Temporal Tickstopper",
        "tags": ["accessories"],
    },
    {
        "id": "87e89b11-d319-446d-b9be-50adcca5224a",
        "name": "Up & Away Parasol",
        "tags": ["clothing"],
    },
    {
        "id": "4f18544b-70a5-4352-8e19-0d070f46745d",
        "name": "Levitator Oxfords",
        "tags": ["clothing"],
    },
    {
        "id": "79bce3f3-935f-4912-8c62-0d2f3e059405",
        "name": "Facechanger Formal Wear",
        "tags": ["clothing"],
    },
    {
        "id": "d27cf49f-b689-4a75-a249-d373e0330bb5",
        "name": "The Quiet Quill",
        "tags": ["accessories"],
    },
    {
        "id": "1ca35e86-4b4c-4124-b6b5-076ba4134d0d",
        "name": "The Forgetter MK-II",
        "tags": ["accessories"],
    },
    {
        "id": "631a3db5-ac07-492c-a994-8cd56923c112",
        "name": "The Morning Teleporter",
        "tags": ["accessories"],
    },
    {
        "id": "8757729a-c518-4356-8694-9e795a9b3237",
        "name": "Forget-Me-Pop",
        "tags": ["food"],
    },
    {
        "id": "d4edfedb-dbe9-4dd9-aae8-009489394955",
        "name": "Audio-Illusion Spinner",
        "tags": ["accessories"],
    },
    {
        "id": "a1258cd2-176c-4507-ade6-746dab5ad625",
        "name": "Aqua Ace GT",
        "tags": ["vehicles"],
    },
    {
        "id": "d3104128-1d14-4465-99d3-8ab9267c687b",
        "name": "SkyCycle X-1000",
        "tags": ["vehicles"],
    },
    {
        "id": "d77f9ae6-e9a8-4a3e-86bd-b72af75cbc49",
        "name": "Phantom Pursuit",
        "tags": ["vehicles"],
    },
]


class BaseRecommendationRepository(Protocol):
    config: dict[str, Any]

    def get_recommendations(self, product_id: str) -> list[Recommendation] | None:
        ...


class InMemoryRecommendationRepository:
    def check_ready(self) -> None:
        pass

    def __init__(self, config: dict[str, Any], products: list[dict[str, Any]] | None = None) -> None:
        self.config = config
        self.provider = "in-memory"
        source = products if products is not None else SEED_PRODUCTS
        self._products_by_id: dict[str, set[str]] = {p["id"]: set(p["tags"]) for p in source}
        logger.info("Initialized in-memory recommendations repository with %d products", len(self._products_by_id))

    def get_recommendations(self, product_id: str) -> list[Recommendation] | None:
        if product_id not in self._products_by_id:
            return None

        target_tags = self._products_by_id[product_id]
        shared_ids = sorted(
            other_id
            for other_id, other_tags in self._products_by_id.items()
            if other_id != product_id and bool(target_tags & other_tags)
        )[:10]

        if not shared_ids:
            shared_ids = sorted(
                other_id for other_id in self._products_by_id if other_id != product_id
            )[:4]

        return [Recommendation(productId=pid) for pid in shared_ids]


class MySQLRecommendationRepository:
    def check_ready(self) -> None:
        # Verify connectivity, schema and runtime SELECT permissions without fetching data.
        with self.engine.connect() as conn:
            conn.execute(text("SELECT id FROM products LIMIT 0"))
            conn.execute(text("SELECT product_id, tag_name FROM product_tags LIMIT 0"))

    def __init__(self, config: dict[str, Any], engine: Engine | None = None) -> None:
        self.config = config
        self.provider = "mysql"
        if engine is not None:
            self.engine = engine
        else:
            user = quote(config["db_user"], safe="")
            password = quote(config["db_password"], safe="")
            endpoint = config["endpoint"]
            db_name = config["db_name"]
            url = f"mysql+pymysql://{user}:{password}@{endpoint}/{db_name}?charset=utf8mb4"
            connect_args = {"connect_timeout": config.get("connect_timeout", 5)}
            if config.get("tls") or config.get("ssl_ca"):
                connect_args["ssl"] = ssl.create_default_context(cafile=config.get("ssl_ca") or None)
            self.engine = create_engine(
                url,
                pool_size=config.get("pool_size", 3),
                max_overflow=config.get("max_overflow", 2),
                pool_pre_ping=True,
                pool_recycle=config.get("pool_recycle", 1800),
                pool_timeout=config.get("pool_timeout", 30),
                connect_args=connect_args,
            )
            logger.info("Initialized MySQL repository targeting %s/%s", endpoint, db_name)

    def get_recommendations(self, product_id: str) -> list[Recommendation] | None:
        exists_stmt = text("SELECT id FROM products WHERE id = :id")
        shared_tags_stmt = text(
            """
            SELECT DISTINCT pt2.product_id
            FROM product_tags pt1
            JOIN product_tags pt2 ON pt1.tag_name = pt2.tag_name
            WHERE pt1.product_id = :id AND pt2.product_id != :id
            ORDER BY pt2.product_id
            LIMIT 10
            """
        )
        fallback_stmt = text(
            """
            SELECT id
            FROM products
            WHERE id != :id
            ORDER BY id
            LIMIT 4
            """
        )

        with self.engine.connect() as conn:
            row = conn.execute(exists_stmt, {"id": product_id}).fetchone()
            if row is None:
                return None

            rows = conn.execute(shared_tags_stmt, {"id": product_id}).fetchall()
            recommended_ids = [r[0] for r in rows]

            if not recommended_ids:
                fallback_rows = conn.execute(fallback_stmt, {"id": product_id}).fetchall()
                recommended_ids = [r[0] for r in fallback_rows]

        return [Recommendation(productId=pid) for pid in recommended_ids]


class RecommendationsRepository:
    """
    Unified recommendation repository wrapper providing compatibility
    for both MySQL and in-memory storage configurations.
    """

    def __init__(self, config: dict[str, Any], engine: Engine | None = None) -> None:
        self.config = config
        self.provider = config.get("provider", "in-memory")
        if self.provider == "mysql" or engine is not None:
            self._impl: BaseRecommendationRepository = MySQLRecommendationRepository(config, engine=engine)
            self.engine = getattr(self._impl, "engine", None)
        else:
            self._impl = InMemoryRecommendationRepository(config)
            self.engine = None

    def get_recommendations(self, product_id: str) -> list[Recommendation] | None:
        return self._impl.get_recommendations(product_id)

    def check_ready(self) -> None:
        self._impl.check_ready()
