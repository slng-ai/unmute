"""A frozen state, for the schema reader's tests."""

from pydantic import BaseModel, ConfigDict


class State(BaseModel):
    """A frozen state."""

    model_config = ConfigDict(frozen=True)

    phone: str = ""
