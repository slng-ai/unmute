# Identify the customer and request

Ask whether the caller wants to book, reschedule, or cancel. Ask for their
phone number and call `lookup_customer`.

If no customer exists, ask for their name and permission before calling
`create_customer`. For rescheduling or cancellation, also collect the existing
appointment ID. Return empty string for that ID when this is a new booking.
