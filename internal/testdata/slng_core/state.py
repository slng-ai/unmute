"""The call state of slng_core: every value its agents and tasks share."""

from pydantic import BaseModel


class State(BaseModel):
    """What one call knows, shared by every agent and task in it."""

    customer_name: str = "there"
