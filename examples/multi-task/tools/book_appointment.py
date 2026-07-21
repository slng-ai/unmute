from hashlib import sha256


def book_appointment(customer_id, service, slot_id):
    # ponytail: mock tool, no validation — always books
    digest = sha256(f"{customer_id}:{slot_id}".encode()).hexdigest()[:10]
    return {"appointment_id": f"apt_{digest}", "status": "booked"}
