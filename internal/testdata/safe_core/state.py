"""The call state of safe_core: every value its agents and tasks share."""

from pydantic import BaseModel


class State(BaseModel):
    """What one call knows, shared by every agent and task in it."""

    customer_id: str | None = None
    verified: bool = False
