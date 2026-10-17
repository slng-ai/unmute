"""Parcel Desk's tools: plain Python functions over an in-memory depot.

Ported from unmute-core's examples/parcel-desk. The depot resets on each process start.
A failure the model should read is a ``ToolError``.
"""

import re
from datetime import date, timedelta
from typing import Annotated, Any, Literal

from pydantic import BaseModel, Field
from state import Parcel
from unmute_core.tools import ToolError

TrackingId = Annotated[str, Field(description="The parcel's tracking id, like PD-1042.")]

PARCELS: dict[str, dict[str, Any]] = {
    "PD-1042": {
        "tracking_id": "PD-1042",
        "status": "in_transit",
        "delivery_day": (date.today() + timedelta(days=2)).isoformat(),
        "town": "Leeds",
    },
    "PD-2077": {
        "tracking_id": "PD-2077",
        "status": "held",
        "delivery_day": None,
        "town": "York",
    },
}
"""The parcels the depot holds."""
NOTES: list[dict[str, str]] = []
"""The call-back notes left on this process."""


class Found(BaseModel):
    """What ``find_parcel`` returns: the parcel, or a hint for the caller."""

    found: bool
    parcel: Parcel | None = None
    hint: str = ""


class Rescheduled(BaseModel):
    """What ``reschedule_delivery`` returns."""

    status: Literal["rescheduled"]
    parcel: Parcel


class NoteSaved(BaseModel):
    """What ``save_callback_note`` returns."""

    saved: bool
    note: str


class Ended(BaseModel):
    """What ``end_call`` returns."""

    ended: bool


def depot_key(tracking_id: str) -> str:
    """Return a tracking id the way the depot keys it: ``PD-1042`` from "p d 1 0 4 2".

    A caller reads the id aloud, so speech to text gives "PD 1042" or "P D 1042".
    """
    letters = re.sub(r"[^A-Z0-9]", "", tracking_id.upper())
    return f"{letters[:2]}-{letters[2:]}"


def find_parcel(tracking_id: TrackingId) -> Found:
    """Find a parcel by its tracking id: where it is and the day it is due."""
    parcel = PARCELS.get(depot_key(tracking_id))
    if parcel is None:
        return Found(found=False, hint="No parcel has that id. Ask the caller to read it again.")
    return Found(found=True, parcel=Parcel.model_validate(parcel))


def reschedule_delivery(tracking_id: TrackingId, day: date) -> Rescheduled:
    """Move a parcel's delivery to another day, from tomorrow on."""
    parcel = PARCELS.get(tracking_id)
    if parcel is None:
        raise ToolError("no parcel has that id")
    if day <= date.today():
        raise ToolError("a delivery can be moved to tomorrow at the earliest")
    parcel["delivery_day"] = day.isoformat()
    return Rescheduled(status="rescheduled", parcel=Parcel.model_validate(parcel))


def save_callback_note(note: str, call: str) -> NoteSaved:
    """Leave a note for a person to call the caller back about."""
    NOTES.append({"note": note, "call": call})
    return NoteSaved(saved=True, note=note)


def end_call() -> Ended:
    """End the call once the caller says they are done. Say your goodbye in the same reply."""
    return Ended(ended=True)
