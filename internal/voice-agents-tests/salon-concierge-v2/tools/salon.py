"""Salon demo tools, backed by one in-process store.

The compiler copies this file once per tool (`tools/<tool_name>.py`), so a
module-level dict would give each tool its own private store and the eight
copies would never see each other's writes. One module parked in `sys.modules`
under a name nothing else uses is the smallest thing every copy can reach.
"""

import sys
import threading
import types
from datetime import UTC, date, datetime, timedelta
from uuid import uuid4
from zoneinfo import ZoneInfo

_fresh = types.ModuleType("unmute_salon_state")
_fresh.customers = set()
_fresh.names = {"34111111111": "Robin Vega"}
_fresh.bookings = {}
_fresh.complaints = {}
_fresh.lock = threading.Lock()
_state = sys.modules.setdefault("unmute_salon_state", _fresh)

_SERVICES = {"haircut", "hair-color", "blowout"}
_TIMES = ("09:00", "11:30", "15:00")
_SALON_TIMEZONE = "Europe/Madrid"


def _now():
    return datetime.now(UTC).isoformat()


def _booking_today() -> date:
    """Return the calendar date used by booking validation, in the salon's zone.

    Not `date.today()`, which reads the container clock. That clock is UTC, so a
    booking taken at 23:30 in Madrid landed on the following day and every date
    check here disagreed with the caller by one. The zone matches `timezone:` in
    agent.yaml, which is also what the pre-fetched {{booking_date}} is read in, so
    the prompt and the validation agree about what day it is.
    """
    return datetime.now(ZoneInfo(_SALON_TIMEZONE)).date()


def _normalize_phone(phone):
    """Digits only, and the store's one key.

    Every function here normalises its own argument rather than trusting the
    caller to. The phone number is the customer identifier, and a caller says it
    in whatever shape they like, so "+1 555 070 7444" and "1 (555) 070-7444"
    have to reach the same record.
    """
    digits = "".join(character for character in str(phone) if character.isdigit())
    return digits if 10 <= len(digits) <= 15 else ""


def _e164(digits):
    """The one shape a phone number takes anywhere in this package.

    E.164: a plus sign, then digits, and nothing else. `MANAGER_PHONE_NUMBER`
    already holds that shape, so this makes every number in the package one
    format instead of two, and `tasks/verify-customer.md` says the same thing on
    the prompt side. The tool returns it in that shape so the model copies it
    rather than inventing a second one.

    It never splits a country code off, and never supplies one. Guessing took the
    last ten digits for the local number, so `+34 111 111 111` came back as
    "3 411 111 1111": a lone "3" standing in for a country code that is really
    34, and a caller hearing their own number read as somebody else's.
    """
    return "+" + digits


def _slot_parts(slot_id):
    try:
        date_text, service, time = str(slot_id).split("|")
        requested_date = date.fromisoformat(date_text)
    except (TypeError, ValueError):
        return None
    if requested_date < _booking_today() or service not in _SERVICES or time not in _TIMES:
        return None
    return date_text, service, time


def _slot_taken(slot_id) -> bool:
    """True when an active booking already holds this slot. Call under the lock."""
    return any(
        booking["slot_id"] == slot_id and booking["status"] == "booked"
        for booking in _state.bookings.values()
    )


def find_or_create_customer(phone):
    normalized_phone = _normalize_phone(phone)
    if not normalized_phone:
        return {
            "customer_phone": "",
            "status": "invalid",
            "summary": "A valid phone number of 10 to 15 digits is required.",
        }
    with _state.lock:
        known = normalized_phone in _state.customers
        _state.customers.add(normalized_phone)
    return {
        "customer_phone": _e164(normalized_phone),
        "status": "existing" if known else "created",
        "summary": (
            "The existing customer was verified."
            if known
            else "A new customer was verified and created."
        ),
    }


def list_bookings(customer_phone):
    caller = _normalize_phone(customer_phone)
    with _state.lock:
        rows = [
            {
                "booking_id": booking_id,
                "service": booking["service"],
                "start_time": booking["start_time"],
                "status": booking["status"],
            }
            for booking_id, booking in _state.bookings.items()
            if booking["customer_phone"] == caller and booking["status"] == "booked"
        ]
    return {"bookings": sorted(rows, key=lambda row: row["start_time"])}


def look_up_customer(phone):
    """Read the record a number belongs to. Writes nothing.

    This is the whole difference from find_or_create_customer, and the reason both
    exist: a prefetch runs unasked on every inbound call, so a tool that writes
    would create a customer record for every wrong number that ever rang. Nothing
    below mutates _state, and that is what `read_only: true` in the tool file is
    promising.

    An unknown number is not an error. It returns an empty name, the prefetch
    assigns that empty value, and the verification step asks for a number exactly
    as it did before any of this existed.
    """
    normalized_phone = _normalize_phone(phone)
    if not normalized_phone:
        return {"name": "", "status": "unknown"}
    with _state.lock:
        name = _state.names.get(normalized_phone, "")
        known = normalized_phone in _state.customers or bool(name)
    return {"name": name, "status": "existing" if known else "unknown"}


def check_availability(service, date):
    if service not in _SERVICES:
        return {"slots": [], "status": "invalid"}
    try:
        requested_date = datetime.strptime(date, "%Y-%m-%d").date()
    except (TypeError, ValueError):
        return {"slots": [], "status": "invalid"}
    if requested_date < _booking_today():
        return {"slots": [], "status": "invalid"}

    with _state.lock:
        used = {
            booking["slot_id"]
            for booking in _state.bookings.values()
            if booking["status"] == "booked"
        }
    slots = [
        {"slot_id": f"{date}|{service}|{time}", "start_time": f"{date}T{time}:00"}
        for time in _TIMES
        if f"{date}|{service}|{time}" not in used
    ]
    return {"slots": slots, "status": "available" if slots else "full"}


def create_booking(customer_phone, service, slot_id, confirmed=False):
    if confirmed is not True:
        return {
            "booking_id": "",
            "status": "not_confirmed",
            "summary": "The booking change was not confirmed.",
        }
    parts = _slot_parts(slot_id)
    if not parts or parts[1] != service:
        return {"booking_id": "", "status": "invalid", "summary": "Invalid slot."}
    booking_id = f"bkg_{uuid4().hex[:12]}"
    timestamp = _now()
    caller = _normalize_phone(customer_phone)
    with _state.lock:
        if caller not in _state.customers:
            return {
                "booking_id": "",
                "status": "customer_not_found",
                "summary": "The customer is not verified.",
            }
        if _slot_taken(slot_id):
            return {
                "booking_id": "",
                "status": "slot_unavailable",
                "summary": "That time was just taken.",
            }
        _state.bookings[booking_id] = {
            "customer_phone": caller,
            "service": service,
            "slot_id": slot_id,
            "start_time": f"{parts[0]}T{parts[2]}:00",
            "status": "booked",
            "created_at": timestamp,
            "updated_at": timestamp,
        }
    return {"booking_id": booking_id, "status": "booked", "summary": "Booking saved."}


def modify_booking(customer_phone, booking_id, service, slot_id, confirmed=False):
    if confirmed is not True:
        return {
            "booking_id": booking_id,
            "status": "not_confirmed",
            "summary": "The booking change was not confirmed.",
        }
    parts = _slot_parts(slot_id)
    if not parts or parts[1] != service:
        return {"booking_id": booking_id, "status": "invalid", "summary": "Invalid slot."}
    caller = _normalize_phone(customer_phone)
    with _state.lock:
        booking = _state.bookings.get(booking_id)
        if (
            booking is None
            or booking["customer_phone"] != caller
            or booking["status"] != "booked"
        ):
            return {
                "booking_id": booking_id,
                "status": "not_found",
                "summary": "No active matching booking was found.",
            }
        if slot_id != booking["slot_id"] and _slot_taken(slot_id):
            return {
                "booking_id": booking_id,
                "status": "slot_unavailable",
                "summary": "That time was just taken.",
            }
        booking.update(
            service=service,
            slot_id=slot_id,
            start_time=f"{parts[0]}T{parts[2]}:00",
            updated_at=_now(),
        )
    return {"booking_id": booking_id, "status": "modified", "summary": "Booking updated."}


def cancel_booking(customer_phone, booking_id, confirmed=False):
    if confirmed is not True:
        return {
            "booking_id": booking_id,
            "status": "not_confirmed",
            "summary": "The booking change was not confirmed.",
        }
    caller = _normalize_phone(customer_phone)
    with _state.lock:
        booking = _state.bookings.get(booking_id)
        if booking is None or booking["customer_phone"] != caller:
            return {
                "booking_id": booking_id,
                "status": "not_found",
                "summary": "No matching booking was found.",
            }
        if booking["status"] == "cancelled":
            return {
                "booking_id": booking_id,
                "status": "already_cancelled",
                "summary": "The booking was already cancelled.",
            }
        booking.update(status="cancelled", updated_at=_now())
    return {"booking_id": booking_id, "status": "cancelled", "summary": "Booking cancelled."}


def record_complaint(customer_phone, summary, requested_resolution=""):
    clean_summary = " ".join(str(summary).split())
    if not clean_summary:
        return {"complaint_id": "", "status": "invalid"}
    caller = _normalize_phone(customer_phone)
    with _state.lock:
        if caller not in _state.customers:
            return {"complaint_id": "", "status": "customer_not_found"}
        complaint_id = f"cmp_{uuid4().hex[:12]}"
        _state.complaints[complaint_id] = {
            "customer_phone": caller,
            "summary": clean_summary,
            "requested_resolution": " ".join(str(requested_resolution).split()),
            "created_at": _now(),
        }
    return {"complaint_id": complaint_id, "status": "recorded"}


def _demo():
    import importlib.util
    from concurrent.futures import ThreadPoolExecutor

    for phone in ("123456", "1234567", "123456789", "1234567890123456", ""):
        invalid = find_or_create_customer(phone)
        assert invalid["status"] == "invalid" and not invalid["customer_phone"]
    assert _normalize_phone("(555) 010-1010") == "5550101010"
    assert _normalize_phone("123456789012345") == "123456789012345"

    assert _e164("15550707444") == "+15550707444"
    assert _e164("5550101010") == "+5550101010"
    assert _e164("34111111111") == "+34111111111"

    created = find_or_create_customer("+1 555 010 1010")
    repeated = find_or_create_customer("15550101010")
    assert created["status"] == "created"
    assert repeated["status"] == "existing"
    assert repeated["customer_phone"] == created["customer_phone"] == "+15550101010"
    customer = created["customer_phone"]
    assert find_or_create_customer("1-555-010-1010")["status"] == "existing"
    assert find_or_create_customer("(555) 010-1010")["status"] == "created"

    spec = importlib.util.spec_from_file_location("salon_copy_two", __file__)
    copy_two = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(copy_two)
    assert copy_two._state is _state
    assert copy_two.find_or_create_customer("15550101010")["customer_phone"] == customer
    fresh_phone = "15550109999"
    from_copy = copy_two.find_or_create_customer(fresh_phone)
    assert from_copy["status"] == "created"
    assert find_or_create_customer(fresh_phone)["status"] == "existing"

    with ThreadPoolExecutor(max_workers=16) as pool:
        for suffix in range(20, 30):
            concurrent = list(
                pool.map(lambda _: find_or_create_customer(f"1555010 20{suffix}"), range(16))
            )
            assert {result["status"] for result in concurrent} <= {"created", "existing"}
            assert len({result["customer_phone"] for result in concurrent}) == 1
            assert [result["status"] for result in concurrent].count("created") == 1

    before = (set(_state.customers), dict(_state.names))
    seeded = look_up_customer("+34 111 111 111")
    assert seeded == {"name": "Robin Vega", "status": "existing"}
    assert look_up_customer("+1 555 010 0000") == {"name": "", "status": "unknown"}
    assert look_up_customer("nonsense") == {"name": "", "status": "unknown"}
    assert (set(_state.customers), dict(_state.names)) == before, "look_up_customer wrote"

    current_date = _booking_today().isoformat()
    assert current_date == datetime.now(ZoneInfo(_SALON_TIMEZONE)).date().isoformat()
    first_date = (date.fromisoformat(current_date) + timedelta(days=1)).isoformat()
    second_date = (date.fromisoformat(current_date) + timedelta(days=2)).isoformat()
    assert check_availability("haircut", "not-a-date")["status"] == "invalid"
    assert check_availability("massage", first_date)["status"] == "invalid"
    first_slot = check_availability("haircut", first_date)["slots"][0]["slot_id"]

    assert create_booking(customer, "haircut", first_slot)["status"] == "not_confirmed"
    assert (
        create_booking(customer, "haircut", first_slot, confirmed=False)["status"]
        == "not_confirmed"
    )
    assert (
        create_booking("555 000 0000", "haircut", first_slot, confirmed=True)["status"]
        == "customer_not_found"
    )
    assert not _state.bookings

    booking = create_booking(customer, "haircut", first_slot, confirmed=True)
    assert booking["status"] == "booked"
    active = list_bookings(customer)["bookings"]
    assert len(active) == 1 and active[0]["booking_id"] == booking["booking_id"]
    assert (
        copy_two.create_booking(customer, "haircut", first_slot, confirmed=True)["status"]
        == "slot_unavailable"
    )
    assert first_slot not in {
        slot["slot_id"] for slot in check_availability("haircut", first_date)["slots"]
    }

    second_slot = check_availability("haircut", second_date)["slots"][0]["slot_id"]
    assert (
        modify_booking(customer, booking["booking_id"], "haircut", second_slot)["status"]
        == "not_confirmed"
    )
    assert list_bookings(customer)["bookings"] == active
    assert (
        modify_booking("555 000 0000", booking["booking_id"], "haircut", second_slot, True)[
            "status"
        ]
        == "not_found"
    )
    changed = modify_booking(
        customer, booking["booking_id"], "haircut", second_slot, confirmed=True
    )
    assert changed["status"] == "modified"

    assert cancel_booking(customer, booking["booking_id"])["status"] == "not_confirmed"
    assert (
        cancel_booking(customer, "bkg_missing", confirmed=True)["status"] == "not_found"
    )
    cancelled = cancel_booking(customer, booking["booking_id"], confirmed=True)
    assert cancelled["status"] == "cancelled"
    assert list_bookings(customer)["bookings"] == []
    assert (
        cancel_booking(customer, booking["booking_id"], confirmed=True)["status"]
        == "already_cancelled"
    )

    assert record_complaint(customer, "   ")["status"] == "invalid"
    assert (
        record_complaint("555 000 0000", "Uneven cut.")["status"] == "customer_not_found"
    )
    complaint = record_complaint(customer, "My cut was uneven.")
    assert complaint["status"] == "recorded"
    assert _state.complaints[complaint["complaint_id"]]["customer_phone"] == "15550101010"

    reshaped = list_bookings("+1 (555) 010-1010")["bookings"]
    assert reshaped == list_bookings(customer)["bookings"]

    print("salon in-memory check passed")


if __name__ == "__main__":
    _demo()
