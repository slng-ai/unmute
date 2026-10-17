"""The salon's demo diary: customers, bookings and complaints, kept in memory.

Ported from unmute-core's examples/salon-concierge-unmute, which typed this example's
tools. The rules are the ones this file always had. One ``Salon`` holds the diary, and
each tool below is a function on it: agent.yaml names the function, and unmute-core reads
its types from the signature. ``customer_phone`` is never the model's: each tool's
``inject:`` locks it to the call state.
"""

import threading
from datetime import UTC, date, datetime
from datetime import time as TimeOfDay
from typing import Annotated, Any, Literal
from uuid import uuid4
from zoneinfo import ZoneInfo

from pydantic import BaseModel, Field
from unmute_core.state import Phone
from unmute_core.tools import spoken

from state import Appointment

SERVICES = ("haircut", "hair-color", "blowout")
TIMES = ("09:00", "11:30", "15:00")
TIMEZONE = "Europe/Madrid"
WEEKDAYS = ("Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday")

PhoneArgument = Annotated[
    Phone,
    Field(
        description=(
            "The caller's number in full international form, as they said it: a plus, the "
            "country code, then the number. Never infer a country code the caller did not say."
        )
    ),
]
DayArgument = Annotated[
    date | None,
    Field(
        description=(
            "The day the caller named. Leave it out when they named none: never pick a day "
            "for them, today included. Without a day, the caller's own bookings still come "
            "back, and code asks a caller who holds none which day they want."
        )
    ),
]
ServiceArgument = Annotated[
    Literal["any", "haircut", "hair-color", "blowout"],
    Field(
        description=(
            "Narrow the free times to one service, or `any` to see all three. Use `any` when "
            "the caller has not said which service they want."
        )
    ),
]
IntentArgument = Annotated[
    Literal["book", "move", "cancel"],
    Field(
        description=(
            "What the caller wants to do with the time: book a new one, move theirs, or "
            "cancel. It only changes the offer sentence that comes back."
        )
    ),
]
BookedServiceArgument = Annotated[
    Literal["haircut", "hair-color", "blowout"],
    Field(description="The service, exactly as find_slots returned it."),
]
BookedDayArgument = Annotated[
    date, Field(description="The day, YYYY-MM-DD, exactly as find_slots returned it.")
]
BookedTimeArgument = Annotated[
    TimeOfDay, Field(description="The time, HH:MM, exactly as find_slots returned it.")
]
BookingArgument = Annotated[
    str, Field(description="The exact booking_id find_slots returned for the caller's booking.")
]
AdditionalArgument = Annotated[
    bool,
    Field(
        description=(
            "True only when the caller asked for a second appointment on top of one they "
            "already hold. A caller who holds a booking is changing it, so this is false for "
            "an ordinary booking and the tool refuses with has_booking."
        )
    ),
]
SummaryArgument = Annotated[str, Field(description="Short factual summary of the complaint")]
ResolutionArgument = Annotated[
    str, Field(description="What the customer wants the salon to do, or an empty string")
]


def salon_today() -> date:
    """Return today's date on the salon's clock, not the container's UTC clock."""
    return datetime.now(ZoneInfo(TIMEZONE)).date()


def normalize_phone(phone: str) -> str:
    """Return the digits of a phone number, the store's one key.

    Every number reaching the diary is a ``Phone`` already: a tool argument the library
    validated, or the confirmed number in the call state.
    """
    return "".join(character for character in str(phone) if character.isdigit())


def slot_passed(day: date, time: str) -> bool:
    """Return True when a slot is earlier than the salon's clock reads now."""
    now = datetime.now(ZoneInfo(TIMEZONE))
    return day < now.date() or (day == now.date() and time <= now.strftime("%H:%M"))


def slot_parts(slot_id: str) -> tuple[str, str, str] | None:
    """Return a slot's date, service and time, or None if the diary would never offer it."""
    try:
        day, service, time = str(slot_id).split("|")
        requested = date.fromisoformat(day)
    except (TypeError, ValueError):
        return None
    if slot_passed(requested, time) or service not in SERVICES or time not in TIMES:
        return None
    return day, service, time


def offer(
    intent: str,
    day: str,
    slots: list[dict[str, str]],
    bookings: list[dict[str, str]],
    service: str = "",
) -> str:
    """Return the sentence the caller hears next, or "" when the model should answer.

    It covers the common cases only: the free times of one service to book, the free
    times to move the one booking the caller holds to, or a day with nothing free for the
    service asked for. Anything else is left to the model, which has the same result in
    front of it. A cancel is never offered here: the model calls ``cancel_appointment``,
    and the step asks the caller to confirm it.
    """
    if intent not in ("book", "move"):
        return ""
    if (intent == "book" and bookings) or (intent == "move" and len(bookings) != 1):
        return ""
    weekday = WEEKDAYS[date.fromisoformat(day).weekday()]
    if not slots and service:
        when = "today" if day == salon_today().isoformat() else f"on {weekday}"
        what = service.replace("-", " ")
        return f"I'm sorry, there's nothing free for a {what} {when}. Would another day work?"
    services = {slot["service"] for slot in slots}
    if len(services) != 1:
        return ""
    times = [str(spoken(slot["time"], "time", "en")) for slot in slots]
    listed = times[0] if len(times) == 1 else ", ".join(times[:-1]) + " or " + times[-1]
    if intent == "move":
        return f"I can move it to {weekday} at {listed}. Which works best for you?"
    what = services.pop().replace("-", " ")
    return f"For a {what} on {weekday}, I have {listed}. Which works best for you?"


def slot_key(day: date, service: str, hour: TimeOfDay) -> str:
    """Return the diary's key for one slot: ``2026-10-16|haircut|09:00``."""
    return f"{day.isoformat()}|{service}|{hour.strftime('%H:%M')}"


def appointment(booking_id: str, service: str, slot_id: str, action: str) -> dict[str, str]:
    """Return the saved shape of one successful booking action, complete.

    A receipt line says its service, day and time from it, as code speaks them: "your hair
    color is booked for tomorrow, October 9 at 9:00 AM".
    """
    parts = slot_parts(slot_id)
    day, time = (parts[0], parts[2]) if parts else ("", "")
    return {
        "booking_id": booking_id,
        "service": service,
        "date": day,
        "time": time,
        "action": action,
    }


def refusal(status: str, summary: str, booking_id: str = "") -> dict[str, Any]:
    """Return a refused booking result."""
    return {"status": status, "booking_id": booking_id, "summary": summary}


Service = Literal["haircut", "hair-color", "blowout"]


class Customer(BaseModel):
    """What ``find_or_create_customer`` returns."""

    status: Literal["existing", "created"]
    customer_phone: Phone
    summary: str


class Held(BaseModel):
    """One of the caller's active bookings."""

    booking_id: str
    service: Service
    date: date
    time: TimeOfDay


class Slot(BaseModel):
    """One free time in the diary."""

    slot_id: str
    service: Service
    date: date
    time: TimeOfDay


class Diary(BaseModel):
    """What ``find_slots`` returns: the caller's bookings, the free times, and the offer."""

    bookings: list[Held]
    slots: list[Slot]
    status: Literal["need_date", "ok", "invalid_service", "invalid_date", "full"]
    offer: str = ""
    offered: bool = False


class BookingAction(BaseModel):
    """What a booking, a move or a cancellation returns, or why it was refused."""

    status: Literal[
        "booked", "moved", "cancelled", "not_found", "invalid", "has_booking", "slot_taken"
    ]
    summary: str
    booking_id: str = ""
    service: Service | None = None
    time: TimeOfDay | None = None
    appointment: Appointment | None = None
    existing: list[Held] = []
    date: Annotated[date | None, Field(default=None)]


class Complaint(BaseModel):
    """One recorded complaint."""

    complaint_id: str
    summary: str
    requested_resolution: str


class ComplaintRecord(BaseModel):
    """What ``record_complaint`` returns."""

    complaint_id: str
    status: Literal["recorded", "invalid", "customer_not_found"]
    complaint: Complaint | None = None


class Salon:
    """One salon's diary, shared by every call in the process."""

    def __init__(self) -> None:
        """Start with no customers, bookings or complaints."""
        self.customers: set[str] = set()
        self.bookings: dict[str, dict[str, str]] = {}
        self.complaints: dict[str, dict[str, str]] = {}
        self.lock = threading.Lock()

    def find_or_create_customer(self, phone: PhoneArgument) -> dict[str, str]:
        """Return the customer for a phone number: existing or created.

        The step asks the caller to confirm the number before this runs.
        """
        digits = normalize_phone(phone)
        with self.lock:
            known = digits in self.customers
            self.customers.add(digits)
        return {
            "status": "existing" if known else "created",
            "customer_phone": phone,
            "summary": "The existing customer was verified."
            if known
            else "A new customer was verified and created.",
        }

    def find_slots(
        self,
        customer_phone: str,
        date: DayArgument = None,
        service: ServiceArgument = "any",
        intent: IntentArgument = "book",
    ) -> dict[str, Any]:
        """Return the caller's bookings, the free slots on ``date``, and the offer to say.

        With no date and no booking the status is ``need_date``, and the offer asks which
        day. ``offer`` is the sentence to say, or "" when there is none. ``offered`` is True
        when there is one: code then says it, and the model does not.
        """
        caller = normalize_phone(customer_phone)
        wanted = "" if service == "any" else service
        with self.lock:
            bookings = self._held_by(caller)
        if date is None:
            if not bookings:
                what = f"your {wanted.replace('-', ' ')}" if wanted else "to come in"
                ask = f"Sure, which day would you like {what}?"
                return {
                    "bookings": [],
                    "slots": [],
                    "status": "need_date",
                    "offer": ask,
                    "offered": True,
                }
            return {"bookings": bookings, "slots": [], "status": "ok"}
        if wanted and wanted not in SERVICES:
            return {"bookings": bookings, "slots": [], "status": "invalid_service"}
        if date < salon_today():
            return {"bookings": bookings, "slots": [], "status": "invalid_date"}
        day = date.isoformat()
        with self.lock:
            used = {b["slot_id"] for b in self.bookings.values() if b["status"] == "booked"}
        slots = [
            {"slot_id": f"{day}|{each}|{time}", "service": each, "date": day, "time": time}
            for each in ((wanted,) if wanted else SERVICES)
            for time in TIMES
            if f"{day}|{each}|{time}" not in used and not slot_passed(date, time)
        ]
        said = offer(intent, day, slots, bookings, wanted)
        return {
            "bookings": bookings,
            "slots": slots,
            "status": "ok" if slots else "full",
            "offer": said,
            "offered": bool(said),
        }

    def book_appointment(
        self,
        customer_phone: str,
        service: BookedServiceArgument,
        date: BookedDayArgument,
        time: BookedTimeArgument,
        additional: AdditionalArgument,
    ) -> dict[str, Any]:
        """Book a free slot. The step asks the caller to confirm it before this runs."""
        caller = normalize_phone(customer_phone)
        return self._book(caller, slot_key(date, service, time), additional)

    def move_appointment(
        self,
        customer_phone: str,
        booking_id: BookingArgument,
        date: BookedDayArgument,
        time: BookedTimeArgument,
    ) -> dict[str, Any]:
        """Move the caller's booking to a free time. The step asks the caller to confirm it."""
        with self.lock:
            booking = self.bookings.get(booking_id)
        if booking is None:
            return refusal("not_found", "No active matching booking was found.", booking_id)
        target = slot_key(date, booking["service"], time)
        return self._move(normalize_phone(customer_phone), booking_id, target)

    def cancel_appointment(
        self,
        customer_phone: str,
        booking_id: BookingArgument,
        service: BookedServiceArgument,
        date: BookedDayArgument,
        time: BookedTimeArgument,
    ) -> dict[str, Any]:
        """Cancel one of the caller's bookings. The step asks the caller to confirm it.

        The service, day and time are what the caller hears, so they must be the booking's.
        """
        with self.lock:
            booking = self.bookings.get(booking_id)
        if booking is None or booking["slot_id"] != slot_key(date, service, time):
            return refusal(
                "not_found",
                "That booking is not on that service, day and time. Use find_slots again.",
                booking_id,
            )
        return self._cancel(normalize_phone(customer_phone), booking_id)

    def record_complaint(
        self,
        customer_phone: str,
        summary: SummaryArgument,
        requested_resolution: ResolutionArgument,
    ) -> dict[str, Any]:
        """Save a verified customer's complaint."""
        clean_summary = " ".join(str(summary).split())
        if not clean_summary:
            return {"complaint_id": "", "status": "invalid"}
        caller = normalize_phone(customer_phone)
        clean_resolution = " ".join(str(requested_resolution).split())
        with self.lock:
            if caller not in self.customers:
                return {"complaint_id": "", "status": "customer_not_found"}
            complaint_id = f"cmp_{uuid4().hex[:12]}"
            self.complaints[complaint_id] = {
                "customer_phone": caller,
                "summary": clean_summary,
                "requested_resolution": clean_resolution,
                "created_at": datetime.now(UTC).isoformat(),
            }
        return {
            "complaint_id": complaint_id,
            "status": "recorded",
            "complaint": {
                "complaint_id": complaint_id,
                "summary": clean_summary,
                "requested_resolution": clean_resolution,
            },
        }

    def _slot_taken(self, slot_id: str) -> bool:
        """Return True when an active booking holds the slot. Call under the lock."""
        return any(
            b["slot_id"] == slot_id and b["status"] == "booked" for b in self.bookings.values()
        )

    def _held_by(self, caller: str) -> list[dict[str, str]]:
        """Return the caller's active bookings, soonest first. Call under the lock."""
        rows = [
            {
                "booking_id": booking_id,
                "service": booking["service"],
                "date": booking["slot_id"].split("|")[0],
                "time": booking["slot_id"].split("|")[2],
            }
            for booking_id, booking in self.bookings.items()
            if booking["customer_phone"] == caller and booking["status"] == "booked"
        ]
        return sorted(rows, key=lambda row: (row["date"], row["time"]))

    def _book(self, caller: str, slot_id: str, additional: bool) -> dict[str, Any]:
        """Book a new slot. A second booking needs ``additional``."""
        parts = slot_parts(slot_id)
        if not parts:
            return refusal("invalid", "That slot is not one the diary offered.")
        service = parts[1]
        new_id = f"bkg_{uuid4().hex[:12]}"
        timestamp = datetime.now(UTC).isoformat()
        with self.lock:
            if caller not in self.customers:
                return refusal("not_found", "The customer is not verified.")
            held = self._held_by(caller)
            if held and additional is not True:
                refused = refusal(
                    "has_booking",
                    "The customer already has a booking. Move it, or pass additional true "
                    "for a second appointment.",
                )
                refused["existing"] = held
                return refused
            if self._slot_taken(slot_id):
                return refusal("slot_taken", "That time was just taken.")
            self.bookings[new_id] = {
                "customer_phone": caller,
                "service": service,
                "slot_id": slot_id,
                "status": "booked",
                "created_at": timestamp,
                "updated_at": timestamp,
            }
        saved = appointment(new_id, service, slot_id, "book")
        return {
            "status": "booked",
            "booking_id": new_id,
            "service": service,
            "date": saved["date"],
            "time": saved["time"],
            "appointment": saved,
            "summary": "Booking saved.",
        }

    def _move(self, caller: str, booking_id: str, slot_id: str) -> dict[str, Any]:
        """Move the caller's active booking to another free slot."""
        parts = slot_parts(slot_id)
        if not parts:
            return refusal("invalid", "That slot is not one the diary offered.", booking_id)
        service = parts[1]
        with self.lock:
            booking = self.bookings.get(booking_id)
            if (
                booking is None
                or booking["customer_phone"] != caller
                or booking["status"] != "booked"
            ):
                return refusal("not_found", "No active matching booking was found.", booking_id)
            if slot_id != booking["slot_id"] and self._slot_taken(slot_id):
                return refusal("slot_taken", "That time was just taken.", booking_id)
            booking.update(
                service=service, slot_id=slot_id, updated_at=datetime.now(UTC).isoformat()
            )
        saved = appointment(booking_id, service, slot_id, "move")
        return {
            "status": "moved",
            "booking_id": booking_id,
            "service": service,
            "date": saved["date"],
            "time": saved["time"],
            "appointment": saved,
            "summary": "Booking moved.",
        }

    def _cancel(self, caller: str, booking_id: str) -> dict[str, Any]:
        """Cancel one of the caller's bookings."""
        with self.lock:
            booking = self.bookings.get(booking_id)
            if booking is None or booking["customer_phone"] != caller:
                return refusal("not_found", "No matching booking was found.", booking_id)
            if booking["status"] == "cancelled":
                return refusal("not_found", "That booking was already cancelled.", booking_id)
            booking.update(status="cancelled", updated_at=datetime.now(UTC).isoformat())
            saved = appointment(booking_id, booking["service"], booking["slot_id"], "cancel")
        return {
            "status": "cancelled",
            "booking_id": booking_id,
            "service": saved["service"],
            "date": saved["date"],
            "time": saved["time"],
            "appointment": saved,
            "summary": "Booking cancelled.",
        }


SALON = Salon()
"""The diary every call in this process books into."""


def find_or_create_customer(phone: PhoneArgument) -> Customer:
    """Return the customer for a phone number: existing or created."""
    return Customer.model_validate(SALON.find_or_create_customer(phone))


def find_slots(
    customer_phone: str,
    date: DayArgument = None,
    service: ServiceArgument = "any",
    intent: IntentArgument = "book",
) -> Diary:
    """Return the caller's bookings, the free slots on ``date``, and the offer to say."""
    return Diary.model_validate(SALON.find_slots(customer_phone, date, service, intent))


def book_appointment(
    customer_phone: str,
    service: BookedServiceArgument,
    date: BookedDayArgument,
    time: BookedTimeArgument,
    additional: AdditionalArgument = False,
) -> BookingAction:
    """Book a free time for the caller."""
    return BookingAction.model_validate(
        SALON.book_appointment(customer_phone, service, date, time, additional)
    )


def move_appointment(
    customer_phone: str,
    booking_id: BookingArgument,
    date: BookedDayArgument,
    time: BookedTimeArgument,
) -> BookingAction:
    """Move the caller's booking to a free time."""
    return BookingAction.model_validate(
        SALON.move_appointment(customer_phone, booking_id, date, time)
    )


def cancel_appointment(
    customer_phone: str,
    booking_id: BookingArgument,
    service: BookedServiceArgument,
    date: BookedDayArgument,
    time: BookedTimeArgument,
) -> BookingAction:
    """Cancel the caller's booking."""
    return BookingAction.model_validate(
        SALON.cancel_appointment(customer_phone, booking_id, service, date, time)
    )


def record_complaint(
    customer_phone: str,
    summary: SummaryArgument,
    requested_resolution: ResolutionArgument,
) -> ComplaintRecord:
    """Record the verified customer's complaint."""
    return ComplaintRecord.model_validate(
        SALON.record_complaint(customer_phone, summary, requested_resolution)
    )
