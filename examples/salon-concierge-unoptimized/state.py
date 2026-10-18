"""What a salon-concierge-unoptimized call keeps: nothing.

This package saves no values and injects none, on purpose. `agent.yaml` version 2
still reads a State class from this file. ``Appointment`` is here because the
booking tool returns one.
"""

from datetime import date, time
from typing import Literal

from pydantic import BaseModel
from unmute_core.state import Id


class Appointment(BaseModel):
    """The latest booking successfully created, moved, or cancelled."""

    booking_id: Id
    service: Literal["haircut", "hair-color", "blowout"]
    date: date
    spoken: str
    time: time
    action: Literal["book", "move", "cancel"]


class State(BaseModel):
    """One call. It holds no values."""
