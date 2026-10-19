# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

import asyncio
import threading
from typing import Any

from fastapi import APIRouter, HTTPException, Request, Response


class ChaosController:
    def __init__(self) -> None:
        self._lock = threading.Lock()
        self.latency_ms: int = 0
        self.is_latency_on: bool = False
        self.error_status_code: int = 0
        self.is_error_status_on: bool = False
        self.is_healthy: bool = True

    def set_latency(self, ms: int) -> None:
        with self._lock:
            self.latency_ms = ms
            self.is_latency_on = True

    def disable_latency(self) -> None:
        with self._lock:
            self.latency_ms = 0
            self.is_latency_on = False

    def set_error_status(self, code: int) -> None:
        with self._lock:
            self.error_status_code = code
            self.is_error_status_on = True

    def disable_error_status(self) -> None:
        with self._lock:
            self.error_status_code = 0
            self.is_error_status_on = False

    def set_unhealthy(self) -> None:
        with self._lock:
            self.is_healthy = False

    def set_healthy(self) -> None:
        with self._lock:
            self.is_healthy = True

    def get_status(self) -> dict[str, Any]:
        with self._lock:
            return {
                "latency": {
                    "enabled": self.is_latency_on,
                    "value": self.latency_ms,
                },
                "error_status": {
                    "enabled": self.is_error_status_on,
                    "code": self.error_status_code,
                },
            }


chaos_router = APIRouter(prefix="/chaos", tags=["chaos"])


@chaos_router.post("/latency/{ms}")
def set_chaos_latency(ms: int, request: Request):
    if ms < 0:
        raise HTTPException(status_code=400, detail="Invalid latency value. Must be positive.")
    request.app.state.chaos.set_latency(ms)
    return {"message": f"Latency set to {ms}ms"}


@chaos_router.delete("/latency")
def disable_chaos_latency(request: Request):
    request.app.state.chaos.disable_latency()
    return {"message": "Latency disabled"}


@chaos_router.post("/status/{code}")
def set_chaos_status(code: int, request: Request):
    if code < 100 or code > 599:
        raise HTTPException(status_code=400, detail="Invalid HTTP status code (100-599)")
    request.app.state.chaos.set_error_status(code)
    return {"message": f"Error status code set to {code}"}


@chaos_router.delete("/status")
def disable_chaos_status(request: Request):
    request.app.state.chaos.disable_error_status()
    return {"message": "Error status disabled"}


@chaos_router.get("/status")
def get_chaos_status(request: Request):
    return request.app.state.chaos.get_status()


@chaos_router.post("/health")
def set_chaos_health(request: Request):
    request.app.state.chaos.set_unhealthy()
    return {"message": "Health check endpoint disabled"}


@chaos_router.delete("/health")
def disable_chaos_health(request: Request):
    request.app.state.chaos.set_healthy()
    return {"message": "Health endpoint enabled"}


async def apply_chaos_middleware(request: Request, call_next):
    path = request.url.path
    if not path.startswith("/chaos"):
        cc: ChaosController = request.app.state.chaos
        if cc.is_latency_on and cc.latency_ms > 0:
            await asyncio.sleep(cc.latency_ms / 1000.0)
        if cc.is_error_status_on and cc.error_status_code > 0:
            return Response(
                content=f"Chaos error response ({cc.error_status_code})",
                status_code=cc.error_status_code,
                media_type="text/plain",
            )
    return await call_next(request)
