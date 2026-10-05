"""A field that may be two types, for the schema reader's tests."""

from pydantic import BaseModel


class State(BaseModel):
    """A field that may be two types."""

    either: int | str = 0
