# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
# SPDX-License-Identifier: MIT-0

from pydantic import BaseModel


class Recommendation(BaseModel):
    productId: str


class HTTPError(BaseModel):
    code: int
    message: str
