"""What one salon call knows, shared by both agents and every task.

Ported from unmute-core's examples/salon-concierge-unmute. A line code speaks reads the
tool's input or result directly, so nothing is saved here only to be said.
"""

from datetime import date, time
from typing import Literal

from pydantic import BaseModel, Field
from unmute_core.state import Id, Phone


class Appointment(BaseModel):
    """The latest booking that was created, moved or cancelled."""

    booking_id: Id
    service: Literal["haircut", "hair-color", "blowout"]
    date: date
    time: time
    action: Literal["book", "move", "cancel"]


class State(BaseModel):
    """What one call knows. Every value has a default, as ``CallState`` requires."""

    appointment: Appointment | None = Field(
        None, description="The latest successful booking action, saved after the tool succeeds."
    )
    booking_date: date | None = Field(
        None, description="Today's date in the salon's timezone, read once before the greeting."
    )
    booking_weekday: str = Field("", description="The weekday of booking_date, in English.")
    salon_local_time: time | None = Field(
        None, description="The salon's wall clock when the call began."
    )
    customer_phone: Phone | None = Field(
        None,
        description=(
            "The caller's phone number in E.164. It shows in no prompt but verify_customer's "
            "until the caller agrees to it."
        ),
    )
    requested_service: Literal["haircut", "hair-color", "blowout"] | None = Field(
        None, description="The service the caller asked for. Leave it out if they did not say."
    )
    requested_day: date | None = Field(
        None,
        description=(
            "The day the caller asked for, YYYY-MM-DD. Leave it out if they did not name one; "
            "never guess today."
        ),
    )
    requested_intent: Literal["book", "move", "cancel"] | None = Field(
        None,
        description="Whether the caller wants to book a new time, move theirs, or cancel it.",
    )
    customer_verified: str = Field(
        "",
        description=(
            "What verification found on this call: existing or created. Empty means nobody "
            "was verified yet. Customer care reads it instead of the phone number."
        ),
    )
