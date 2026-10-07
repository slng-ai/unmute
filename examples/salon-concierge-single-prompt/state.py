"""The call state of salon-concierge-single-prompt: what the pre-fetch reads before the greeting."""

from datetime import date, time
from typing import Annotated

import phonenumbers
from pydantic import BaseModel, Field
from pydantic_extra_types.phone_numbers import PhoneNumberValidator

# A phone number in E.164, as +34600111222. PhoneNumber alone saves the
# RFC 3966 form ("tel:+34-600-111-222"), which no prompt or tool expects.
Phone = Annotated[
    str | phonenumbers.PhoneNumber, PhoneNumberValidator(number_format="E164")
]


class State(BaseModel):
    """What one call knows before anybody speaks. Nothing writes to it later."""

    booking_date: date | None = Field(
        None,
        description="Today's date in the salon's own timezone, YYYY-MM-DD.",
    )
    booking_weekday: str = Field(
        "",
        description="The weekday today falls on, spelled out in English: Monday to Sunday.",
    )
    salon_local_time: time | None = Field(
        None,
        description="The salon's own wall clock when the call began, HH:MM on a 24 hour clock.",
    )
    customer_phone: Phone | None = Field(
        None,
        description=(
            "The number the carrier says is calling, in E.164. Empty on a browser call. "
            "Only a suggestion: the prompt reads it back and the caller may give another."
        ),
    )
