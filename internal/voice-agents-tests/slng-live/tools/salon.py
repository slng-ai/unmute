"""Sage and Stone's diary. Deterministic, so a test can say what a call books.

SLNG runs each of these as a code tool with no internet access, so the diary
is held here: every weekday has the same three free times.
"""

from datetime import date
from typing import Annotated, Literal

from pydantic import BaseModel, Field

_TIMES = ("10:00", "12:30", "16:00")


class Slots(BaseModel):
    """The free times on one day."""

    status: Literal["free", "closed"]
    times: list[str] = []


class Booking(BaseModel):
    """One appointment the salon saved."""

    reference: str = Field(description="The booking reference.")
    day: date = Field(description="The day of the appointment, written year-month-day.")
    time: str = Field(description="The start time, as 10:00.")
    service: Literal["cut", "colour"]


class Booked(BaseModel):
    """The outcome of booking one time."""

    status: Literal["booked", "taken"]
    booking: Booking | None = None


def find_free_slots(
    day: Annotated[date, Field(description="The day the caller wants, year-month-day.")],
) -> Slots:
    """Find the free times on one day. The salon is closed on Sundays."""
    if day.weekday() == 6:
        return Slots(status="closed")
    return Slots(status="free", times=list(_TIMES))


def book_slot(
    day: Annotated[date, Field(description="The day the caller picked, year-month-day.")],
    time: Annotated[str, Field(description="One of the free times, as 10:00.")],
    service: Literal["cut", "colour"],
    phone_number: str = "",
) -> Booked:
    """Book one free time for the caller. Call it once the caller has picked a time."""
    if day.weekday() == 6 or time not in _TIMES:
        return Booked(status="taken")
    reference = f"SS-{day:%m%d}-{time.replace(':', '')}"
    return Booked(status="booked", booking=Booking(reference=reference, day=day, time=time, service=service))
