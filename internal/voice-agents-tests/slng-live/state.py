"""The call state of slng-live: every value its agent and tasks share."""

from datetime import date
from typing import Annotated, Literal

import phonenumbers
from pydantic import BaseModel, Field
from pydantic_extra_types.phone_numbers import PhoneNumberValidator

Phone = Annotated[
    str | phonenumbers.PhoneNumber, PhoneNumberValidator(number_format="E164")
]


class Booking(BaseModel):
    """One appointment the salon saved."""

    reference: str = Field(description="The booking reference.")
    day: date = Field(description="The day of the appointment, written year-month-day.")
    time: str = Field(description="The start time, as 10:00.")
    service: Literal["cut", "colour"]


class State(BaseModel):
    """What one call knows, shared by the agent and its tasks."""

    phone_number: Phone | None = Field(
        None, description="The caller's number in E.164, confirmed with them."
    )
    booking: Booking | None = Field(None, description="The appointment booked on this call.")
