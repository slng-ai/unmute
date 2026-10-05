"""The call state of history_core: every value its agents and tasks share."""

from pydantic import BaseModel


class State(BaseModel):
    """What one call knows, shared by every agent and task in it."""

    caller_phone: str = ""
    verified: bool = False
