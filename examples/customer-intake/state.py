"""The call state of customer-intake: every value its agents and tasks share."""

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


class CustomerRecord(BaseModel):
    """The record the intake desk opened for this caller."""

    record_id: Id
    opened_on: date
    enquiry: Literal["new_customer", "existing_customer", "complaint", "other"]


# A description is not a note to the next author. Every value a step assigns
# becomes a parameter on that step's finish call, and the description is the
# only thing the model is told about it. So each one is written for the model,
# short, and it reads whole when the value is empty. The reasoning an author
# wants is in these `#` comments, which reach nothing.
class State(BaseModel):
    """What one call knows, shared by every agent and task in it."""

    # confirm: in agent.yaml names the step that must hear a yes. Until it does,
    # this value renders in no prompt but that step's own, and every tool
    # injecting it refuses itself with the step's name. Somebody may be ringing
    # from a friend's phone, so a pre-fetched number is a proposal, not a fact.
    #
    # No `source:` on purpose: the prefetch entry receives the carrier's fact
    # into this value instead. On a route with no caller ID the entry skips and
    # this holds its default.
    caller_phone: Phone | None = Field(
        None,
        description="The caller's number in E.164: a plus sign, then digits, with no spaces, brackets or dashes.",
    )
    # A model with two fields rather than one string, so the two parts read
    # separately: the agent can use the name without saying the address out
    # loud.
    contact: Contact | None = Field(
        None,
        description="Who the caller is and where their confirmation goes, the name and the email address held as two separate parts.",
    )
    # Filled by a dotted assign off `contact`, so the caller spells the address
    # out once and both values come from that one answer. Checked by
    # email-validator with no DNS lookup, because this runs inside a turn and a
    # slow resolver would hold the caller in silence.
    caller_email: EmailStr | None = Field(
        None,
        description="The caller's email address on its own, with no name around it.",
    )
    # A value outside the set is refused where it enters and the refusal lists
    # what was allowed, so the model corrects itself rather than writing
    # something the record cannot hold. Its default is None: an empty string is
    # not one of the four words.
    enquiry: (
        Literal["new_customer", "existing_customer", "complaint", "other"] | None
    ) = Field(
        None, description="What the caller rang about, in one of the four words listed."
    )
    # The one value here the model has to convert rather than copy: a caller says
    # "half four" or "after lunch" and this has to become 16:30 or nothing. A
    # wrong one is refused with the format and the model gets one more go, which
    # is the whole reason the type is not a plain string.
    #
    # `| None` because the caller may simply not give it, and the type has to say
    # so. The step writes `result.callback_time?` in agent.yaml, so a finish call
    # that leaves it out is accepted. No wording in a description reliably stops
    # a model saying "nothing" when there is nothing. The type and the `?` are
    # the fix.
    callback_time: time | None = Field(
        None,
        description="A good time of day to ring the caller back, on the 24 hour clock. Leave it out when the caller has not named one.",
    )
    # Appended with `notes+:` rather than assigned, so a second remark does not
    # overwrite the first.
    notes: list[str] = Field(
        [],
        description="Anything the caller added that no other field holds, one short entry per thing they said.",
    )
    # The model relays the tool's return into the finish call, and that is where
    # the Id and the date on it are actually checked. A wrong one is refused with
    # the format, so the model can correct itself rather than saving a mangled
    # reference.
    record: CustomerRecord | None = Field(
        None,
        description="The record create_customer_record wrote, exactly as it came back. Fill it only after that tool succeeded.",
    )
    # Taken off `record` with a dotted assign, so the reference can be read back
    # to the caller without rendering the whole record into a prompt.
    record_id: Id | None = Field(
        None, description="The reference number for this caller's record, on its own."
    )
    # Read once from the clock before the greeting. No step assigns it, so its
    # description is what a prompt reader sees rather than something the model
    # fills in.
    today_date: date | None = Field(
        None, description="Today's date at the desk, written year-month-day."
    )
