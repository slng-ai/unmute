"""Return only the explicit arguments supplied to this fixture tool."""


def inspect_state(*, note, appointment, date, accepted, count, label):
    return {
        "note": note,
        "appointment": appointment,
        "date": date,
        "accepted": accepted,
        "count": count,
        "label": label,
    }
