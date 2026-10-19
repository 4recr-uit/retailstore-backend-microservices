# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0
from contextlib import asynccontextmanager
import logging
from typing import Optional
from fastapi import FastAPI, HTTPException
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
import uvicorn
from app.chaos import ChaosController, apply_chaos_middleware, chaos_router
from app.config import Settings, get_settings
from app.metrics import init_metrics
from app.repository import BaseCheckoutRepository, InMemoryCheckoutRepository, PostgresCheckoutRepository, create_repository
from app.routes import api_router, system_router
from app.worker import CheckoutWorker, SQSService, burn_cpu, create_sqs_service, make_id

__all__ = [
    "app",
    "create_app",
    "ChaosController",
    "CheckoutWorker",
    "InMemoryCheckoutRepository",
    "PostgresCheckoutRepository",
    "SQSService",
    "Settings",
    "burn_cpu",
    "create_repository",
    "get_settings",
    "make_id",
]
logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s [%(name)s] %(message)s")


def create_app(
    repository: Optional[BaseCheckoutRepository] = None,
    sqs_service: Optional[SQSService] = None,
    chaos: Optional[ChaosController] = None,
    app_mode: Optional[str] = None,
) -> FastAPI:
    cfg, repo, sqs, chaos_ctrl = get_settings(), repository or create_repository(), sqs_service or create_sqs_service(), chaos or ChaosController()
    mode = (app_mode or cfg.app_mode).strip().lower()
    worker = CheckoutWorker(sqs, repo, cfg.worker_concurrency, cfg.sqs_max_messages, cfg.sqs_wait_time_seconds, cfg.worker_db_mode)

    @asynccontextmanager
    async def lifespan(_: FastAPI):
        init_metrics("checkout", sqs.queue_name)
        if mode in ("worker", "all"):
            worker.start()
        yield
        if mode in ("worker", "all"):
            worker.stop()

    app = FastAPI(title="Checkout service", description="Checkout API & Worker", lifespan=lifespan)
    app.state.repository, app.state.sqs_service, app.state.chaos = repo, sqs, chaos_ctrl
    app.state.worker, app.state.app_mode = worker, mode
    app.middleware("http")(apply_chaos_middleware)
    app.add_exception_handler(HTTPException, lambda _, e: JSONResponse(status_code=e.status_code, content={"code": e.status_code, "message": str(e.detail)}))
    app.add_exception_handler(RequestValidationError, lambda _, e: JSONResponse(status_code=400, content={"code": 400, "message": str(e)}))
    app.include_router(chaos_router)
    app.include_router(system_router)
    if mode != "worker":
        app.include_router(api_router)
    return app


app = create_app()

if __name__ == "__main__":
    uvicorn.run("main:app", host="0.0.0.0", port=get_settings().port, log_level="info")
