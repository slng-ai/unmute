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

# ponytail: worker-local state behind one lock. `setdefault` is the whole
# race-free part: dict.setdefault is atomic under the GIL, so whichever copy
# imports first wins and the rest get that same object, and the throwaway
# module the losers built is collected. Ceiling: one process. Put a real
# service behind these functions before a second replica exists, and the
# lock becomes that service's transaction.
_fresh = types.ModuleType("unmute_salon_state")
_fresh.customers = {}
_fresh.bookings = {}
_fresh.complaints = {}
_fresh.lock = threading.Lock()
_state = sys.modules.setdefault("unmute_salon_state", _fresh)

_SERVICES = {"haircut", "hair-color", "blowout"}
_TIMES = ("09:00", "11:30", "15:00")


def _now():
    return datetime.now(UTC).isoformat()


def _booking_today() -> date:
    """Return the calendar date used by booking validation."""
    return date.today()


def _normalize_phone(phone):
    digits = "".join(character for character in str(phone) if character.isdigit())
    return digits if 10 <= len(digits) <= 15 else ""


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
            "customer_id": "",
            "status": "invalid",
            "summary": "A valid phone number of 10 to 15 digits is required.",
        }
    with _state.lock:
        existing = _state.customers.get(normalized_phone)
        if existing is not None:
            return {
                "customer_id": existing,
                "status": "existing",
                "summary": "The existing customer was verified.",
            }
        customer_id = f"cus_{uuid4().hex[:12]}"
        _state.customers[normalized_phone] = customer_id
    return {
        "customer_id": customer_id,
        "status": "created",
        "summary": "A new customer was verified and created.",
    }


def list_bookings(customer_id):
    with _state.lock:
        rows = [
            {
                "booking_id": booking_id,
                "service": booking["service"],
                "start_time": booking["start_time"],
                "status": booking["status"],
            }
            for booking_id, booking in _state.bookings.items()
            if booking["customer_id"] == customer_id and booking["status"] == "booked"
        ]
    return {"bookings": sorted(rows, key=lambda row: row["start_time"])}


def get_current_date() -> dict[str, str]:
    """Return the current booking date for relative-date requests."""
    return {"date": _booking_today().isoformat()}


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


def create_booking(customer_id, service, slot_id, confirmed=False):
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
    with _state.lock:
        if customer_id not in _state.customers.values():
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
            "customer_id": customer_id,
            "service": service,
            "slot_id": slot_id,
            "start_time": f"{parts[0]}T{parts[2]}:00",
            "status": "booked",
            "created_at": timestamp,
            "updated_at": timestamp,
        }
    return {"booking_id": booking_id, "status": "booked", "summary": "Booking saved."}


def modify_booking(customer_id, booking_id, service, slot_id, confirmed=False):
    if confirmed is not True:
        return {
            "booking_id": booking_id,
            "status": "not_confirmed",
            "summary": "The booking change was not confirmed.",
        }
    parts = _slot_parts(slot_id)
    if not parts or parts[1] != service:
        return {"booking_id": booking_id, "status": "invalid", "summary": "Invalid slot."}
    with _state.lock:
        booking = _state.bookings.get(booking_id)
        if (
            booking is None
            or booking["customer_id"] != customer_id
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


def cancel_booking(customer_id, booking_id, confirmed=False):
    if confirmed is not True:
        return {
            "booking_id": booking_id,
            "status": "not_confirmed",
            "summary": "The booking change was not confirmed.",
        }
    with _state.lock:
        booking = _state.bookings.get(booking_id)
        if booking is None or booking["customer_id"] != customer_id:
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


def record_complaint(customer_id, summary, requested_resolution=""):
    clean_summary = " ".join(str(summary).split())
    if not clean_summary:
        return {"complaint_id": "", "status": "invalid"}
    with _state.lock:
        if customer_id not in _state.customers.values():
            return {"complaint_id": "", "status": "customer_not_found"}
        complaint_id = f"cmp_{uuid4().hex[:12]}"
        _state.complaints[complaint_id] = {
            "customer_id": customer_id,
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
        assert invalid["status"] == "invalid" and not invalid["customer_id"]
    assert _normalize_phone("(555) 010-1010") == "5550101010"
    assert _normalize_phone("123456789012345") == "123456789012345"

    created = find_or_create_customer("+1 555 010 1010")
    repeated = find_or_create_customer("15550101010")
    assert created["status"] == "created"
    assert repeated["status"] == "existing"
    assert repeated["customer_id"] == created["customer_id"]
    customer = created["customer_id"]

    # The compiler emits this file once per tool. Load a second copy the way the
    # runtime would and prove both copies read and write the one store.
    spec = importlib.util.spec_from_file_location("salon_copy_two", __file__)
    copy_two = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(copy_two)
    assert copy_two._state is _state
    assert copy_two.find_or_create_customer("15550101010")["customer_id"] == customer
    fresh_phone = "15550109999"
    from_copy = copy_two.find_or_create_customer(fresh_phone)
    assert from_copy["status"] == "created"
    assert find_or_create_customer(fresh_phone)["customer_id"] == from_copy["customer_id"]

    with ThreadPoolExecutor(max_workers=16) as pool:
        for suffix in range(20, 30):
            concurrent = list(
                pool.map(lambda _: find_or_create_customer(f"1555010 20{suffix}"), range(16))
            )
            assert {result["status"] for result in concurrent} <= {"created", "existing"}
            assert len({result["customer_id"] for result in concurrent}) == 1

    current_date = get_current_date()["date"]
    assert current_date == _booking_today().isoformat()
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
        create_booking("cus_missing", "haircut", first_slot, confirmed=True)["status"]
        == "customer_not_found"
    )
    assert not _state.bookings

    booking = create_booking(customer, "haircut", first_slot, confirmed=True)
    assert booking["status"] == "booked"
    active = list_bookings(customer)["bookings"]
    assert len(active) == 1 and active[0]["booking_id"] == booking["booking_id"]
    # One active booking per slot, checked through the second copy.
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
        modify_booking("cus_missing", booking["booking_id"], "haircut", second_slot, True)[
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
    assert record_complaint("cus_missing", "Uneven cut.")["status"] == "customer_not_found"
    complaint = record_complaint(customer, "My cut was uneven.")
    assert complaint["status"] == "recorded"
    assert _state.complaints[complaint["complaint_id"]]["customer_id"] == customer

    print("salon in-memory check passed")


if __name__ == "__main__":
    _demo()
