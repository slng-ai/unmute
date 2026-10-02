"""The call state of typed_state: every value its agents and tasks share."""

from datetime import date, time
from typing import Annotated, Literal

import phonenumbers
from pydantic import BaseModel, EmailStr, Field
from pydantic_extra_types.phone_numbers import PhoneNumberValidator

# A phone number in E.164, as +34600111222. PhoneNumber alone saves the
# RFC 3966 form ("tel:+34-600-111-222"), which no prompt or tool expects.
Phone = Annotated[
    str | phonenumbers.PhoneNumber, PhoneNumberValidator(number_format="E164")
]


class Contact(BaseModel):
    """A person's name and email address, held as two separate parts.

    Two fields rather than Pydantic's NameEmail, which is one string: a prompt
    can then use the name without saying the address out loud.
    """

    name: str
    email: EmailStr


class Appointment(BaseModel):
    """One thing being booked, moved or cancelled."""

    scheduled_date: date
    scheduled_time: time = Field(description="The time the caller agreed to.")
    appointment_type: Literal["haircut", "dry_cut"] = Field(
        description="The service the caller asked for, in the salon's own words."
    )


class State(BaseModel):
    """What one call knows, shared by every agent and task in it."""

    count: int | None = None
    accepted: bool | None = None
    caller_reason: list[Literal["create_booking", "cancel_booking"]] = Field(
        [],
        description="Why the caller rang. More than one, because one call can do more than one thing.",
    )
    appointments: list[Appointment] = Field(
        [],
        description="What this call has booked so far, in the order the caller gave them.",
    )
    caller_phone: Phone | None = Field(
        None,
        description="The number the caller rang from, once they have agreed it is theirs.",
    )
    last_appointment: Appointment | None = Field(
        None,
        description="The appointment most recently booked on this call, so a prompt can name one part of it.",
    )
    reminder_email: EmailStr | None = Field(
        None,
        description="Where the caller wants the reminder sent, saved only once they have spelled it out.",
    )
    booked_for: Contact | None = Field(
        None,
        description="Who the appointment is under, with their name and their email address held apart.",
    )
