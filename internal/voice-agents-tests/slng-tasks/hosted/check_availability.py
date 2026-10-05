"""Say whether the salon has a free slot on a day for a service."""

from datetime import date
from typing import Literal

from pydantic import BaseModel, Field


class Input(BaseModel):
    day: date = Field(description="The day the caller wants, written year-month-day.")
    service: Literal["cut", "colour"] = Field(
        description="The service the caller wants."
    )


class Output(BaseModel):
    free: bool
    reason: str


def handler(input: Input) -> Output:
    # The salon is closed on Sundays, and colour takes the whole of a Monday.
    if input.day.weekday() == 6:
        return Output(free=False, reason="The salon is closed on Sundays.")
    if input.service == "colour" and input.day.weekday() == 0:
        return Output(free=False, reason="Colour is not booked on Mondays.")
    return Output(free=True, reason="There is a free slot.")
