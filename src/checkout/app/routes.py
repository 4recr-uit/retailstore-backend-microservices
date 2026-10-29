# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

import os
import time
from typing import Optional
import uuid

from fastapi import APIRouter, HTTPException, Request, Response, status
from fastapi.concurrency import run_in_threadpool
from fastapi.responses import JSONResponse, PlainTextResponse
from prometheus_client import CONTENT_TYPE_LATEST, generate_latest

from app.config import get_settings
from app.models import (
    Checkout,
    CheckoutRequest,
    CheckoutSubmitted,
    Item,
    JobRequest,
    ShippingOption,
    ShippingRates,
)
from app.worker import BatchPublishError, SQSService, make_id

api_router = APIRouter()


@api_router.get(
    "/checkout/{customerId}",
    response_model=Checkout,
    responses={
        200: {"model": Checkout},
        201: {"model": Checkout},
        404: {"description": "Checkout session not found"},
    },
    tags=["checkout"],
)
def get_checkout(customerId: str, request: Request):
    session = request.app.state.repository.get_session(customerId)
    if not session:
        raise HTTPException(status_code=404, detail="Checkout session not found")
    return session


@api_router.post(
    "/checkout/{customerId}/update",
    response_model=Checkout,
    status_code=status.HTTP_201_CREATED,
    tags=["checkout"],
)
def update_checkout(customerId: str, req: CheckoutRequest, request: Request):
    subtotal = 0
    items_out: list[Item] = []
    for it in req.items:
        cost = it.price * it.quantity
        subtotal += cost
        items_out.append(
            Item(
                id=it.id,
                name=it.name,
                quantity=it.quantity,
                price=it.price,
                totalCost=cost,
            )
        )

    tax = 5 if req.shippingAddress else -1
    effective_tax = tax if tax != -1 else 0

    shipping = -1
    shipping_rates: Optional[ShippingRates] = None
    if req.shippingAddress:
        settings = get_settings()
        prefix = settings.shipping_name_prefix
        rates = [
            ShippingOption(
                name=f"{prefix}Priority Mail",
                amount=10,
                token="priority-mail",
                estimatedDays=10,
            ),
            ShippingOption(
                name=f"{prefix}Priority Mail Express",
                amount=25,
                token="priority-mail-express",
                estimatedDays=5,
            ),
        ]
        shipping_rates = ShippingRates(
            shipmentId=make_id(32),
            rates=rates,
        )
        if req.deliveryOptionToken:
            for opt in rates:
                if opt.token == req.deliveryOptionToken:
                    shipping = opt.amount
                    break

    effective_shipping = shipping if shipping != -1 else 0
    total = subtotal + effective_tax + effective_shipping

    checkout = Checkout(
        items=items_out,
        shippingAddress=req.shippingAddress,
        deliveryOptionToken=req.deliveryOptionToken or "",
        shippingRates=shipping_rates,
        paymentId=make_id(16),
        paymentToken=make_id(32),
        subtotal=subtotal,
        shipping=shipping,
        tax=tax,
        total=total,
    )

    request.app.state.repository.save_session(customerId, checkout.model_dump())
    return checkout


@api_router.post(
    "/checkout/{customerId}/submit",
    response_model=CheckoutSubmitted,
    status_code=status.HTTP_201_CREATED,
    tags=["checkout"],
)
async def submit_checkout(customerId: str, request: Request):
    session = request.app.state.repository.get_session(customerId)
    if not session:
        raise HTTPException(status_code=404, detail="Checkout session not found")

    order_id = str(uuid.uuid4())
    now_ms = int(time.time() * 1000)
    settings = get_settings()
    worker_db_mode = settings.worker_db_mode

    msg = {
        "jobId": order_id,
        "orderId": order_id,
        "enqueuedAt": now_ms,
        "workUnits": 10,
        "dbMode": worker_db_mode,
        "customerId": customerId,
        "payload": session,
    }

    sqs_svc: SQSService = request.app.state.sqs_service
    if not await run_in_threadpool(sqs_svc.publish_single, msg):
        raise HTTPException(status_code=503, detail="SQS queue is not configured")

    request.app.state.repository.delete_session(customerId)

    email = ""
    shipping_addr = session.get("shippingAddress")
    if isinstance(shipping_addr, dict):
        email = shipping_addr.get("email", "")

    return CheckoutSubmitted(
        orderId=order_id,
        email=email,
        items=[Item(**it) for it in session.get("items", [])],
        subtotal=session.get("subtotal", 0),
        shipping=session.get("shipping", 0),
        tax=session.get("tax", 0),
        total=session.get("total", 0),
    )


@api_router.post(
    "/jobs",
    status_code=status.HTTP_201_CREATED,
    tags=["benchmark"],
)
async def create_jobs(job_req: JobRequest, request: Request):
    if job_req.workUnits < 1 or job_req.workUnits > 10000:
        raise HTTPException(status_code=400, detail="workUnits must be between 1 and 10000")
    if job_req.batchSize < 1 or job_req.batchSize > 10:
        raise HTTPException(status_code=400, detail="batchSize must be between 1 and 10")
    if job_req.dbMode not in ("none", "light", "full"):
        raise HTTPException(status_code=400, detail="dbMode must be 'none', 'light', or 'full'")

    sqs_svc: SQSService = request.app.state.sqs_service
    now_ms = int(time.time() * 1000)
    job_ids: list[str] = []
    messages = []

    for _ in range(job_req.batchSize):
        jid = str(uuid.uuid4())
        job_ids.append(jid)
        messages.append(
            {
                "jobId": jid,
                "orderId": jid,
                "enqueuedAt": now_ms,
                "workUnits": job_req.workUnits,
                "dbMode": job_req.dbMode,
                "customerId": job_req.customerId or "",
                "payload": job_req.payload or {},
            }
        )

    try:
        if job_req.batchSize == 1:
            published = int(await run_in_threadpool(sqs_svc.publish_single, messages[0]))
        else:
            published = await run_in_threadpool(sqs_svc.publish_batch, messages)
    except BatchPublishError as exc:
        return JSONResponse(status_code=502, content={"status": "partial_failure",
                            "enqueued": len(exc.successful_job_ids), "jobIds": exc.successful_job_ids,
                            "failedJobIds": exc.failed_job_ids})
    if published != job_req.batchSize:
        raise HTTPException(status_code=503, detail="SQS queue is not configured")

    return {"enqueued": job_req.batchSize, "jobIds": job_ids}


system_router = APIRouter()


@system_router.get("/health", response_class=PlainTextResponse, tags=["system"])
def health(request: Request):
    if not request.app.state.chaos.is_healthy:
        return PlainTextResponse("Unhealthy", status_code=500)
    return PlainTextResponse("OK", status_code=200)


@system_router.get("/actuator/health", tags=["system"])
@system_router.get("/actuator/health/liveness", tags=["system"])
@system_router.get("/actuator/health/readiness", tags=["system"])
def actuator_health(request: Request):
    if not request.app.state.chaos.is_healthy:
        return JSONResponse(status_code=503, content={"status": "DOWN"})
    return JSONResponse(status_code=200, content={"status": "UP"})


@system_router.get("/topology", tags=["system"])
def topology(request: Request):
    return {
        "service": "checkout",
        "mode": request.app.state.app_mode,
        "persistenceProvider": request.app.state.repository.provider,
        "queueUrl": request.app.state.sqs_service.queue_url or "N/A",
    }


@system_router.get("/metrics", tags=["system"])
def metrics():
    return Response(content=generate_latest(), media_type=CONTENT_TYPE_LATEST)
