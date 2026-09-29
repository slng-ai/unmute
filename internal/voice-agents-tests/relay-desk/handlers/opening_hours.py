_WEEKDAY = ("09:00", "17:00")
_HOURS = {
    "monday": _WEEKDAY,
    "tuesday": _WEEKDAY,
    "wednesday": _WEEKDAY,
    "thursday": _WEEKDAY,
    "friday": ("09:00", "15:00"),
}


def opening_hours(day):
    # ponytail: fixed table, read-only; a real desk would read a calendar.
    opens, closes = _HOURS.get(day, ("", ""))
    return {"day": day, "open": bool(opens), "opens": opens, "closes": closes}
