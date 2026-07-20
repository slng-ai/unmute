from datetime import date as calendar_date

_SERVICES = {"haircut", "hair-color", "blowout"}
_TIMES = ("09:00", "11:30", "15:00")


def check_availability(service, date):
    if service not in _SERVICES:
        raise ValueError("service must be haircut, hair-color, or blowout")
    try:
        requested_date = calendar_date.fromisoformat(date)
    except ValueError as error:
        raise ValueError("date must use YYYY-MM-DD") from error
    if requested_date.weekday() == 6:
        return {"slots": []}
    return {
        "slots": [
            {
                "slot_id": f"{date}_{service}_{time.replace(':', '')}",
                "start_time": f"{date}T{time}:00-05:00",
            }
            for time in _TIMES
        ]
    }
