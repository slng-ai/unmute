"""The call state of slng_memory_sms: every value its agents and tasks share."""

from pydantic import BaseModel, Field


class State(BaseModel):
    """What one call knows, shared by every agent and task in it."""

    customer_name: str = Field(
        "there", description="The caller's first name, supplied by the session."
    )
    caller_phone: str | None = Field(
        None,
        description="The caller's mobile number, recorded after the caller confirmed it.",
    )
