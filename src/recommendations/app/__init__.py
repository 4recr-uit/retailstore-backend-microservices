# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

import logging
from typing import Any
from fastapi import FastAPI, HTTPException, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from app.config import get_persistence_config
from app.metrics import prometheus_middleware
from app.repository import RecommendationsRepository
from app.routes import router

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s [%(name)s] %(message)s",
)
logger = logging.getLogger("recommendations")


def create_app(repository: Any = None) -> FastAPI:
    app = FastAPI(
        title="Recommendations API",
        description="This API serves product recommendations",
        version="1.0",
    )

    repo = repository or RecommendationsRepository(get_persistence_config())
    app.state.repository = repo

    app.middleware("http")(prometheus_middleware)

    @app.exception_handler(HTTPException)
    async def http_exception_handler(_: Request, exc: HTTPException) -> JSONResponse:
        return JSONResponse(
            status_code=exc.status_code,
            content={"code": exc.status_code, "message": str(exc.detail)},
        )

    @app.exception_handler(RequestValidationError)
    async def validation_exception_handler(_: Request, exc: RequestValidationError) -> JSONResponse:
        return JSONResponse(
            status_code=400,
            content={"code": 400, "message": str(exc)},
        )

    @app.exception_handler(Exception)
    async def unhandled_exception_handler(_: Request, exc: Exception) -> JSONResponse:
        logger.exception("Unhandled error processing request: %s", exc)
        return JSONResponse(
            status_code=500,
            content={"code": 500, "message": "Internal server error"},
        )

    app.include_router(router)

    return app
