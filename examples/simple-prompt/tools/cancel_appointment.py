import re


def cancel_appointment(appointment_id):
    known = appointment_id == "apt_1001" or re.fullmatch(
        r"apt_[0-9a-f]{10}", appointment_id
    )
    return {
        "appointment_id": appointment_id,
        "status": "cancelled" if known else "not_found",
    }
