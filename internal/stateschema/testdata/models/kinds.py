"""Every kind of field a state may hold, for the schema reader's tests."""

from datetime import date, time
from enum import Enum
from typing import Annotated, Literal

import phonenumbers
from pydantic import BaseModel, EmailStr, Field, NameEmail, StringConstraints
from pydantic_extra_types.currency_code import ISO4217
from pydantic_extra_types.language_code import LanguageAlpha2
from pydantic_extra_types.phone_numbers import PhoneNumberValidator

Phone = Annotated[
    str | phonenumbers.PhoneNumber, PhoneNumberValidator(number_format="E164")
]
Id = Annotated[str, StringConstraints(pattern=r"^[A-Z0-9-]{3,32}$")]


class Tier(Enum):
    """A closed set written as an Enum class."""

    GOLD = "gold"
    SILVER = "silver"


class Appointment(BaseModel):
    """One booking."""

    booking_id: Id
    day: date
    at: time | None = None
    service: Literal["haircut", "blowout"]


class Customer(BaseModel):
    """A caller and the bookings they hold."""

    name: str
    email: EmailStr | None = None
    bookings: list[Appointment] = []


class State(BaseModel):
    """The call state."""

    caller_phone: Phone | None = Field(
        None, description="The caller's number in E.164."
    )
    verified: bool = False
    visits: int = 0
    spend: float = 0.0
    contact: NameEmail | None = None
    tier: Tier | None = None
    enquiry: Literal["new", "existing"] | None = None
    single: Literal["only"] = "only"
    notes: list[str] = Field(default_factory=list)
    customer: Customer | None = None
    history: list[Appointment] = []
    currency: ISO4217 | None = None
    language: LanguageAlpha2 | None = None
    greeting: str = "Hello"
