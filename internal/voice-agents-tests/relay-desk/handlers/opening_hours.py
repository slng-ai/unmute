"""The front desk's opening hours, from a fixed table."""

from typing import Annotated, Literal

from pydantic import BaseModel, Field

Day = Literal["monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"]

_WEEKDAY = ("09:00", "17:00")
_HOURS = {
    "monday": _WEEKDAY,
    "tuesday": _WEEKDAY,
    "wednesday": _WEEKDAY,
    "thursday": _WEEKDAY,
    "friday": ("09:00", "15:00"),
}


class Hours(BaseModel):
    """One day's opening hours."""

    day: str
    open: bool
    opens: str = Field(description="Opening time as HH:MM, empty when closed.")
    closes: str = Field(description="Closing time as HH:MM, empty when closed.")


def opening_hours(
    day: Annotated[Day, Field(description="The day of the week the caller asked about.")],
) -> Hours:
    """Return the front desk's opening hours for one day of the week. Read-only."""
    # ponytail: fixed table, read-only; a real desk would read a calendar.
    opens, closes = _HOURS.get(day, ("", ""))
    return Hours(day=day, open=bool(opens), opens=opens, closes=closes)
