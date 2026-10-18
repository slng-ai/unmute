"""Local handlers for the terminal-step fixture.

Deterministic on purpose: a test asserts on what the step saved, so a random
reference would make the assertion unwritable.
"""

from typing import Annotated, Literal

from pydantic import BaseModel, Field
from state import Booking


class Customer(BaseModel):
    """The customer the number belongs to."""

    customer_phone: str
    status: Literal["existing", "created", "invalid"]
    summary: str


class Booked(BaseModel):
    """The outcome of booking."""

    status: Literal["booked", "not_confirmed", "slot_unavailable"]
    summary: str
    booking: Booking | None = Field(None, description="Present and complete on booked, absent otherwise.")


class Cancelled(BaseModel):
    """The outcome of cancelling."""

    status: Literal["cancelled", "not_confirmed", "not_found"]
    summary: str
    booking: Booking | None = Field(None, description="Present and complete on cancelled, absent otherwise.")


def look_up(phone: Annotated[str, Field(description="The number the caller agreed to, digits only.")]) -> Customer:
    """Look one customer up by the phone number the caller agreed to.

    It creates a record when there is none. The lookup decides whether a number
    is usable.
    """
    digits = "".join(character for character in phone if character.isdigit())
    if len(digits) < 8:
        return Customer(customer_phone="", status="invalid", summary="That number is too short to look up.")
    return Customer(customer_phone="+" + digits, status="existing", summary="A customer was found.")


def book_it(
    service: Literal["haircut", "blowout"],
    confirmed: Annotated[bool, Field(description="True only after the caller agrees to this exact booking.")],
    customer_phone: str = "",
) -> Booked:
    """Book one appointment for the verified caller."""
    if not confirmed:
        return Booked(status="not_confirmed", summary="The caller has not agreed yet.")
    return Booked(
        status="booked",
        summary="Booking saved.",
        booking=Booking(reference="bkg_0001", service=service, action="create"),
    )


def cancel_it(
    reference: Annotated[str, Field(description="The reference of the booking to cancel.")],
    confirmed: Annotated[bool, Field(description="True only after the caller agrees to the cancellation.")],
) -> Cancelled:
    """Cancel the caller's booking."""
    if not confirmed:
        return Cancelled(status="not_confirmed", summary="The caller has not agreed yet.")
    if reference != "bkg_0001":
        return Cancelled(status="not_found", summary="No booking with that reference.")
    return Cancelled(
        status="cancelled",
        summary="Booking cancelled.",
        booking=Booking(reference=reference, service="haircut", action="cancel"),
    )
