"""The call state of slng_tasks: every value its agent and tasks share."""

from datetime import date
from typing import Annotated, Literal

import phonenumbers
from pydantic import BaseModel, Field
from pydantic_extra_types.phone_numbers import PhoneNumberValidator

Phone = Annotated[
    str | phonenumbers.PhoneNumber, PhoneNumberValidator(number_format="E164")
]


class Booking(BaseModel):
    """One appointment the caller asked for."""

    day: date = Field(description="The day of the appointment, written year-month-day.")
    service: Literal["cut", "colour"]


class State(BaseModel):
    """What one call knows, shared by the agent and its tasks."""

    customer_name: str = "there"
    phone_number: Phone | None = Field(
        None, description="The caller's number in E.164, confirmed with them."
    )
    booking: Booking | None = Field(
        None, description="The appointment the caller asked for."
    )
    notes: list[str] = Field(
        [], description="Anything the caller asked us to pass on, one entry each."
    )
