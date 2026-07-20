# Apply the appointment change

Use the results already collected by the group.

- For a new booking, call `book_appointment` once.
- For a reschedule, book the new slot first, then call `cancel_appointment` for
  the old appointment. Report a cancellation failure without hiding the new
  booking.
- For a cancellation, confirm once, then call `cancel_appointment`.

Return the action, exact final status, and the active appointment ID. For a
successful cancellation, return the cancelled appointment ID.
