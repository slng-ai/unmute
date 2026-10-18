"""Return only the explicit arguments supplied to this fixture tool."""

from datetime import date as Date

from pydantic import BaseModel
from state import Appointment


class Inspected(BaseModel):
    """Each argument the tool received, as it received it."""

    note: str
    appointment: Appointment | None
    date: Date | None
    accepted: bool | None
    count: int | None
    label: str


def inspect_state(
    note: str,
    appointment: Appointment | None = None,
    date: Date | None = None,
    accepted: bool | None = None,
    count: int | None = None,
    label: str = "",
) -> Inspected:
    """Return the deliberately selected values for the sharing check."""
    return Inspected(note=note, appointment=appointment, date=date, accepted=accepted, count=count, label=label)
