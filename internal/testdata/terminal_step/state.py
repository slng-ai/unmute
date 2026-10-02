"""The call state of terminal_step: every value its agents and tasks share."""

from typing import Annotated, Literal

import phonenumbers
from pydantic import BaseModel, Field
from pydantic_extra_types.phone_numbers import PhoneNumberValidator

# A phone number in E.164, as +34600111222. PhoneNumber alone saves the
# RFC 3966 form ("tel:+34-600-111-222"), which no prompt or tool expects.
Phone = Annotated[
    str | phonenumbers.PhoneNumber, PhoneNumberValidator(number_format="E164")
]


class Booking(BaseModel):
    """One saved booking action."""

    reference: str = Field(description="The booking reference the tool returned.")
    service: str = Field(description="The service booked, as the tool spelled it.")
    action: Literal["create", "cancel"] = Field(description="What the tool did.")


class State(BaseModel):
    """What one call knows, shared by every agent and task in it."""

    customer_phone: Phone | None = Field(
        None,
        description="The caller's number in E.164, saved once the lookup recognises it.",
    )
    booking: Booking | None = Field(
        None, description="The latest successful booking action."
    )
    note: str = Field(
        "", description="What the caller asked to pass on to the stylist."
    )
