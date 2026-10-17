"""The salon's tools. Their types are the tools' types."""

from datetime import date
from typing import Literal

from pydantic import BaseModel
from state import Appointment, Booking


class Slots(BaseModel):
    """The free times on one day."""

    status: Literal["free", "none_free", "closed"]
    slots: list[str] = []
    read_back: str = ""


class Saved(BaseModel):
    """The outcome of saving a booking."""

    status: Literal["booked", "slot_taken"]
    appointment: Appointment


class Account(BaseModel):
    """The caller's account."""

    account_id: str


def find_slots(day: date, service: Literal["cut", "colour"] = "cut") -> Slots:
    """Find free times for a service on one day.

    Args:
        day: The day to look at.
        service: The service the caller wants.
    """
    return Slots(status="free", slots=["10:00", "14:30"])


def check_day(day: date) -> Slots | None:
    """Refuse a Sunday before anything runs.

    Args:
        day: The day to look at.
    """
    return Slots(status="closed") if day.weekday() == 6 else None


def save_booking(booking: Booking, customer_phone: str = "") -> Saved:
    """Save the booking the caller agreed to.

    Args:
        booking: What to book.
        customer_phone: The caller's number.
    """
    return Saved(
        status="booked",
        appointment=Appointment(
            reference="A1", spoken=f"{booking.day} at {booking.time}"
        ),
    )


def lookup_account(caller_phone: str) -> Account:
    """Find the caller's account by their number.

    Args:
        caller_phone: The number the caller rang from.
    """
    return Account(account_id=f"acct-{caller_phone[-4:]}")
