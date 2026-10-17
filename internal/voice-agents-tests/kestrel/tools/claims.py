"""Kestrel's claims desk: the tools the agent calls, as typed Python functions.

Ported from unmute-core's examples/kestrel. The desk stands in for the insurer's
backend: two invented policies, and the claims filed against them, in memory.

- ``lookup_policy`` finds a policy by its number and the holder's date of birth.
- ``note_facts`` merges what the caller just said into the report, and returns the next
  question in code's words, or the whole claim read back once every fact is in.
- ``file_claim`` files the report once per call.

Nothing the caller hears from these tools is worded by the model: each result carries the
line code says, or a typed ``status`` a reply in the package turns into a line.
"""

from datetime import date, datetime, time
from threading import Lock
from typing import Annotated, Any, Literal
from zoneinfo import ZoneInfo

from pydantic import BaseModel, Field
from state import DayPart, Policy, PolicyNumber, Report, Vehicle, is_policy_number
from unmute_core.state import BirthDate, Spelled
from unmute_core.tools import ToolError

ZONE = ZoneInfo("Europe/London")
QUESTIONS = (
    ("incident_day", "I can take your claim now. On which day did it happen?"),
    ("incident_time", "Roughly what time was it?"),
    ("place", "Where did it happen?"),
    ("what_happened", "In your own words, what happened?"),
    ("injuries", "Was anyone hurt?"),
    ("police_report", "Was it reported to the police? If so, do you have a reference?"),
    ("other_party", "Was another vehicle or driver involved?"),
    ("vehicle_drivable", "Can your car still be driven?"),
)
"""The incident's facts in the order code asks for them, each with its question."""
SAFETY = (
    "I'm sorry to hear that. If anyone needs medical help, please call the emergency "
    "services first."
)
POLICIES: dict[str, dict[str, Any]] = {
    "KP482913": {
        "policy_number": "KP482913",
        "first_name": "Daniel",
        "birth_date": date(1979, 6, 5),
        "vehicles": [{"plate": "AB12CDE", "words": "the blue Toyota Corolla"}],
    },
    "KP731508": {
        "policy_number": "KP731508",
        "first_name": "Priya",
        "birth_date": date(1992, 1, 23),
        "vehicles": [
            {"plate": "LM70XYZ", "words": "the white Kia Niro"},
            {"plate": "GH19RST", "words": "the black Ford Fiesta"},
        ],
    },
}
"""Invented people and cars. Any resemblance to a real one is a coincidence."""
FILED: dict[str, dict[str, Any]] = {}
"""Every claim filed, by call."""
LOCK = Lock()
COVERAGE = """\
- A claims handler calls within one working day of a claim being filed, and decides what
  the policy covers. Never promise cover yourself.
- Comprehensive policies cover damage to the caller's own car, whoever was at fault, less
  the excess. Third-party policies cover only damage to others.
- The excess is on the policy schedule. The handler confirms it.
- A courtesy car is offered while the car is repaired at a Kestrel approved garage, if
  the policy includes one.
- A car that cannot be driven can be recovered by Kestrel's recovery partner: the handler
  arranges it.
- Photos of the damage and the scene help: the handler asks for them.
"""

IncidentDay = Annotated[
    date | None,
    Field(
        description=(
            "The day it happened, as YYYY-MM-DD. Work it out from today's date. Pass it even "
            "when it is in the future: the application checks it."
        )
    ),
]


class PolicyFound(BaseModel):
    """What ``lookup_policy`` returns.

    Attributes:
        status: ``looked_up``, or ``not_a_number`` when the number is not KP and six
            digits. Then nothing is looked up or read back, and the tool's reply asks again.
    """

    found: bool
    status: Literal["looked_up", "not_a_number"] = "looked_up"
    policy: Policy | None = None
    first_name: str = ""
    hint: str = ""


class Noted(BaseModel):
    """What ``note_facts`` returns: the report so far, and what code says next.

    Attributes:
        report: Every fact so far, the new ones merged in.
        status: ``noted``, or ``future_day`` when the day given has not happened yet. That
            day is left out of the report, and the tool's reply asks for it again.
        complete: True once every fact and the car are known.
        next_question: The question for the first missing fact, while one is missing.
        read_back: The whole claim in code's words, once it is complete.
    """

    report: Report
    status: Literal["noted", "future_day"] = "noted"
    complete: bool
    next_question: str = ""
    read_back: str | None = None


class Filed(BaseModel):
    """What ``file_claim`` returns. The claim number is said a character at a time."""

    filed: bool
    claim_number: Spelled


class Guide(BaseModel):
    """What ``coverage_guide`` returns."""

    guide: str


class Ended(BaseModel):
    """What ``end_call`` returns."""

    ended: bool


def today() -> date:
    """Return today's date on the claims desk's clock."""
    return datetime.now(ZONE).date()


def ordinal(n: int) -> str:
    """Say 1 as "1st", 22 as "22nd" and 13 as "13th"."""
    suffix = "th" if 10 <= n % 100 <= 20 else {1: "st", 2: "nd", 3: "rd"}.get(n % 10, "th")
    return f"{n}{suffix}"


def spoken_day(value: date) -> str:
    """Say a day as "Monday the 28th of September"."""
    return f"{value:%A} the {ordinal(value.day)} of {value:%B}"


def spoken_when(value: time | str) -> str:
    """Say a time as "at 6:30 PM", or a part of the day as "in the evening" or "at night"."""
    if isinstance(value, str):
        return "at night" if value == "night" else f"in the {value}"
    hour = value.hour % 12 or 12
    minutes = f":{value.minute:02d}" if value.minute else ""
    return f"at {hour}{minutes} {'AM' if value.hour < 12 else 'PM'}"


def car_for(said: str, vehicles: list[Vehicle]) -> Vehicle | None:
    """Return the one car a caller named, by its plate or by a word of its make or model."""
    key = "".join(said.split()).upper()
    words = set(said.lower().split()) - {"the", "my", "car", "a"}
    named = [
        vehicle
        for vehicle in vehicles
        if key == vehicle.plate or words & (set(vehicle.words.lower().split()) - {"the"})
    ]
    return named[0] if len(named) == 1 else None


def read_back(report: Report, vehicles: list[Vehicle]) -> str:
    """Return the whole claim in code's words, ending in the question the caller answers."""
    assert report.incident_day is not None and report.incident_time is not None
    car = next((v.words for v in vehicles if v.plate == report.vehicle), "your car")
    place = (report.place or "").strip().rstrip(".")
    happened = (report.what_happened or "").strip().rstrip(".")
    police = (report.police_report or "").strip()
    reported = "no police report" if police.lower() in ("", "none") else f"police report {police}"
    return " ".join(
        [
            f"Shall I file your claim for {car}?",
            f"On {spoken_day(report.incident_day)} {spoken_when(report.incident_time)},",
            f"at {place}: {happened}.",
            "Someone was hurt," if report.injuries else "Nobody was hurt,",
            f"{reported},",
            "another driver was involved,"
            if report.other_party
            else "no other driver was involved,",
            f"and the car {'can' if report.vehicle_drivable else 'cannot'} be driven.",
            "Is that all right?",
        ]
    )


def next_step(report: Report, vehicles: list[Vehicle], newly_hurt: bool) -> Noted:
    """Return what code says next: the first missing fact's question, or that it is complete."""
    question = next((ask for name, ask in QUESTIONS if getattr(report, name) is None), None)
    if question is None and report.vehicle is None:
        question = f"Which car was it: {' or '.join(v.words for v in vehicles)}?"
    if question is not None:
        said = f"{SAFETY} {question}" if newly_hurt else question
        return Noted(report=report, complete=False, next_question=said)
    return Noted(report=report, complete=True)


def not_a_policy_number(policy_number: PolicyNumber) -> PolicyFound | None:
    """Refuse a number that is not KP and six digits, before it is read back for a yes."""
    if is_policy_number(policy_number):
        return None
    return PolicyFound(found=False, status="not_a_number")


def lookup_policy(policy_number: PolicyNumber, birth_date: BirthDate) -> PolicyFound:
    """Find the caller's policy by its number and the policy holder's date of birth.

    Call it as soon as you have both. It first reads both back to the caller, so never
    read them back yourself. Call it again with the same values after their yes.
    """
    found = POLICIES.get(policy_number)
    if found is None or found["birth_date"] != birth_date:
        return PolicyFound(
            found=False,
            hint=(
                "No policy matches that number and date of birth. Reveal nothing about "
                "any policy."
            ),
        )
    policy = Policy.model_validate(found)
    return PolicyFound(found=True, policy=policy, first_name=policy.first_name)


def note_facts(
    report: Report,
    policy: Policy,
    incident_day: IncidentDay = None,
    incident_time: Annotated[
        time | DayPart | None,
        Field(description="HH:MM on a 24-hour clock, or morning, afternoon, evening, night."),
    ] = None,
    place: Annotated[
        str | None, Field(description="Where it happened, in the caller's own words.")
    ] = None,
    what_happened: Annotated[
        str | None,
        Field(description="What happened, in the caller's own words: the shortest excerpt."),
    ] = None,
    injuries: Annotated[bool | None, Field(description="Whether anyone was hurt.")] = None,
    police_report: Annotated[
        str | None,
        Field(description='The police reference they gave, or "none" for no report.'),
    ] = None,
    other_party: Annotated[
        bool | None, Field(description="Whether another vehicle or driver was involved.")
    ] = None,
    vehicle_drivable: Annotated[
        bool | None, Field(description="Whether their car can still be driven.")
    ] = None,
    vehicle: Annotated[
        str | None, Field(description="The car it happened to: its plate or its name.")
    ] = None,
) -> Noted:
    """Note every fact of the incident the caller just gave, all in one call.

    Pass only what they said. The application asks the next question itself. When it
    answers complete, call file_claim at once.
    """
    known = report
    future = incident_day is not None and incident_day > today()
    given: dict[str, object] = {
        "incident_day": None if future else incident_day,
        "incident_time": incident_time,
        "place": place,
        "what_happened": what_happened,
        "injuries": injuries,
        "police_report": police_report,
        "other_party": other_party,
        "vehicle_drivable": vehicle_drivable,
    }
    if vehicle is not None and (car := car_for(vehicle, policy.vehicles)) is not None:
        given["vehicle"] = car.plate
    newly_hurt = injuries is True and known.injuries is not True
    report = known.model_copy(
        update={name: value for name, value in given.items() if value is not None}
    )
    if report.vehicle is None and len(policy.vehicles) == 1:
        report.vehicle = policy.vehicles[0].plate
    noted = next_step(report, policy.vehicles, newly_hurt)
    if noted.complete:
        noted.read_back = read_back(report, policy.vehicles)
    if future:
        noted.status = "future_day"
    return noted


def file_claim(report: Report, policy_number: str, call: str) -> Filed:
    """File the claim exactly as it was read back. Call it once the report is complete.

    It first reads the whole claim back to the caller, so never read it back yourself.
    Call it again after their yes.
    """
    if not next_step(report, [], False).complete:
        raise ToolError("the report is not complete yet: note the missing facts first")
    with LOCK:
        if call not in FILED:
            number = f"KC{100231 + len(FILED)}"
            FILED[call] = {
                "claim_number": number,
                "policy_number": policy_number,
                **report.model_dump(mode="json"),
            }
        number = FILED[call]["claim_number"]
    return Filed(filed=True, claim_number=number)


def coverage_guide() -> Guide:
    """Read the coverage guide: cover, the excess, a courtesy car, recovery, what happens next.

    Call it only when the caller asks about one of these, and answer from it.
    """
    return Guide(guide=COVERAGE)


def end_call() -> Ended:
    """End the call once the caller says they are done. Say your goodbye in the same reply."""
    return Ended(ended=True)
