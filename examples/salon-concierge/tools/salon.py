import sqlite3
import tempfile
from concurrent.futures import ThreadPoolExecutor
from datetime import UTC, date, datetime, timedelta
from pathlib import Path
from uuid import uuid4


# ponytail: one private, runtime-local SQLite file is enough for the demo; use a
# shared service before multi-replica deployment or when worker-local state is
# not enough.
_DB_PATH = Path(tempfile.gettempdir()) / "unmute-salon-concierge" / "salon.db"
_SERVICES = {"haircut", "hair-color", "blowout"}
_TIMES = ("09:00", "11:30", "15:00")
_SCHEMA = """
CREATE TABLE IF NOT EXISTS customers (
    customer_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    phone TEXT NOT NULL,
    normalized_phone TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS bookings (
    booking_id TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL REFERENCES customers(customer_id),
    service TEXT NOT NULL,
    slot_id TEXT NOT NULL,
    start_time TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('booked', 'cancelled')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS one_active_booking_per_slot
ON bookings(slot_id) WHERE status = 'booked';
CREATE TABLE IF NOT EXISTS complaints (
    complaint_id TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL REFERENCES customers(customer_id),
    summary TEXT NOT NULL,
    requested_resolution TEXT NOT NULL,
    created_at TEXT NOT NULL
);
"""


def _connect():
    _DB_PATH.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    _DB_PATH.parent.chmod(0o700)
    _DB_PATH.touch(mode=0o600, exist_ok=True)
    _DB_PATH.chmod(0o600)
    database = sqlite3.connect(_DB_PATH, timeout=5)
    database.row_factory = sqlite3.Row
    database.execute("PRAGMA foreign_keys = ON")
    database.executescript(_SCHEMA)
    return database


def _now():
    return datetime.now(UTC).isoformat()


def _booking_today() -> date:
    """Return the calendar date used by booking validation."""
    return date.today()


def _normalize_name(name):
    return " ".join(str(name).split()).casefold()


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


def find_or_create_customer(name, phone):
    display_name = " ".join(str(name).split())
    normalized_name = _normalize_name(display_name)
    normalized_phone = _normalize_phone(phone)
    if not normalized_name or not normalized_phone:
        return {
            "customer_id": "",
            "customer_name": "",
            "status": "invalid",
            "summary": "A valid name and phone number are required.",
        }

    with _connect() as database:
        proposed_id = f"cus_{uuid4().hex[:12]}"
        inserted = database.execute(
            "INSERT OR IGNORE INTO customers VALUES (?, ?, ?, ?, ?, ?)",
            (
                proposed_id,
                display_name,
                normalized_name,
                str(phone).strip(),
                normalized_phone,
                _now(),
            ),
        ).rowcount
        customer = database.execute(
            "SELECT customer_id, name, normalized_name FROM customers "
            "WHERE normalized_phone = ?",
            (normalized_phone,),
        ).fetchone()
        if customer["normalized_name"] != normalized_name:
            return {
                "customer_id": "",
                "customer_name": "",
                "status": "name_mismatch",
                "summary": "The supplied details could not be verified together.",
            }
        status = "created" if inserted else "existing"
        summary = (
            "A new customer was verified and created."
            if inserted
            else "The existing customer was verified."
        )
        return {
            "customer_id": customer["customer_id"],
            "customer_name": customer["name"],
            "status": status,
            "summary": summary,
        }


def list_bookings(customer_id):
    with _connect() as database:
        rows = database.execute(
            "SELECT booking_id, service, start_time, status FROM bookings "
            "WHERE customer_id = ? AND status = 'booked' ORDER BY start_time",
            (customer_id,),
        ).fetchall()
    return {"bookings": [dict(row) for row in rows]}


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

    with _connect() as database:
        used = {
            row["slot_id"]
            for row in database.execute(
                "SELECT slot_id FROM bookings WHERE status = 'booked'"
            )
        }
    slots = []
    for time in _TIMES:
        slot_id = f"{date}|{service}|{time}"
        if slot_id not in used:
            slots.append(
                {
                    "slot_id": slot_id,
                    "start_time": f"{date}T{time}:00",
                }
            )
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
    try:
        with _connect() as database:
            customer = database.execute(
                "SELECT 1 FROM customers WHERE customer_id = ?", (customer_id,)
            ).fetchone()
            if not customer:
                return {
                    "booking_id": "",
                    "status": "customer_not_found",
                    "summary": "The customer is not verified.",
                }
            database.execute(
                "INSERT INTO bookings VALUES (?, ?, ?, ?, ?, 'booked', ?, ?)",
                (
                    booking_id,
                    customer_id,
                    service,
                    slot_id,
                    f"{parts[0]}T{parts[2]}:00",
                    timestamp,
                    timestamp,
                ),
            )
    except sqlite3.IntegrityError:
        return {
            "booking_id": "",
            "status": "slot_unavailable",
            "summary": "That time was just taken.",
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
    try:
        with _connect() as database:
            booking = database.execute(
                "SELECT status FROM bookings WHERE booking_id = ? AND customer_id = ?",
                (booking_id, customer_id),
            ).fetchone()
            if not booking or booking["status"] != "booked":
                return {
                    "booking_id": booking_id,
                    "status": "not_found",
                    "summary": "No active matching booking was found.",
                }
            database.execute(
                "UPDATE bookings SET service = ?, slot_id = ?, start_time = ?, "
                "updated_at = ? WHERE booking_id = ?",
                (service, slot_id, f"{parts[0]}T{parts[2]}:00", _now(), booking_id),
            )
    except sqlite3.IntegrityError:
        return {
            "booking_id": booking_id,
            "status": "slot_unavailable",
            "summary": "That time was just taken.",
        }
    return {"booking_id": booking_id, "status": "modified", "summary": "Booking updated."}


def cancel_booking(customer_id, booking_id, confirmed=False):
    if confirmed is not True:
        return {
            "booking_id": booking_id,
            "status": "not_confirmed",
            "summary": "The booking change was not confirmed.",
        }
    with _connect() as database:
        booking = database.execute(
            "SELECT status FROM bookings WHERE booking_id = ? AND customer_id = ?",
            (booking_id, customer_id),
        ).fetchone()
        if not booking:
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
        database.execute(
            "UPDATE bookings SET status = 'cancelled', updated_at = ? WHERE booking_id = ?",
            (_now(), booking_id),
        )
    return {"booking_id": booking_id, "status": "cancelled", "summary": "Booking cancelled."}


def record_complaint(customer_id, summary, requested_resolution=""):
    clean_summary = " ".join(str(summary).split())
    if not clean_summary:
        return {"complaint_id": "", "status": "invalid"}
    complaint_id = f"cmp_{uuid4().hex[:12]}"
    try:
        with _connect() as database:
            database.execute(
                "INSERT INTO complaints VALUES (?, ?, ?, ?, ?)",
                (
                    complaint_id,
                    customer_id,
                    clean_summary,
                    " ".join(str(requested_resolution).split()),
                    _now(),
                ),
            )
    except sqlite3.IntegrityError:
        return {"complaint_id": "", "status": "customer_not_found"}
    return {"complaint_id": complaint_id, "status": "recorded"}


def _demo():
    global _DB_PATH
    original_path = _DB_PATH
    assert original_path.parent != Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory() as directory:
        _DB_PATH = Path(directory) / "salon.db"

        for phone in ("123456", "1234567", "123456789"):
            invalid = find_or_create_customer("Alex Morgan", phone)
            assert invalid["status"] == "invalid" and not invalid["customer_id"]
            assert not _DB_PATH.exists()

        assert _normalize_phone("(555) 010-1010") == "5550101010"
        assert _normalize_phone("123456789012345") == "123456789012345"
        assert _normalize_phone("1234567890123456") == ""
        created = find_or_create_customer("Alex Morgan", "+1 555 010 1010")
        assert _DB_PATH.stat().st_mode & 0o777 == 0o600
        repeated = find_or_create_customer("  alex   MORGAN ", "15550101010")
        mismatch = find_or_create_customer("Someone Else", "+1 555 010 1010")
        assert created["status"] == "created"
        assert repeated["customer_id"] == created["customer_id"]
        assert mismatch["status"] == "name_mismatch" and not mismatch["customer_id"]
        assert mismatch["summary"] == "The supplied details could not be verified together."

        with ThreadPoolExecutor(max_workers=16) as pool:
            for suffix in range(20, 30):
                concurrent = list(
                    pool.map(
                        lambda _: find_or_create_customer(
                            "Jamie Lee", f"+1 555 010 20{suffix}"
                        ),
                        range(16),
                    )
                )
                assert {result["status"] for result in concurrent} <= {
                    "created",
                    "existing",
                }
                assert len({result["customer_id"] for result in concurrent}) == 1

        current_date = get_current_date()["date"]
        assert current_date == _booking_today().isoformat()
        first_date = (date.fromisoformat(current_date) + timedelta(days=1)).isoformat()
        second_date = (date.fromisoformat(current_date) + timedelta(days=2)).isoformat()
        first_slot = check_availability("haircut", first_date)["slots"][0]["slot_id"]
        omitted_create = create_booking(created["customer_id"], "haircut", first_slot)
        false_create = create_booking(
            created["customer_id"], "haircut", first_slot, confirmed=False
        )
        assert omitted_create["status"] == false_create["status"] == "not_confirmed"
        unverified = create_booking(
            "cus_missing", "haircut", first_slot, confirmed=True
        )
        assert unverified["status"] == "customer_not_found"
        with _connect() as database:
            assert database.execute("SELECT COUNT(*) FROM bookings").fetchone()[0] == 0

        booking = create_booking(
            created["customer_id"], "haircut", first_slot, confirmed=True
        )
        active = list_bookings(created["customer_id"])["bookings"]
        assert len(active) == 1 and active[0]["booking_id"] == booking["booking_id"]
        second_slot = check_availability("haircut", second_date)["slots"][0]["slot_id"]
        omitted_modify = modify_booking(
            created["customer_id"], booking["booking_id"], "haircut", second_slot
        )
        false_modify = modify_booking(
            created["customer_id"],
            booking["booking_id"],
            "haircut",
            second_slot,
            confirmed=False,
        )
        assert omitted_modify["status"] == false_modify["status"] == "not_confirmed"
        assert list_bookings(created["customer_id"])["bookings"] == active
        changed = modify_booking(
            created["customer_id"],
            booking["booking_id"],
            "haircut",
            second_slot,
            confirmed=True,
        )
        assert booking["status"] == "booked"
        assert changed["status"] == "modified"
        active = list_bookings(created["customer_id"])["bookings"]
        omitted_cancel = cancel_booking(created["customer_id"], booking["booking_id"])
        false_cancel = cancel_booking(
            created["customer_id"], booking["booking_id"], confirmed=False
        )
        assert omitted_cancel["status"] == false_cancel["status"] == "not_confirmed"
        assert list_bookings(created["customer_id"])["bookings"] == active
        cancelled = cancel_booking(
            created["customer_id"], booking["booking_id"], confirmed=True
        )
        assert cancelled["status"] == "cancelled"
        assert list_bookings(created["customer_id"])["bookings"] == []
        complaint = record_complaint(created["customer_id"], "My cut was uneven.")
        assert complaint["status"] == "recorded"
        with _connect() as database:
            owner = database.execute(
                "SELECT customer_id FROM complaints WHERE complaint_id = ?",
                (complaint["complaint_id"],),
            ).fetchone()
        assert owner["customer_id"] == created["customer_id"]
    _DB_PATH = original_path
    print("salon SQLite check passed")


if __name__ == "__main__":
    _demo()
