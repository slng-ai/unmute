"""The call state: every value the agents and tasks of one call share."""

from pydantic import BaseModel


class State(BaseModel):
    """What one call knows, shared by every agent and task in it."""
