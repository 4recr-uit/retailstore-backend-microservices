# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

import logging
from fastapi import APIRouter, HTTPException, Request, Response
from fastapi.responses import PlainTextResponse

from app.metrics import get_metrics_response
from app.models import HTTPError, Recommendation

logger = logging.getLogger("recommendations.routes")

router = APIRouter()


@router.get(
    "/recommendations/{productId}",
    response_model=list[Recommendation],
    responses={
        400: {"model": HTTPError, "description": "Bad Request"},
        404: {"model": HTTPError, "description": "Not Found"},
        500: {"model": HTTPError, "description": "Internal Server Error"},
    },
    tags=["recommendations"],
    summary="Get product recommendations",
    description="Returns a list of recommended products for the given product ID",
)
def get_recommendations(productId: str, request: Request) -> list[Recommendation]:
    cleaned = productId.strip()
    if not cleaned or len(cleaned) > 64 or any(c.isspace() for c in cleaned):
        raise HTTPException(status_code=400, detail="Invalid product ID")

    try:
        recommendations = request.app.state.repository.get_recommendations(cleaned)
    except HTTPException:
        raise
    except Exception as exc:
        logger.exception("Database error fetching recommendations for %s: %s", cleaned, exc)
        raise HTTPException(status_code=500, detail="Internal server error") from exc

    if recommendations is None:
        raise HTTPException(status_code=404, detail="Product not found")

    return recommendations


@router.get("/health", response_class=PlainTextResponse, tags=["system"])
def health() -> str:
    return "OK"


@router.get("/ready", response_class=PlainTextResponse, tags=["system"])
def ready(request: Request) -> PlainTextResponse:
    try:
        request.app.state.repository.check_ready()
    except Exception:
        logger.warning("Recommendations database is not ready")
        return PlainTextResponse("Not ready", status_code=503)
    return PlainTextResponse("OK")


@router.get("/topology", tags=["system"])
def topology(request: Request) -> dict[str, str]:
    cfg = getattr(request.app.state.repository, "config", {})
    provider = cfg.get("provider", "in-memory")
    endpoint = cfg.get("endpoint", "")
    return {
        "persistenceProvider": provider,
        "databaseEndpoint": endpoint if provider != "in-memory" else "N/A",
    }


@router.get("/metrics", tags=["system"])
def metrics() -> Response:
    return get_metrics_response()
