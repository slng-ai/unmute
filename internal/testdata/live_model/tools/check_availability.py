"""Sage and Stone's diary: a mock that always offers three fixed slots."""

from typing import Annotated, Literal

from pydantic import BaseModel, Field

_TIMES = ("09:00", "11:30", "15:00")


class Slot(BaseModel):
    """One free slot."""

    slot_id: str
    start_time: str


class Slots(BaseModel):
    """The free slots for one service and date."""

    slots: list[Slot]


def check_availability(
    service: Literal["haircut", "hair-color", "blowout"],
    date: Annotated[str, Field(description="Preferred date in YYYY-MM-DD form")],
) -> Slots:
    """List Sage and Stone slots for one service and date.

    Call only after customer identification succeeds. This tool accepts only
    service and date.
    """
    return Slots(
        slots=[
            Slot(slot_id=f"{date}_{service}_{time.replace(':', '')}", start_time=f"{date}T{time}:00-05:00")
            for time in _TIMES
        ]
    )
