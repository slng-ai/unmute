# Select an appointment

For a new booking or reschedule, ask for `haircut`, `hair-color`, or `blowout`
and a preferred date. Call `check_availability`, offer only returned slots, and
confirm one with the caller.

For a cancellation, don't call a tool. Return empty strings for service, slot
ID, and start time so the next task can cancel the existing appointment.
