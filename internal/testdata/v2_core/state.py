"""The call state of v2_core: every value its agents, tasks and tools share."""

from datetime import date, time
from typing import Literal

from pydantic import BaseModel, Field


class Booking(BaseModel):
    """What the caller asked to book, read back before it is saved."""

    service: Literal["cut", "colour"]
    day: date
    time: time


class Appointment(BaseModel):
    """A saved appointment."""

    reference: str
    spoken: str = Field(description="The appointment as the agent says it aloud.")


class State(BaseModel):
    """What one call knows."""

    caller_name: str = ""
    caller_phone: str = ""
    account_id: str = ""
    reason: str = ""
    preferred_day: date | None = None
    offered_slots: list[str] = []
    booking: Booking | None = None
    appointment: Appointment | None = None
