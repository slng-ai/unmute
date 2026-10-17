"""What one Nosh call knows: one Pydantic model, typed field by field.

The model saves none of these directly. Each tool's checked result saves them, through
the `assign:` rows in tools/*.yaml, and a new area takes back the places on offer and the
details said.

`listed`, `spoken` and `place_name` hold the place service's words so a spoken line can
read them as {{state.x}}.
"""

from typing import Annotated

from pydantic import BaseModel, Field, StringConstraints

CustomerName = Annotated[str, StringConstraints(strip_whitespace=True, min_length=1, max_length=40)]


class Spot(BaseModel):
    """The location the caller's words resolved to."""

    formatted_address: str
    latitude: float
    longitude: float
    at_address: bool = Field(description="An exact address or a point they are at.")
    craving: str | None = Field(default=None, description="What they asked for with the area.")


class Offered(BaseModel):
    """One place on offer, by the id the caller picks it with."""

    id: str
    name: str
    place_id: str


class Offer(BaseModel):
    """The places on offer for the spot and the craving."""

    places: list[Offered]


class Details(BaseModel):
    """What code said about the place the caller picked."""

    place: str
    name: str
    address: str
    rating: float | None = None
    price: str = ""
    today: str = ""
    phone: str | None = None
    website: str | None = None


class State(BaseModel):
    """One call. Only the caller's name is known when it starts.

    Attributes:
        customer_name: Who asked for the call, from the dispatch's call_start.
        spot: The location read back to the caller.
        craving: What they are in the mood for.
        offer: The places on offer, p1 to p6.
        details: The details of the place they picked.
        listed: The places on offer in words, as code says them.
        spoken: What code says about the picked place.
        place_name: The name of the place the last details lookup was for.
    """

    customer_name: CustomerName | None = None
    spot: Spot | None = None
    craving: str | None = None
    offer: Offer | None = None
    details: Details | None = None
    listed: str | None = None
    spoken: str | None = None
    place_name: str | None = None
