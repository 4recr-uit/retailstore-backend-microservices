# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

import time
from typing import Callable
from fastapi import Request, Response
from prometheus_client import CONTENT_TYPE_LATEST, Counter, Histogram, generate_latest

HTTP_REQUESTS_TOTAL = Counter(
    "recommendations_http_requests_total",
    "Total number of HTTP requests to recommendations service",
    ["method", "endpoint", "status"],
)

HTTP_REQUEST_DURATION_SECONDS = Histogram(
    "recommendations_http_request_duration_seconds",
    "HTTP request latency in seconds for recommendations service",
    ["method", "endpoint"],
)


async def prometheus_middleware(request: Request, call_next: Callable) -> Response:
    start_time = time.perf_counter()
    endpoint = request.url.path
    if endpoint.startswith("/recommendations/"):
        endpoint = "/recommendations/{productId}"
    status_code = 500
    try:
        response = await call_next(request)
        status_code = response.status_code
        return response
    finally:
        if endpoint != "/metrics":
            duration = time.perf_counter() - start_time
            HTTP_REQUESTS_TOTAL.labels(
                method=request.method,
                endpoint=endpoint,
                status=str(status_code),
            ).inc()
            HTTP_REQUEST_DURATION_SECONDS.labels(
                method=request.method,
                endpoint=endpoint,
            ).observe(duration)


def get_metrics_response() -> Response:
    return Response(content=generate_latest(), media_type=CONTENT_TYPE_LATEST)
