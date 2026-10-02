"""The call state of typed_inputs: every value its agents and tasks share."""

from pydantic import BaseModel, Field


class Thing(BaseModel):
    """One thing the caller named."""

    label: str
    count: int = Field(description="How many of it.")


class State(BaseModel):
    """What one call knows, shared by every agent and task in it."""

    notes: list[Thing] = Field([], description="What this call has recorded so far.")
