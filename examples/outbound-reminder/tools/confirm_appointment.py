def confirm_appointment(customer_id, dialed_number, channel):
    """Mock a successful confirmation without calling a booking service."""
    return {
        "confirmed": True,
        "customer_id": customer_id,
        "dialed_number": dialed_number,
        "channel": channel,
    }
