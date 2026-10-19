# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

from typing import Any, Optional
from pydantic import BaseModel, Field, field_validator


class ItemRequest(BaseModel):
    id: str
    name: str
    quantity: int
    price: int


class Item(BaseModel):
    id: str
    name: str
    quantity: int
    price: int
    totalCost: int


class ShippingAddress(BaseModel):
    firstName: str
    lastName: str
    address1: str
    address2: Optional[str] = ""
    city: str
    state: str
    zip: str
    email: str


class ShippingOption(BaseModel):
    name: str
    amount: int
    token: str
    estimatedDays: int


class ShippingRates(BaseModel):
    shipmentId: str
    rates: list[ShippingOption]


class CheckoutRequest(BaseModel):
    items: list[ItemRequest]
    shippingAddress: Optional[ShippingAddress] = None
    deliveryOptionToken: Optional[str] = None


class Checkout(BaseModel):
    items: list[Item]
    shippingAddress: Optional[ShippingAddress] = None
    deliveryOptionToken: Optional[str] = ""
    shippingRates: Optional[ShippingRates] = None
    paymentId: str
    paymentToken: str
    subtotal: int
    shipping: int
    tax: int
    total: int


class CheckoutSubmitted(BaseModel):
    orderId: str
    email: str
    items: list[Item]
    subtotal: int
    shipping: int
    tax: int
    total: int


class JobRequest(BaseModel):
    batchSize: int = Field(default=1, ge=1, le=10)
    workUnits: int = Field(default=100, ge=1, le=10000)
    dbMode: str = Field(default="light")
    customerId: Optional[str] = None
    payload: Optional[dict[str, Any]] = None

    @field_validator("dbMode")
    @classmethod
    def validate_db_mode(cls, v: str) -> str:
        mode = v.lower().strip()
        if mode not in ("none", "light", "full"):
            raise ValueError("dbMode must be one of: 'none', 'light', 'full'")
        return mode


class JobResponse(BaseModel):
    enqueued: int
    jobIds: list[str]
