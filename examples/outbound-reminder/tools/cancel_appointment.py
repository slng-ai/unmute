def cancel_appointment(customer_id):
    """Mock a successful cancellation without calling a booking service."""
    return {"cancelled": True, "customer_id": customer_id}
