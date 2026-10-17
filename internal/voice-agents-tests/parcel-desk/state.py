"""What a Parcel Desk call knows: the same State unmute-core builds from type text.

The core example writes it as variables (``Date | None``, ``Phone | None``,
``Parcel | None``) and one ``Parcel`` shape. Here it is a normal Pydantic model.
"""

from datetime import date
from typing import Literal

from pydantic import BaseModel
from unmute_core.state import Id, Phone


class Parcel(BaseModel):
    """One parcel, as the depot knows it."""

    tracking_id: Id
    status: Literal["in_transit", "out_for_delivery", "delivered", "held"]
    delivery_day: date | None = None
    town: str


class State(BaseModel):
    """One parcel call. Every value starts empty.

    Attributes:
        today: Today, on the depot clock.
        caller_phone: The caller's number, from the call itself.
        parcel: The parcel the caller asked about.
        day: The day the caller asked to move the delivery to.
        callback_note: What the caller wants a person to call them back about.
    """

    today: date | None = None
    caller_phone: Phone | None = None
    parcel: Parcel | None = None
    day: date | None = None
    callback_note: str | None = None
