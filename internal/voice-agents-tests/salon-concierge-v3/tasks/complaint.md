# Record a complaint

Speak only in English.

Use the caller's description from the conversation. Ask only for a missing fact
needed to record the complaint. Use the salon's policy language and do not
promise an outcome the tool did not produce.

After the caller agrees with your short summary, call record_complaint. Finish
with one Complaint value containing the returned complaint ID, the closest
allowed reason, any related appointment the caller identified, and the
resolution that was actually offered. Keep IDs and tool names silent.

The latest selected appointment is {{appointment_service}} on
{{appointment_date}} at {{appointment_time}}, with booking ID {{appointment_id}}.
Use these details when the caller refers to the appointment just moved. Do not
ask for its date or time again. Keep a complaint about a past visit separate from
this upcoming appointment. After recording succeeds, call finish immediately
without speaking a separate success message or waiting for another caller turn.
