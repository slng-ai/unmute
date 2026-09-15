"""The two counter tools, backed by a small in-process store.

The compiler copies this file once per tool that names it (`tools/<tool>.py`),
so a plain module level dict would give each copy a private store and a refill
placed through one tool would be invisible to the other. One module parked in
`sys.modules` under a name nothing else uses is the smallest thing every copy
reaches.

Nothing here is a real dispensing system. It holds four repeat prescriptions and
a lock, which is enough to show the three answers a caller on this line actually
gets: here it is, you have none left, and that is not a reference we hold.
"""

import sys
import threading
import types
from datetime import date, timedelta

_fresh = types.ModuleType("unmute_pharmacy_state")
vars(_fresh).update(
    # Four repeats, keyed by reference. Two letters, four digits, one letter,
    # which is the shape instructions.md describes in words rather than writing
    # one out: a model cannot tell an illustration from a value it is holding.
    prescriptions={
        "RX4821B": {
            "surname": "Okafor",
            "medicine": "Atorvastatin 20 milligram tablets",
            "repeats_left": 3,
            "last_issued": "2026-08-19",
        },
        "RX7310D": {
            "surname": "Bianchi",
            "medicine": "Levothyroxine 50 microgram tablets",
            "repeats_left": 1,
            "last_issued": "2026-08-30",
        },
        "RX2045K": {
            "surname": "Novak",
            "medicine": "Salbutamol inhaler",
            "repeats_left": 0,
            "last_issued": "2026-06-02",
        },
        "RX9987M": {
            "surname": "Hardy",
            "medicine": "Ramipril 5 milligram capsules",
            "repeats_left": 2,
            "last_issued": "2026-09-01",
        },
    },
    orders={},
    lock=threading.Lock(),
)
_state = sys.modules.setdefault("unmute_pharmacy_state", _fresh)

# Two working days to dispense, which is what the collection document says. The
# two have to agree: a caller who hears one number from the agent and reads
# another in the leaflet rings back.
_WORKING_DAYS_TO_DISPENSE = 2


def _normalize(reference):
    """Read a reference the way it arrives from speech.

    A caller says "R X, four eight two one, B" and the model writes it with
    spaces, dashes or in lower case. None of that is a different prescription,
    so the letters and digits are what this matches on.
    """
    return "".join(character for character in str(reference) if character.isalnum()).upper()


def _ready_on(today=None):
    """The collection day, skipping the weekend the counter is shut on."""
    day = today or date.today()
    added = 0
    while added < _WORKING_DAYS_TO_DISPENSE:
        day += timedelta(days=1)
        if day.weekday() < 5:
            added += 1
    return day.isoformat()


def look_up_prescription(reference):
    """Find one repeat prescription and say what state it is in.

    `found` is false for a reference nobody holds, and every other field comes
    back empty rather than absent, so the model reads the same shape either way
    and has nothing to invent.
    """
    key = _normalize(reference)
    with _state.lock:
        record = _state.prescriptions.get(key)
        ordered = key in _state.orders
    if record is None:
        return {
            "found": False,
            "reference": key,
            "medicine": "",
            "repeats_left": 0,
            "last_issued": "",
            "already_ordered": False,
        }
    return {
        "found": True,
        "reference": key,
        "medicine": record["medicine"],
        "repeats_left": record["repeats_left"],
        "last_issued": record["last_issued"],
        "already_ordered": ordered,
    }


def request_refill(reference):
    """Place the reorder, or say why it cannot be placed.

    Four outcomes, and each one is a real thing that happens at a counter:
    placed, this is already on its way, the repeats have run out and the surgery
    has to authorize a new one, and we do not hold this reference.
    """
    key = _normalize(reference)
    with _state.lock:
        record = _state.prescriptions.get(key)
        if record is None:
            return {"status": "unknown_reference", "reference": key, "medicine": "", "ready_on": ""}
        if key in _state.orders:
            existing = _state.orders[key]
            return {
                "status": "already_ordered",
                "reference": key,
                "medicine": record["medicine"],
                "ready_on": existing["ready_on"],
            }
        if record["repeats_left"] < 1:
            return {
                "status": "no_repeats_left",
                "reference": key,
                "medicine": record["medicine"],
                "ready_on": "",
            }
        ready_on = _ready_on()
        record["repeats_left"] -= 1
        _state.orders[key] = {"ready_on": ready_on}
    return {
        "status": "placed",
        "reference": key,
        "medicine": record["medicine"],
        "ready_on": ready_on,
    }


def _demo():
    import importlib.util
    import re

    _state.orders.clear()

    # Spoken punctuation and lower case are the same prescription.
    spaced = look_up_prescription("rx 4821 b")
    assert spaced["found"] and spaced["reference"] == "RX4821B"
    assert spaced["repeats_left"] == 3 and not spaced["already_ordered"]

    placed = request_refill("RX4821B")
    assert placed["status"] == "placed"
    assert re.fullmatch(r"\d{4}-\d{2}-\d{2}", placed["ready_on"])
    # The counter is shut at the weekend, so a collection day never lands there.
    assert date.fromisoformat(placed["ready_on"]).weekday() < 5

    # A second attempt is not a second order, and the lookup now says so, which
    # is what stops the agent reordering something already on its way.
    again = request_refill("RX4821B")
    assert again["status"] == "already_ordered" and again["ready_on"] == placed["ready_on"]
    assert look_up_prescription("RX4821B")["already_ordered"]

    assert request_refill("RX2045K")["status"] == "no_repeats_left"
    assert request_refill("ZZ0000Z")["status"] == "unknown_reference"
    assert not look_up_prescription("ZZ0000Z")["found"]

    # Every emitted copy of this file has to reach one store, or the tool that
    # places an order and the tool that reports one would disagree.
    spec = importlib.util.spec_from_file_location("pharmacy_copy_two", __file__)
    assert spec is not None and spec.loader is not None
    copy_two = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(copy_two)
    assert copy_two._state is _state
    assert copy_two.look_up_prescription("RX4821B")["already_ordered"]

    _state.orders.clear()
    _state.prescriptions["RX4821B"]["repeats_left"] = 3
    print("pharmacy demo ok")


if __name__ == "__main__":
    _demo()
