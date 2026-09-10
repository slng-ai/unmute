"""Local handlers for the terminal-step fixture.

Deterministic on purpose: the smoke tests drive these and assert on what the
step saved, so a random reference would make the assertion unwritable.
"""


def look_up(phone: str) -> dict:
    digits = "".join(character for character in phone if character.isdigit())
    if len(digits) < 8:
        return {
            "customer_phone": "",
            "status": "invalid",
            "summary": "That number is too short to look up.",
        }
    return {
        "customer_phone": "+" + digits,
        "status": "existing",
        "summary": "A customer was found.",
    }


def book_it(service: str, confirmed: bool, customer_phone: str = "") -> dict:
    if not confirmed:
        return {"status": "not_confirmed", "summary": "The caller has not agreed yet."}
    return {
        "status": "booked",
        "summary": "Booking saved.",
        "booking": {"reference": "bkg_0001", "service": service, "action": "create"},
    }


def cancel_it(reference: str, confirmed: bool) -> dict:
    if not confirmed:
        return {"status": "not_confirmed", "summary": "The caller has not agreed yet."}
    if reference != "bkg_0001":
        return {"status": "not_found", "summary": "No booking with that reference."}
    return {
        "status": "cancelled",
        "summary": "Booking cancelled.",
        "booking": {"reference": reference, "service": "haircut", "action": "cancel"},
    }
