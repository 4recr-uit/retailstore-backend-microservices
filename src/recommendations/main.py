# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

import os

from app import create_app
from app.config import get_persistence_config
from app.models import HTTPError, Recommendation
from app.repository import (
    InMemoryRecommendationRepository,
    MySQLRecommendationRepository,
    RecommendationsRepository,
)

app = create_app()

__all__ = [
    "app",
    "create_app",
    "get_persistence_config",
    "RecommendationsRepository",
    "InMemoryRecommendationRepository",
    "MySQLRecommendationRepository",
    "Recommendation",
    "HTTPError",
]

if __name__ == "__main__":
    import uvicorn

    port = int(os.getenv("PORT", "8080"))
    uvicorn.run("main:app", host="0.0.0.0", port=port)
