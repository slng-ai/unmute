"""The intake desk's one tool, backed by an in-process store.

The compiler copies this file once per tool (`tools/<tool_name>.py`), so a
module-level dict would give each copy its own private store. One module parked
in `sys.modules` under a name nothing else uses is the smallest thing every copy
can reach. There is one tool here today, so the store is a convenience rather
than a necessity, but the shape is the one that keeps working when a second tool
arrives.

Nothing here validates the phone number, the address or the date. It does not
have to: every one of them is injected from declared state, and the type on the
variable already refused a wrong one where it entered. That is the whole point
of the package. What this file does check is the one thing state cannot: whether
this caller already has a record.
"""

import sys
import threading
import types
from datetime import datetime
from zoneinfo import ZoneInfo

_fresh = types.ModuleType("unmute_intake_state")
vars(_fresh).update(records={}, lock=threading.Lock())
_state = sys.modules.setdefault("unmute_intake_state", _fresh)

_ENQUIRIES = ("new_customer", "existing_customer", "complaint", "other")
# The same zone as the `today` prefetch entry in agent.yaml, so the date this
# stamps and the date the prompt reads agree. Not the container clock, which is
# UTC: a record opened at 23:30 in Madrid would carry the following day.
_DESK_TIMEZONE = "Europe/Madrid"


def _today() -> str:
    """Today in the desk's own zone, in the shape the `Date` type accepts."""
    return datetime.now(ZoneInfo(_DESK_TIMEZONE)).date().isoformat()


def _reference(phone: str) -> str:
    """A reference number shaped the way the `Id` type accepts.

    Letters, digits, and then any of dot, dash, underscore or colon. Built from
    the number's last four digits and a per-call counter, so it is short enough
    to read out loud and different for a caller who rings twice.
    """
    tail = "".join(character for character in phone if character.isdigit())[-4:] or "0000"
    return f"REC-{tail}-{len(_state.records) + 1:03d}"


def create_customer_record(summary, phone="", email="", name="", enquiry=""):
    """Write the record and hand back what the model reads out.

    `summary` is the model's; the other four arrive through `inject:` and were
    checked by their declared types on the way into state. The date is this
    file's own: see the `inject:` comment in create_customer_record.yaml for why
    it is not injected.
    """
    if not phone or enquiry not in _ENQUIRIES:
        return {
            "record_id": "",
            "opened_on": _today(),
            "enquiry": enquiry,
            "status": "invalid",
        }
    with _state.lock:
        existing = _state.records.get(phone)
        if existing is not None:
            return dict(existing, status="duplicate")
        record = {
            "record_id": _reference(phone),
            "opened_on": _today(),
            "enquiry": enquiry,
        }
        _state.records[phone] = record
    return dict(record, name=name, email=email, summary=summary, status="created")


def _demo():
    import importlib.util
    import re

    _state.records.clear()

    created = create_customer_record(
        "Wants to book something next week.",
        phone="+34600111222",
        email="someone@invalid.test",
        name="Robin Vega",
        enquiry="new_customer",
    )
    assert created["status"] == "created"
    # Both of these have to satisfy their declared type, or the finish call that
    # relays them back into state is refused and the step cannot end.
    assert re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._:-]{0,63}", created["record_id"])
    assert re.fullmatch(r"\d{4}-\d{2}-\d{2}", created["opened_on"])

    repeated = create_customer_record(
        "Ringing again.",
        phone="+34600111222",
        email="someone@invalid.test",
        name="Robin Vega",
        enquiry="new_customer",
    )
    assert repeated["status"] == "duplicate"
    assert repeated["record_id"] == created["record_id"]

    # A withheld number skips the prefetch entry and leaves the default, and a
    # word outside the Literal set cannot reach state at all. Both arrive here
    # as an empty string, so neither writes a record. An invalid result still
    # carries a real date, because the shape it lands in declares one.
    for bad in ({"phone": ""}, {"enquiry": "vip"}, {"enquiry": ""}):
        arguments = {"phone": "+34600111333", "enquiry": "other"} | bad
        refused = create_customer_record("Nothing to write.", **arguments)
        assert refused["status"] == "invalid"
        assert re.fullmatch(r"\d{4}-\d{2}-\d{2}", refused["opened_on"])

    # Every emitted copy of this file has to reach one store, or two tools would
    # disagree about whether a caller already has a record.
    spec = importlib.util.spec_from_file_location("intake_copy_two", __file__)
    assert spec is not None and spec.loader is not None
    copy_two = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(copy_two)
    assert copy_two._state is _state
    assert copy_two.create_customer_record(
        "Same caller, other copy.",
        phone="+34600111222",
        enquiry="new_customer",
    )["status"] == "duplicate"

    _state.records.clear()
    print("intake demo ok")


if __name__ == "__main__":
    _demo()
