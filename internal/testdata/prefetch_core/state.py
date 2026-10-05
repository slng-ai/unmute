"""The call state of prefetch_core: every value its agents and tasks share."""

from pydantic import BaseModel


class State(BaseModel):
    """What one call knows, shared by every agent and task in it."""

    customer_id: str = ""
    verified: bool = False
    booking_date: str = ""
    booking_weekday: str = ""
    booking_year: str = ""
    caller_phone: str = ""
    caller_name: str = ""
