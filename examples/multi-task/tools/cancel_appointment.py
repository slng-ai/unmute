def cancel_appointment(appointment_id):
    # ponytail: mock tool, no validation — always cancels
    return {"appointment_id": appointment_id, "status": "cancelled"}
