"""The call state of salon-concierge: every value its agents and tasks share."""

from datetime import date, time
from typing import Annotated, Literal

import phonenumbers
from pydantic import BaseModel, Field, StringConstraints
from pydantic_extra_types.phone_numbers import PhoneNumberValidator

# A phone number in E.164, as +34600111222. PhoneNumber alone saves the
# RFC 3966 form ("tel:+34-600-111-222"), which no prompt or tool expects.
Phone = Annotated[
    str | phonenumbers.PhoneNumber, PhoneNumberValidator(number_format="E164")
]
# An identifier: letters and digits, then dot, dash, underscore or colon.
Id = Annotated[str, StringConstraints(pattern=r"^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$")]


class Appointment(BaseModel):
    """The latest booking successfully created, moved, or cancelled."""

    booking_id: Id
    service: Literal["haircut", "hair-color", "blowout"]
    date: date
    spoken: str
    time: time
    action: Literal["book", "move", "cancel"]


class State(BaseModel):
    """What one call knows, shared by every agent and task in it."""

    appointment: Appointment | None = Field(
        None,
        description="The latest successful booking action, saved only after the tool succeeds.",
    )
    booking_date: date | None = Field(
        None,
        description='Today\'s date in the salon\'s own timezone, YYYY-MM-DD. Read once from the clock before the greeting, so a caller saying "tomorrow" costs one model request instead of two chained tool calls. A call that crosses midnight keeps the day it started on, which is deliberate: a date changing underneath a conversation would leave the caller and the agent disagreeing about what "tomorrow" means halfway through.',
    )
    booking_weekday: str = Field(
        "",
        description='The weekday today falls on, spelled out in English: Monday to Sunday. From the same clock reading as `booking_date`, so the two cannot disagree about which day it is. It is what lets the agent answer "next Friday" without a second turn working out what day today is first.',
    )
    salon_local_time: time | None = Field(
        None,
        description='The salon\'s own wall clock when the call began, HH:MM on a 24 hour clock. From the same reading as the two above. It is what lets the agent know that a caller asking for "this afternoon" at 17:40 is asking for something that has nearly passed, rather than offering them a slot that is already gone.',
    )
    customer_phone: Phone | None = Field(
        None,
        description="The caller's phone number in E.164: a plus sign, then digits, with no spaces, brackets or dashes. One shape for every phone number in this package, the MANAGER_PHONE_NUMBER transfer destination included, so no prompt and no tool has to guess which shape it is holding.\nNo `source:` here on purpose. The prefetch block below reads the carrier's fact and this variable receives it, which leaves the per-route refusal for a variable naming a source its target cannot supply exactly as strict as it is: on a route with no caller ID the entry skips and this holds its default.\nOffered to the caller for a yes, never acted on unasked. Somebody may be ringing from a friend's phone, or may hold a second account, so until the verification step has heard them agree this value stays unconfirmed and appears in no prompt but that step's own.",
    )
    customer_verified: str = Field(
        "",
        description="Whether verification has already run on this call, and what it found. Set by the verification step as it ends, which only happens on a number the caller agreed to, so a value here means the number beside it is confirmed.\nIt exists for customer care, which has to decide whether to identify the caller before writing a complaint down and cannot read `customer_phone` to find out: an unconfirmed number renders in no prompt but the verification step's own, and a confirmed one must not be in front of an agent that might say it out loud. This carries the same fact with nothing sensitive in it.\nThe concierge does not read it. The `book` flow decides the same thing structurally with `skip_when_confirmed:`, off the confirmation itself rather than off a copy of it.\nEmpty means nobody has been verified yet on this call.",
    )
