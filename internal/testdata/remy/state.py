"""The call state of remy: every value its agents and tasks share."""

from pydantic import BaseModel


class State(BaseModel):
    """What one call knows, shared by every agent and task in it."""

    caller_phone: str | None = None
