"""The call state of customer-intake: every value its agents and tasks share."""

from datetime import date, time
from typing import Annotated, Literal

import phonenumbers
from pydantic import BaseModel, EmailStr, Field, NameEmail, StringConstraints
from pydantic_extra_types.phone_numbers import PhoneNumberValidator

# A phone number in E.164, as +34600111222. PhoneNumber alone saves the
# RFC 3966 form ("tel:+34-600-111-222"), which no prompt or tool expects.
Phone = Annotated[
    str | phonenumbers.PhoneNumber, PhoneNumberValidator(number_format="E164")
]
# An identifier: letters and digits, then dot, dash, underscore or colon.
Id = Annotated[str, StringConstraints(pattern=r"^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$")]


class CustomerRecord(BaseModel):
    """The record the intake desk opened for this caller."""

    record_id: Id
    opened_on: date
    enquiry: Literal["new_customer", "existing_customer", "complaint", "other"]


class State(BaseModel):
    """What one call knows, shared by every agent and task in it."""

    caller_phone: Phone | None = Field(
        None,
        description="The caller's number in E.164: a plus sign, then digits, with no spaces, brackets or dashes.",
    )
    contact: NameEmail | None = Field(
        None,
        description="Who the caller is and where their confirmation goes, the name and the email address held as two separate parts.",
    )
    caller_email: EmailStr | None = Field(
        None,
        description="The caller's email address on its own, with no name around it.",
    )
    enquiry: (
        Literal["new_customer", "existing_customer", "complaint", "other"] | None
    ) = Field(
        None, description="What the caller rang about, in one of the four words listed."
    )
    callback_time: time | None = Field(
        None,
        description="A good time of day to ring the caller back, on the 24 hour clock. Leave it out when the caller has not named one.",
    )
    notes: list[str] = Field(
        [],
        description="Anything the caller added that no other field holds, one short entry per thing they said.",
    )
    record: CustomerRecord | None = Field(
        None,
        description="The record create_customer_record wrote, exactly as it came back. Fill it only after that tool succeeded.",
    )
    record_id: Id | None = Field(
        None, description="The reference number for this caller's record, on its own."
    )
    today_date: date | None = Field(
        None, description="Today's date at the desk, written year-month-day."
    )
