from datetime import date as calendar_date
from hashlib import sha256
import re

_SERVICES = {"haircut", "hair-color", "blowout"}
_TIMES = {"0900", "1130", "1500"}


def book_appointment(customer_id, service, slot_id):
    if not re.fullmatch(r"cus_(1001|[0-9a-f]{8})", customer_id):
        raise ValueError("unknown customer_id")
    if service not in _SERVICES:
        raise ValueError("unknown service")
    try:
        slot_date, slot_service, slot_time = slot_id.split("_", 2)
        requested_date = calendar_date.fromisoformat(slot_date)
    except ValueError as error:
        raise ValueError("unknown slot_id") from error
    if (
        slot_service != service
        or slot_time not in _TIMES
        or requested_date.weekday() == 6
    ):
        raise ValueError("unknown slot_id")
    digest = sha256(f"{customer_id}:{slot_id}".encode()).hexdigest()[:10]
    return {"appointment_id": f"apt_{digest}", "status": "booked"}
