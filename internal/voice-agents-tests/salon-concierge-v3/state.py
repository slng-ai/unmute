"""The call state of salon-concierge-v3: every value its agents and tasks share."""

from datetime import date, time
from typing import Annotated, Literal

import phonenumbers
from pydantic import BaseModel, EmailStr, Field, StringConstraints
from pydantic_extra_types.phone_numbers import PhoneNumberValidator

# A phone number in E.164, as +34600111222. PhoneNumber alone saves the
# RFC 3966 form ("tel:+34-600-111-222"), which no prompt or tool expects.
Phone = Annotated[
    str | phonenumbers.PhoneNumber, PhoneNumberValidator(number_format="E164")
]
# An identifier: letters and digits, then dot, dash, underscore or colon.
Id = Annotated[str, StringConstraints(pattern=r"^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$")]


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
    scheduled_time: time
    appointment_type: Literal[
        "haircut", "haircolor", "haircut_and_haircolor", "dry_cut"
    ] = Field(description="The service the caller asked for, in the salon's own words.")
    action: Literal["create", "modify", "cancel"] = Field(
        description="What this call did to this appointment."
    )
    booking_id: Id | None = Field(
        None,
        description="The diary's own id for the booking, once one exists. Absent while the caller is still choosing a time.",
    )


class Complaint(BaseModel):
    """One thing the caller is unhappy about."""

    complaint_id: Id
    reason: Literal["service_quality", "waiting_time", "price", "staff", "other"] = (
        Field(description="What the complaint is about, in the salon's own categories.")
    )
    about: Appointment | None = Field(
        None,
        description="The appointment the complaint concerns, when it concerns one. A complaint about the salon in general has none.",
    )
    resolution: Literal["refund_offered", "rebooking_offered", "escalated", "noted"] = (
        Field(
            description="What was offered or done about it on this call. Three of these are offers and not outcomes: a refund offered is not a refund approved, a rebooking offered is not a rebooking confirmed, and escalated means somebody will look, not that they agreed. Only what a tool did is done. Say this value out loud in the tense it is written in."
        )
    )


class State(BaseModel):
    """What one call knows, shared by every agent and task in it."""

    stylist_note: str = Field(
        "",
        description="What the caller asked to pass on to their stylist, in their own words.",
    )
    today_date: date | None = Field(
        None, description="Today's date in the salon's timezone, in YYYY-MM-DD form."
    )
    complaints: list[Complaint] = Field(
        [], description="Complaints recorded during this call."
    )
    customer_phone: Phone | None = Field(
        None, description="The phone number the caller agreed to use, in E.164 form."
    )
    customer_name: str = Field(
        "", description="The name returned by the customer lookup."
    )
    customer_id: Id | None = Field(
        None, description="The customer ID returned by the customer lookup."
    )
    customer_status: Literal["existing", "created", "invalid"] | None = Field(
        None, description="Whether the customer was found, created, or invalid."
    )
    appointment_id: Id | None = Field(
        None, description="The exact appointment ID returned by the booking tool."
    )
    appointment_date: date | None = Field(
        None, description="The exact date selected with the caller."
    )
    appointment_time: time | None = Field(
        None, description="The exact time selected with the caller."
    )
    appointment_slot_id: str | None = Field(
        None,
        description="The exact slot ID returned by availability, including its separators.",
    )
    appointment_service: (
        Literal["haircut", "haircolor", "haircut_and_haircolor", "dry_cut"] | None
    ) = Field(None, description="The service selected with the caller.")
    confirmation_contact: Contact | None = Field(
        None,
        description="Who the confirmation goes to, name and address held apart, so a prompt can name the person without reading their address out loud. Here to find what only a real call finds: whether a model can turn a spelled-out address into the written form on the first try, and whether the refusal it gets back when it cannot is one it can correct itself from.",
    )
    confirmation_email: EmailStr | None = Field(
        None,
        description="The address on its own, taken off confirmation_contact rather than asked for twice.",
    )
