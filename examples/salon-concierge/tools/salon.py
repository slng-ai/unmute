"""Salon demo tools, backed by one in-process store.

The compiler copies this file once per tool (`tools/<tool_name>.py`), so a
module-level dict would give each tool its own private store and the copies
would never see each other's writes. One module parked in `sys.modules` under a
name nothing else uses is the smallest thing every copy can reach.

Four tools, and the split is the conversation's own seam: `find_slots` reads the
diary, `save_booking` writes it, `find_or_create_customer` verifies, and
`record_complaint` files. A read and a write is one call each on the two turns a
booking actually takes, where five narrower tools cost a call apiece.
"""

import sys
import threading
import types
from datetime import UTC, date, datetime, timedelta
from uuid import uuid4
from zoneinfo import ZoneInfo

_fresh = types.ModuleType("unmute_salon_state")
vars(_fresh).update(
    customers=set(),
    names={"34111111111": "Robin Vega"},
    bookings={},
    complaints={},
    lock=threading.Lock(),
)
_state = sys.modules.setdefault("unmute_salon_state", _fresh)

_SERVICES = ("haircut", "hair-color", "blowout")
_TIMES = ("09:00", "11:30", "15:00")
_SALON_TIMEZONE = "Europe/Madrid"


def _now():
    return datetime.now(UTC).isoformat()


def _booking_today() -> date:
    """Return the calendar date used by booking validation, in the salon's zone.

    Not `date.today()`, which reads the container clock. That clock is UTC, so a
    booking taken at 23:30 in Madrid landed on the following day and every date
    check here disagreed with the caller by one. The zone matches the `timezone:`
    on the `today` prefetch entry in agent.yaml, which is what the pre-fetched
    {{booking_date}} is read in, so the prompt and the validation agree about
    what day it is.
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


def _slot_passed(requested_date, time) -> bool:
    """True when this slot is earlier than the salon's clock reads right now.

    The date check alone let a caller book 15:00 today at four minutes past
    three: the prompt says not to offer a time that has gone, and a live call
    showed the prompt is not enough. The backend refuses it too.
    """
    now = datetime.now(ZoneInfo(_SALON_TIMEZONE))
    return requested_date < now.date() or (
        requested_date == now.date() and time <= now.strftime("%H:%M")
    )


def _slot_parts(slot_id):
    try:
        date_text, service, time = str(slot_id).split("|")
        requested_date = date.fromisoformat(date_text)
    except (TypeError, ValueError):
        return None
    if _slot_passed(requested_date, time) or service not in _SERVICES or time not in _TIMES:
        return None
    return date_text, service, time


def _slot_taken(slot_id) -> bool:
    """True when an active booking already holds this slot. Call under the lock."""
    return any(
        booking["slot_id"] == slot_id and booking["status"] == "booked"
        for booking in _state.bookings.values()
    )


def _held_by(caller):
    """The caller's active bookings, soonest first. Call under the lock."""
    rows = [
        {
            "booking_id": booking_id,
            "service": booking["service"],
            "date": booking["slot_id"].split("|")[0],
            "time": booking["slot_id"].split("|")[2],
        }
        for booking_id, booking in _state.bookings.items()
        if booking["customer_phone"] == caller and booking["status"] == "booked"
    ]
    return sorted(rows, key=lambda row: (row["date"], row["time"]))


def find_or_create_customer(phone):
    normalized_phone = _normalize_phone(phone)
    if not normalized_phone:
        return {
            "customer_phone": "",
            "customer_status": "invalid",
            "summary": "A valid phone number of 10 to 15 digits is required.",
        }
    with _state.lock:
        known = normalized_phone in _state.customers
        _state.customers.add(normalized_phone)
    return {
        "customer_phone": _e164(normalized_phone),
        "customer_status": "existing" if known else "created",
        "summary": (
            "The existing customer was verified."
            if known
            else "A new customer was verified and created."
        ),
    }


def find_slots(customer_phone, date="", service=""):
    """Read the diary: what the caller holds, and what is free.

    One call answers both questions because the conversation asks them together.
    A caller saying "move my appointment to Friday" needs their booking id and
    Friday's free times, and fetching those separately cost two model round
    trips for one request.

    `bookings` comes back whatever was asked, so a modify or a cancel never
    needs a second lookup. `slots` is empty when no date was given.
    """
    caller = _normalize_phone(customer_phone)
    # Every argument is required on one target and optional on the other, so an
    # absent one arrives as "" from one and as None from the other. Both mean
    # "not asked for" here.
    date, service = date or "", service or ""
    # `any` is how the schema spells "all three": an enum cannot carry an empty
    # string, which Gemini refuses outright with a 400 on the whole request.
    service = "" if service == "any" else service
    with _state.lock:
        bookings = _held_by(caller)

    if not date:
        # A read with no date can only answer "what does this caller already
        # hold". When the answer is nothing, reporting `ok` alongside two empty
        # lists reads as a completed search: a live call (149178e8) took it that
        # way, then invented an appointment the diary had twice said was not
        # there and asked for the service three times. Name the gap instead.
        if not bookings:
            return {"bookings": [], "slots": [], "status": "need_date"}
        return {"bookings": bookings, "slots": [], "status": "ok"}

    if service and service not in _SERVICES:
        return {"bookings": bookings, "slots": [], "status": "invalid_service"}
    try:
        requested_date = datetime.strptime(date, "%Y-%m-%d").date()
    except (TypeError, ValueError):
        return {"bookings": bookings, "slots": [], "status": "invalid_date"}
    if requested_date < _booking_today():
        return {"bookings": bookings, "slots": [], "status": "invalid_date"}

    wanted = (service,) if service else _SERVICES
    with _state.lock:
        used = {
            booking["slot_id"]
            for booking in _state.bookings.values()
            if booking["status"] == "booked"
        }
    slots = [
        {
            "slot_id": f"{date}|{each}|{time}",
            "service": each,
            "date": date,
            "time": time,
        }
        for each in wanted
        for time in _TIMES
        if f"{date}|{each}|{time}" not in used and not _slot_passed(requested_date, time)
    ]
    return {"bookings": bookings, "slots": slots, "status": "ok" if slots else "full"}


# Spelled out rather than read off strftime("%A"), which follows the container's
# locale and would hand the agent a Spanish weekday on a Spanish host.
_WEEKDAYS = (
    "Monday",
    "Tuesday",
    "Wednesday",
    "Thursday",
    "Friday",
    "Saturday",
    "Sunday",
)


def _spoken(day, time):
    """The day and time of one booking, written the way it is said out loud.

    "2026-09-18", "09:00" -> "Friday at 9:00 AM".
    """
    try:
        weekday = _WEEKDAYS[date.fromisoformat(day).weekday()]
        hour, minute = int(time[:2]), time[3:5]
    except (ValueError, IndexError):
        return ""
    return f"{weekday} at {(hour - 1) % 12 + 1}:{minute} {'AM' if hour < 12 else 'PM'}"


def _appointment(booking_id, service, slot_id, action):
    """The saved shape of one successful booking action.

    Returned only on success, and complete when it is returned: the step saves
    it without the model in between, so a half-filled record here is a
    half-filled record in the call state.

    `spoken` is the whole confirmation phrase, because every part of it the
    agent composes itself, it has got wrong on a live call. It held a date and
    nothing else, and a Friday booking was confirmed as "Thursday the 18th", so
    the weekday moved in here. It then held a weekday and a 24 hour time, and
    two calls on 2026-09-16 read "09:00" back as "09:00 AM" and answered a move
    saved at 09:00 with "Friday at 3:00 PM", which was the time the caller had
    said earlier in the sentence "the same time". The model is a poor place to
    keep a fact that is already known: this field is the sentence, and the
    prompts copy it.
    """
    parts = _slot_parts(slot_id)
    day, time = (parts[0], parts[2]) if parts else ("", "")
    return {
        "booking_id": booking_id,
        "service": service,
        "date": day,
        "spoken": _spoken(day, time),
        "time": time,
        "action": action,
    }


def _refuse(status, summary, booking_id=""):
    return {"status": status, "booking_id": booking_id, "summary": summary}


def save_booking(
    customer_phone,
    action,
    confirmed=False,
    slot_id="",
    booking_id="",
    additional=False,
):
    """Write the diary. The only tool in this package that changes a booking.

    One write tool rather than three, so the model picks an action rather than
    picking between three similarly named tools, and every refusal below is
    stated once instead of in triplicate.
    """
    if confirmed is not True:
        return _refuse("not_confirmed", "The booking change was not confirmed.", booking_id)
    if action not in ("book", "move", "cancel"):
        return _refuse("invalid", "Action must be book, move, or cancel.", booking_id)

    caller = _normalize_phone(customer_phone)
    slot_id, booking_id = slot_id or "", booking_id or ""
    if action == "cancel":
        return _cancel(caller, booking_id)
    if action == "book":
        return _book(caller, slot_id, additional)
    return _move(caller, booking_id, slot_id)


def _book(caller, slot_id, additional):
    parts = _slot_parts(slot_id)
    if not parts:
        return _refuse("invalid", "That slot is not one the diary offered.")
    _, service, _ = parts
    new_id = f"bkg_{uuid4().hex[:12]}"
    timestamp = _now()
    with _state.lock:
        if caller not in _state.customers:
            return _refuse("not_found", "The customer is not verified.")
        held = _held_by(caller)
        if held and additional is not True:
            # A live call answered "move it to the day after tomorrow" with a
            # second booking, prompt notwithstanding. A caller who holds a
            # booking is changing it unless they asked for another one, and the
            # backend is where that rule holds.
            refusal = _refuse(
                "has_booking",
                "The customer already has a booking. Move it, or pass additional "
                "true for a second appointment.",
            )
            refusal["existing"] = held
            return refusal
        if _slot_taken(slot_id):
            return _refuse("slot_taken", "That time was just taken.")
        _state.bookings[new_id] = {
            "customer_phone": caller,
            "service": service,
            "slot_id": slot_id,
            "status": "booked",
            "created_at": timestamp,
            "updated_at": timestamp,
        }
    saved = _appointment(new_id, service, slot_id, "book")
    return {
        "status": "booked",
        "booking_id": new_id,
        "service": service,
        "date": saved["date"],
        "time": saved["time"],
        "appointment": saved,
        "summary": "Booking saved.",
    }


def _move(caller, booking_id, slot_id):
    parts = _slot_parts(slot_id)
    if not parts:
        return _refuse("invalid", "That slot is not one the diary offered.", booking_id)
    _, service, _ = parts
    with _state.lock:
        booking = _state.bookings.get(booking_id)
        if booking is None or booking["customer_phone"] != caller or booking["status"] != "booked":
            return _refuse("not_found", "No active matching booking was found.", booking_id)
        if slot_id != booking["slot_id"] and _slot_taken(slot_id):
            return _refuse("slot_taken", "That time was just taken.", booking_id)
        booking.update(service=service, slot_id=slot_id, updated_at=_now())
    saved = _appointment(booking_id, service, slot_id, "move")
    return {
        "status": "moved",
        "booking_id": booking_id,
        "service": service,
        "date": saved["date"],
        "time": saved["time"],
        "appointment": saved,
        "summary": "Booking moved.",
    }


def _cancel(caller, booking_id):
    with _state.lock:
        booking = _state.bookings.get(booking_id)
        if booking is None or booking["customer_phone"] != caller:
            return _refuse("not_found", "No matching booking was found.", booking_id)
        if booking["status"] == "cancelled":
            # Not its own status: find_slots only ever returns active bookings,
            # so a cancelled one is already absent from everything the model
            # was shown. The summary carries the difference.
            return _refuse("not_found", "That booking was already cancelled.", booking_id)
        booking.update(status="cancelled", updated_at=_now())
        saved = _appointment(booking_id, booking["service"], booking["slot_id"], "cancel")
    return {
        "status": "cancelled",
        "booking_id": booking_id,
        "service": saved["service"],
        "date": saved["date"],
        "time": saved["time"],
        "appointment": saved,
        "summary": "Booking cancelled.",
    }


def record_complaint(customer_phone, summary, requested_resolution=""):
    clean_summary = " ".join(str(summary).split())
    if not clean_summary:
        return {"complaint_id": "", "status": "invalid"}
    caller = _normalize_phone(customer_phone)
    with _state.lock:
        if caller not in _state.customers:
            return {"complaint_id": "", "status": "customer_not_found"}
        complaint_id = f"cmp_{uuid4().hex[:12]}"
        clean_resolution = " ".join(str(requested_resolution).split())
        _state.complaints[complaint_id] = {
            "customer_phone": caller,
            "summary": clean_summary,
            "requested_resolution": clean_resolution,
            "created_at": _now(),
        }
    return {
        "complaint_id": complaint_id,
        "status": "recorded",
        # The saved record, complete on success and absent otherwise. The step
        # saves this without asking the model to reassemble it, which is a model
        # request that decided nothing and a chance to retype the id.
        "complaint": {
            "complaint_id": complaint_id,
            "summary": clean_summary,
            "requested_resolution": clean_resolution,
        },
    }


def _demo():
    import importlib.util
    from concurrent.futures import ThreadPoolExecutor

    for phone in ("123456", "1234567", "123456789", "1234567890123456", ""):
        invalid = find_or_create_customer(phone)
        assert invalid["customer_status"] == "invalid" and not invalid["customer_phone"]
    assert _normalize_phone("(555) 010-1010") == "5550101010"
    assert _normalize_phone("123456789012345") == "123456789012345"
    assert _e164("15550707444") == "+15550707444"
    assert _e164("34111111111") == "+34111111111"

    # The confirmation phrase, both ends of the clock and both ways it can be
    # asked with nothing to say.
    assert _spoken("2026-09-18", "09:00") == "Friday at 9:00 AM"
    assert _spoken("2026-09-18", "15:00") == "Friday at 3:00 PM"
    assert _spoken("2026-09-18", "00:30") == "Friday at 12:30 AM"
    assert _spoken("2026-09-18", "12:00") == "Friday at 12:00 PM"
    assert _spoken("", "") == "" and _spoken("2026-09-18", "") == ""

    created = find_or_create_customer("+1 555 010 1010")
    repeated = find_or_create_customer("15550101010")
    assert created["customer_status"] == "created"
    assert repeated["customer_status"] == "existing"
    assert repeated["customer_phone"] == created["customer_phone"] == "+15550101010"
    customer = created["customer_phone"]
    assert find_or_create_customer("1-555-010-1010")["customer_status"] == "existing"

    spec = importlib.util.spec_from_file_location("salon_copy_two", __file__)
    assert spec is not None and spec.loader is not None
    copy_two = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(copy_two)
    assert copy_two._state is _state, "each emitted copy must reach one store"
    assert copy_two.find_or_create_customer("15550101010")["customer_phone"] == customer

    with ThreadPoolExecutor(max_workers=16) as pool:
        for suffix in range(20, 24):
            concurrent = list(
                pool.map(lambda _: find_or_create_customer(f"1555010 20{suffix}"), range(16))
            )
            assert {result["customer_status"] for result in concurrent} <= {"created", "existing"}
            assert len({result["customer_phone"] for result in concurrent}) == 1
            assert [result["customer_status"] for result in concurrent].count("created") == 1

    current_date = _booking_today().isoformat()
    first_date = (date.fromisoformat(current_date) + timedelta(days=1)).isoformat()
    second_date = (date.fromisoformat(current_date) + timedelta(days=2)).isoformat()

    # A read with no date answers "what do I hold". A caller holding nothing has
    # asked a question this call cannot answer, so it says so rather than
    # reporting a successful search of nothing.
    empty = find_slots(customer)
    assert empty == {"bookings": [], "slots": [], "status": "need_date"}
    assert find_slots(customer, date="not-a-date")["status"] == "invalid_date"
    assert find_slots(customer, date=first_date, service="massage")["status"] == "invalid_service"
    # No service means every service, so one call covers "what have you got".
    every = find_slots(customer, date=first_date)
    assert {row["service"] for row in every["slots"]} == set(_SERVICES)
    assert len(every["slots"]) == len(_SERVICES) * len(_TIMES)
    haircuts = find_slots(customer, date=first_date, service="haircut")
    assert {row["service"] for row in haircuts["slots"]} == {"haircut"}
    first_slot = haircuts["slots"][0]["slot_id"]

    assert save_booking(customer, "book", slot_id=first_slot)["status"] == "not_confirmed"
    assert save_booking(customer, "sing", confirmed=True)["status"] == "invalid"
    assert (
        save_booking("555 000 0000", "book", confirmed=True, slot_id=first_slot)["status"]
        == "not_found"
    )
    assert not _state.bookings

    booked = save_booking(customer, "book", confirmed=True, slot_id=first_slot)
    assert booked["status"] == "booked"
    assert booked["appointment"]["action"] == "book"
    # The read now reports the booking without being asked for a date, and says
    # `ok` rather than `need_date`, because there is something to answer with.
    dateless = find_slots(customer)
    assert dateless["status"] == "ok", dateless
    held = dateless["bookings"]
    assert len(held) == 1 and held[0]["booking_id"] == booked["booking_id"]
    assert held[0]["service"] == "haircut"

    # Two callers want one slot: the second is refused the slot, not the booking.
    rival = copy_two.find_or_create_customer("1555010 2099")["customer_phone"]
    assert (
        copy_two.save_booking(rival, "book", confirmed=True, slot_id=first_slot)["status"]
        == "slot_taken"
    )
    assert first_slot not in {
        row["slot_id"] for row in find_slots(customer, date=first_date, service="haircut")["slots"]
    }

    second_slot = find_slots(customer, date=second_date, service="haircut")["slots"][0]["slot_id"]
    # A caller who holds a booking is changing it unless they asked for another.
    refused = save_booking(customer, "book", confirmed=True, slot_id=second_slot)
    assert refused["status"] == "has_booking", refused
    assert [row["booking_id"] for row in refused["existing"]] == [booked["booking_id"]]
    assert len(find_slots(customer)["bookings"]) == 1
    extra = save_booking(
        customer, "book", confirmed=True, slot_id=second_slot, additional=True
    )
    assert extra["status"] == "booked"
    assert save_booking(customer, "cancel", confirmed=True, booking_id=extra["booking_id"])[
        "status"
    ] == "cancelled"

    # A slot earlier today is over, on the salon's clock and not the container's.
    salon_now = datetime.now(ZoneInfo(_SALON_TIMEZONE)).strftime("%H:%M")
    assert all(
        row["time"] > salon_now
        for row in find_slots(customer, date=current_date, service="haircut")["slots"]
    )
    assert _slot_parts(f"{current_date}|haircut|00:00") is None

    assert (
        save_booking(customer, "move", confirmed=True, booking_id=booked["booking_id"])["status"]
        == "invalid"
    )
    assert (
        save_booking(
            "555 000 0000", "move", confirmed=True, booking_id=booked["booking_id"],
            slot_id=second_slot,
        )["status"]
        == "not_found"
    )
    moved = save_booking(
        customer, "move", confirmed=True, booking_id=booked["booking_id"], slot_id=second_slot
    )
    assert moved["status"] == "moved" and moved["appointment"]["action"] == "move"
    assert find_slots(customer)["bookings"][0]["date"] == second_date

    assert (
        save_booking(customer, "cancel", confirmed=True, booking_id="bkg_missing")["status"]
        == "not_found"
    )
    cancelled = save_booking(
        customer, "cancel", confirmed=True, booking_id=booked["booking_id"]
    )
    assert cancelled["status"] == "cancelled" and cancelled["appointment"]["action"] == "cancel"
    assert find_slots(customer)["bookings"] == []
    repeat = save_booking(customer, "cancel", confirmed=True, booking_id=booked["booking_id"])
    assert repeat["status"] == "not_found" and "already cancelled" in repeat["summary"]

    assert record_complaint(customer, "   ")["status"] == "invalid"
    assert record_complaint("555 000 0000", "Uneven cut.")["status"] == "customer_not_found"
    complaint = record_complaint(customer, "My cut  was   uneven.", "A redo")
    assert complaint["complaint"] == {
        "complaint_id": complaint["complaint_id"],
        "summary": "My cut was uneven.",
        "requested_resolution": "A redo",
    }, complaint
    assert "complaint" not in record_complaint(customer, "   ")
    assert _state.complaints[complaint["complaint_id"]]["customer_phone"] == "15550101010"

    reshaped = find_slots("+1 (555) 010-1010")["bookings"]
    assert reshaped == find_slots(customer)["bookings"]

    print("salon in-memory check passed")


if __name__ == "__main__":
    _demo()
