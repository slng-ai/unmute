"""Nosh's tools: find the area, find places there, say one place's details, end the call.

Ported from unmute-core's examples/nosh. Each tool returns a typed result. The tool files
in this folder save what it returns and say code's next line, so the model asks for no
reply and a caller turn costs one model request.

- A search runs only on the spot read back to the caller: find_places gets it injected.
- A pick counts only if it was offered: place_details gets the offer injected and refuses
  any id it does not hold.
- Prices and hours are worded here, from the place service's own fields, never by the model.

Without TOOLS_API_KEY the places come from a few demo Barcelona places.
"""

import json
from dataclasses import dataclass, field
from datetime import date, datetime
from typing import Annotated, Any, Protocol
from urllib.error import HTTPError
from urllib.request import Request, urlopen
from zoneinfo import ZoneInfo

from pydantic import BaseModel, Field, TypeAdapter
from state import Details, Offer, Offered, Spot
from unmute_core.settings import env_value
from unmute_core.tools import ToolError


DEFAULT_URL = "https://tools-agent-infra.onrender.com"


class NotFoundError(Exception):
    """The service has nothing for that location or place."""


class Places(Protocol):
    """Anything that answers the four place routes."""

    def post(self, route: str, body: dict[str, Any]) -> Any:
        """Return one route's JSON answer, or raise ``NotFoundError``."""
        ...


class Located(BaseModel):
    """What ``resolve-location`` answers."""

    latitude: float
    longitude: float
    formatted_address: str


class Place(BaseModel):
    """One search result: a search knows a place's name, kind, rating and reviews only."""

    place_id: str
    name: str
    address: str
    rating: float | None = None
    user_rating_count: int | None = None
    primary_type_display_name: str | None = None


class PlaceInfo(BaseModel):
    """What ``place-details`` answers for one place."""

    place_id: str
    name: str
    address: str
    rating: float | None = None
    price_level: str | None = None
    opening_hours: list[str] | None = None
    phone: str | None = None
    website: str | None = None
    primary_type_display_name: str | None = None


@dataclass
class PlaceService:
    """The hosted place tools: one bearer-token ``POST`` per route, no retry."""

    url: str = DEFAULT_URL
    key: str = ""
    timeout: float = 20.0

    def post(self, route: str, body: dict[str, Any]) -> Any:
        """Return one route's JSON answer. A 404 is ``NotFoundError``; any other failure raises."""
        request = Request(
            f"{self.url.rstrip('/')}/tools/{route}",
            data=json.dumps(body).encode(),
            headers={"Authorization": f"Bearer {self.key}", "Content-Type": "application/json"},
            method="POST",
        )
        try:
            with urlopen(request, timeout=self.timeout) as response:
                return json.load(response)
        except HTTPError as error:
            if error.code == 404:
                raise NotFoundError(f"{route}: nothing found") from error
            raise


def _place(gid: str, name: str, rating: float, count: int, kind: str) -> dict[str, Any]:
    """Return one demo search result."""
    return {
        "place_id": gid,
        "name": name,
        "address": f"{name} street 1, 08002 Barcelona, Spain",
        "rating": rating,
        "user_rating_count": count,
        "primary_type_display_name": kind,
    }


WEEK = ("Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday")
AREAS = (
    (("gothic", "gotic", "gòtic"), (41.3833, 2.1777, "Barri Gòtic, Barcelona, Spain")),
    (("gràcia", "gracia"), (41.4036, 2.1561, "Gràcia, Barcelona, Spain")),
    (("born",), (41.3851, 2.1823, "El Born, Barcelona, Spain")),
    (
        ("sant joan 55", "sant joan, 55"),
        (41.3946, 2.1745, "Passeig de Sant Joan, 55, 08009 Barcelona, Spain"),
    ),
)
"""The demo's areas, by words a caller may use for them. Anything else is not found."""
TAPAS = [
    _place("g-canete", "Bar Cañete", 4.5, 3200, "Tapas Restaurant"),
    _place("g-gone", "El Xampanyet", 4.4, 800, "Bar"),
    _place("g-palma", "Bodega La Palma", 4.0, 1500, "Spanish Restaurant"),
    _place("g-paixano", "Can Paixano", 4.2, 9000, "Bar"),
]
COCKTAILS = [
    _place("g-paradiso", "Paradiso", 4.6, 12000, "Cocktail Bar"),
    _place("g-boadas", "Boadas", 4.3, 2400, "Cocktail Bar"),
]
NEARBY = [_place("g-snack", "Bar Snack 55", 4.0, 600, "Bar")]
DETAILS = {
    "g-canete": {
        "place_id": "g-canete",
        "name": "Bar Cañete",
        "address": "Carrer de la Unió, 17, Ciutat Vella, 08001 Barcelona, Spain",
        "rating": 4.5,
        "price_level": "PRICE_LEVEL_MODERATE",
        "opening_hours": [f"{day}: 1:00 PM \u2013 12:00 AM" for day in WEEK],
        "phone": "+34 932 70 34 58",
        "website": "https://barcanete.com",
        "primary_type_display_name": "Tapas Restaurant",
    },
    "g-palma": {
        "place_id": "g-palma",
        "name": "Bodega La Palma",
        "address": "Carrer de la Palma de Sant Just, 7, 08002 Barcelona, Spain",
        "rating": 4.0,
    },
    "g-paradiso": {
        "place_id": "g-paradiso",
        "name": "Paradiso",
        "address": "Carrer de Rera Palau, 4, 08003 Barcelona, Spain",
        "rating": 4.6,
        "price_level": "PRICE_LEVEL_EXPENSIVE",
        "opening_hours": [f"{day}: 7:00 PM \u2013 2:30 AM" for day in WEEK],
        "phone": "+34 933 60 72 22",
        "primary_type_display_name": "Cocktail Bar",
    },
    "g-snack": {
        "place_id": "g-snack",
        "name": "Bar Snack 55",
        "address": "Passeig de Sant Joan, 55, 08009 Barcelona, Spain",
        "rating": 4.0,
        "price_level": "PRICE_LEVEL_INEXPENSIVE",
        "opening_hours": [f"{day}: 8:00 AM \u2013 11:00 PM" for day in WEEK],
    },
}
"""Invented details for real-sounding places. A place missing here has no details."""


@dataclass
class DemoPlaces:
    """A few Barcelona places, answered the way the place service answers. Keeps every request."""

    sent: list[tuple[str, dict[str, Any]]] = field(default_factory=list)

    def post(self, route: str, body: dict[str, Any]) -> Any:
        """Return one route's answer from the demo data, or raise ``NotFoundError``."""
        self.sent.append((route, body))
        if route == "resolve-location":
            query = " ".join(body["query"].lower().split())
            found = next((at for names, at in AREAS if any(n in query for n in names)), None)
            if found is None:
                raise NotFoundError(route)
            latitude, longitude, address = found
            return {"latitude": latitude, "longitude": longitude, "formatted_address": address}
        if route == "search-text":
            query = body["query"].lower()
            if "sushi" in query:
                return []
            return COCKTAILS if "cocktail" in query else TAPAS
        if route == "search-nearby":
            return NEARBY
        if route == "place-details":
            if (answer := DETAILS.get(body["place_id"])) is None:
                raise NotFoundError(route)
            return answer
        raise ValueError(f"Nosh never calls {route}")

    def routes(self, name: str) -> list[dict[str, Any]]:
        """Return the bodies sent to one route, in order."""
        return [body for route, body in self.sent if route == name]


def places() -> Places:
    """Return the place service when ``TOOLS_API_KEY`` is set, else the demo places."""
    if key := env_value("TOOLS_API_KEY", str):
        return PlaceService(url=env_value("TOOLS_URL", str) or DEFAULT_URL, key=key)
    return DemoPlaces()


PRICES = {
    "PRICE_LEVEL_FREE": "free to get in",
    "PRICE_LEVEL_INEXPENSIVE": "budget-friendly",
    "PRICE_LEVEL_MODERATE": "mid-range",
    "PRICE_LEVEL_EXPENSIVE": "on the pricier side",
    "PRICE_LEVEL_VERY_EXPENSIVE": "quite upscale, fine-dining territory",
}
"""Google's price buckets in words. A bucket not listed here is not said."""
MANY_REVIEWS = 1000


def rating(value: float | None) -> str:
    """Say a rating as "a 4.5 out of five", or nothing when there is none."""
    return f"a {value:g} out of five" if value is not None else ""


def reviews(count: int | None) -> str:
    """Say "lots of reviews" for a well-known spot, the count otherwise, or nothing."""
    if not count:
        return ""
    return "lots of reviews" if count >= MANY_REVIEWS else f"{count} reviews"


def address(text: str) -> str:
    """Say an address as directions: street and number, then the area, without the country.

    A numeric postcode is dropped too: "Carrer de la Unió, 17, Ciutat Vella, Barcelona".
    """
    parts = text.split(", ")[:-1] or [text]
    kept = [
        " ".join(w for w in part.split() if not (w.isdigit() and len(w) >= 4)) for part in parts
    ]
    return ", ".join(part for part in kept if part)


def today(lines: list[str] | None, day: date) -> str:
    """Say today's hours from the weekday lines ("Monday: 9:00 AM to 1:00 AM", with a dash), or nothing."""
    line = next((line for line in lines or [] if line.startswith(f"{day:%A}:")), "")
    if not line:
        return ""
    hours = " ".join(line.split(":", 1)[1].replace("\u2013", " to ").split())
    if hours.lower() == "closed":
        return "They're closed today."
    if "open 24 hours" in hours.lower():
        return "They're open all day today."
    return f"Today they're open {hours}."


def kind(place: Place | PlaceInfo) -> str:
    """Say what a place is, like "a tapas restaurant", or nothing."""
    label = (place.primary_type_display_name or "").lower()
    return f"{'an' if label[:1] in 'aeiou' else 'a'} {label}" if label else ""


def short(place: Place) -> str:
    """Say one search result in a breath: name, kind, rating, reviews. Never price or hours."""
    said = [kind(place), rating(place.rating) and f"with {rating(place.rating)}"]
    if (many := reviews(place.user_rating_count)) and place.rating is not None:
        said.append(f"from {many}")
    return ", ".join([place.name, " ".join(s for s in said if s)]).rstrip(", ")


def listed(places: list[Place]) -> str:
    """Say up to three results: "A; B; and C"."""
    said = [short(place) for place in places[:3]]
    return said[0] if len(said) == 1 else f"{'; '.join(said[:-1])}; and {said[-1]}"


def describe(info: PlaceInfo, day: date) -> tuple[str, str, str]:
    """Return what code says about one place, with the price and today's hours in words.

    Returns:
        The spoken lines, the price in words, and today's hours in words.
    """
    price = PRICES.get(info.price_level or "", "")
    hours = today(info.opening_hours, day)
    about = ", and ".join(s for s in (rating(info.rating), price) if s)
    have = " and ".join(
        s for s, there in (("their number", info.phone), ("a website", info.website)) if there
    )
    spoken = " ".join(
        s
        for s in (
            f"{info.name} is on {address(info.address)}.",
            f"It's {about}." if about else "",
            hours,
            f"I've got {have} too, if you'd like them." if have else "",
            "Want to hear about another one, or is this the one?",
        )
        if s
    )
    return spoken, price, hours


TIMEZONE = "Europe/Madrid"
PLACES = places()
"""The place service when TOOLS_API_KEY is set, else the demo places."""
OFFER_AT_MOST = 6
"""How many places go on offer. The caller hears the first three."""
SEARCH = TypeAdapter(list[Place])


class FoundArea(BaseModel):
    """What ``find_area`` returns. A new area takes back the offer and the details.

    ``ask_craving`` is code's next step: with no craving and no exact address, the read-back
    asks what they are in the mood for, so the caller's yes never has to pick one.
    """

    found: bool
    spot: Spot | None = None
    formatted_address: str = ""
    ask_craving: bool = False
    craving: str | None = None
    offer: Offer | None = None
    details: Details | None = None


class FoundPlaces(BaseModel):
    """What ``find_places`` returns: the offer, or why there is none."""

    found: bool
    ask_craving: bool = False
    listed: str = ""
    craving: str | None = None
    offer: Offer | None = None
    details: Details | None = None


class FoundDetails(BaseModel):
    """What ``place_details`` returns: what code says about the place, or that it has none."""

    found: bool
    name: str
    spoken: str = ""
    details: Details | None = None


def madrid_today() -> date:
    """Return today on the Madrid clock, which "today's hours" are read for."""
    return datetime.now(ZoneInfo(TIMEZONE)).date()


def find_area(
    area: Annotated[
        str,
        Field(
            min_length=1,
            description=(
                "Where they want to eat or drink, as they said it: an address, a cross "
                "street, a landmark, a neighbourhood or a town, with the city if they named one."
            ),
        ),
    ],
    at_address: Annotated[
        bool,
        Field(description="True only for an exact street address or a point they are at."),
    ],
    craving: Annotated[
        str | None,
        Field(
            description=(
                'What they said they are in the mood for along with the area, like "tapas". '
                "Leave it out if they have not said."
            )
        ),
    ] = None,
) -> FoundArea:
    """Find where the caller wants to look. The application reads the place back to them."""
    try:
        located = Located.model_validate(PLACES.post("resolve-location", {"query": area}))
    except NotFoundError:
        return FoundArea(found=False)
    wanted = (craving or "").strip() or None
    spot = Spot(**located.model_dump(), at_address=at_address, craving=wanted)
    return FoundArea(
        found=True,
        spot=spot,
        formatted_address=located.formatted_address,
        ask_craving=wanted is None and not at_address,
        craving=wanted,
    )

def find_places(
    spot: Spot,
    craving: Annotated[
        str | None,
        Field(
            description=(
                'What they are in the mood for: a cuisine, "bar", "restaurant", "cocktail '
                'bar", a venue by name, or "no preference". Leave it out to search for what '
                "they said with the area."
            )
        ),
    ] = None,
) -> FoundPlaces:
    """Search places at the location the caller agreed to. The application says them."""
    wanted = (craving or "").strip() or spot.craving or ""
    anything = wanted.lower() in ("", "no preference")
    if not wanted and not spot.at_address:
        return FoundPlaces(found=False, ask_craving=True)
    point = {"latitude": spot.latitude, "longitude": spot.longitude}
    if spot.at_address and anything:
        raw = PLACES.post("search-nearby", point)
    else:
        area = address(spot.formatted_address) or spot.formatted_address
        query = f"{'restaurants and bars' if anything else wanted} in {area}"
        raw = PLACES.post("search-text", {"query": query, **point})
    found = SEARCH.validate_python(raw)[:OFFER_AT_MOST]
    if not found:
        return FoundPlaces(found=False, craving=wanted or None)
    offer = Offer(
        places=[
            Offered(id=f"p{index}", name=place.name, place_id=place.place_id)
            for index, place in enumerate(found, 1)
        ]
    )
    return FoundPlaces(
        found=True, listed=listed(found), craving=wanted or None, offer=offer
    )

def place_details(
    offer: Offer,
    place: Annotated[
        str, Field(description="The id of the place they picked from the offer, like p1.")
    ],
) -> FoundDetails:
    """Say the details of the place the caller picked: where it is, price, today's hours."""
    picked = next((p for p in offer.places if p.id == place.strip().lower()), None)
    if picked is None:
        # The model may only pick what the caller was offered.
        ids = ", ".join(f"{p.id} ({p.name})" for p in offer.places)
        raise ToolError(f"{place} was never offered. The offer is: {ids}.")
    try:
        info = PlaceInfo.model_validate(
            PLACES.post("place-details", {"place_id": picked.place_id})
        )
    except NotFoundError:
        return FoundDetails(found=False, name=picked.name)
    spoken, price, hours = describe(info, madrid_today())
    details = Details(
        place=picked.id,
        name=info.name,
        address=address(info.address),
        rating=info.rating,
        price=price,
        today=hours,
        phone=info.phone,
        website=info.website,
    )
    return FoundDetails(found=True, name=info.name, spoken=spoken, details=details)


class Ended(BaseModel):
    """What ``end_call`` returns."""

    ended: bool


def end_call() -> Ended:
    """End the call once the caller is done. Say nothing: the application says goodbye."""
    return Ended(ended=True)
