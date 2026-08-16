def reschedule_appointment(customer_id, new_time):
    """Mock a successful reschedule without calling a booking service."""
    return {"rescheduled": True, "customer_id": customer_id, "new_time": new_time}
