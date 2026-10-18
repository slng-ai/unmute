"""Sage and Stone's customers: a mock that knows one phone number."""

from typing import Annotated

from pydantic import BaseModel, Field

_CUSTOMERS = {
    "+1555010101": {"customer_id": "cus_1001", "name": "Alex Morgan"},
}


class Customer(BaseModel):
    """The customer the phone number belongs to, when there is one."""

    found: bool
    customer_id: str
    name: str


def lookup_customer(phone: Annotated[str, Field(description="Customer phone number")]) -> Customer:
    """Identify a Sage and Stone customer by phone number.

    Do it before any availability check or appointment change. Use only the
    returned customer_id; never invent one or use a placeholder.
    """
    digits = "".join(character for character in phone if character.isdigit())
    customer = _CUSTOMERS.get(f"+{digits}")
    if customer is None:
        return Customer(found=False, customer_id="", name="")
    return Customer(found=True, **customer)
