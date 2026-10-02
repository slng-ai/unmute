"""The call state of salon-concierge-v2: every value its agents and tasks share."""

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


class Customer(BaseModel):
    """Who the caller is, as the salon's records have them."""

    phone_number: Phone
    status: Literal["existing", "created", "invalid"] = Field(
        description="What the lookup found for the confirmed number: existing for a record that was already there, created for one written during this call, and invalid for a number the lookup could not use."
    )


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

    booking_date: date | None = Field(
        None,
        description='Today\'s date in the salon\'s own timezone, YYYY-MM-DD. Read once from the clock before the greeting, so a caller saying "tomorrow" costs one model request instead of two chained tool calls. A call that crosses midnight keeps the day it started on, which is deliberate: a date changing underneath a conversation would leave the caller and the agent disagreeing about what "tomorrow" means halfway through.',
    )
    caller_reason: list[
        Literal[
            "create_booking",
            "modify_booking",
            "cancel_booking",
            "request_informations",
            "complain",
        ]
    ] = Field(
        [],
        description="Why the caller rang. A list, because one call can do more than one thing: somebody who books and also complains has two reasons, and each step appends the one it heard rather than replacing what the last step recorded.",
    )
    customer: Customer | None = Field(
        None,
        description="Who the caller is, once the verification step has heard them agree to the number and looked the record up. Absent until then: the concierge's own instructions say to run verification before the booking step, and ordering between steps is the prompt's job rather than a code gate.\nNo `default:` for the same reason a default was wrong on the flat status value it replaced. A default is a value the variable holds before the first word, so a defaulted customer would render in the conversation info as a real-looking record before anyone had been looked up.",
    )
    appointments: list[Appointment] = Field(
        [],
        description="What this call booked, moved or cancelled, in the order the caller gave them. A list because a caller can do two things in one call, and the booking step appends rather than replacing, so the second does not erase the first. It starts empty: nothing recorded yet, not a decision the caller made.",
    )
    complaints: list[Complaint] = Field(
        [],
        description="What the caller was unhappy about, one entry per thing, each with what was offered about it. Appended by the complaint step for the same reason the appointments are.",
    )
    customer_phone: Phone | None = Field(
        None,
        description="The caller's phone number in E.164: a plus sign, then digits, with no spaces, brackets or dashes. One shape for every phone number in this package, the MANAGER_PHONE_NUMBER transfer destination included, so no prompt and no tool has to guess which shape it is holding.\nNo `source:` here on purpose. The prefetch block below reads the carrier's fact and this variable receives it, which leaves the per-route refusal for a variable naming a source its target cannot supply exactly as strict as it is: on a route with no caller ID the entry skips and this holds its default.\nOffered to the caller for a yes, never acted on unasked. Somebody may be ringing from a friend's phone, or may hold a second account, so until the verification step has heard them agree this value stays unconfirmed and appears in no prompt but that step's own.\nAn earlier version of this note said no prompt ever reads the number back. That is now false rather than merely out of date: reading it back is the whole saving, and it replaced twelve spoken digits with one yes. The read-back turn does not cache, and that trade was made deliberately.",
    )
    customer_name: str = Field(
        "",
        description="The name on the record the caller's number belongs to, looked up before the greeting. Inherits `customer_phone`'s confirming step, because a name found from a number nobody has agreed to is exactly as unconfirmed as that number was: greeting a stranger by the account holder's name is the worst thing this feature could do, and the compiler refuses the prompt that would.",
    )
