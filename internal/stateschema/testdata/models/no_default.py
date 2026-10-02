"""A field with no default, for the schema reader's tests."""

from pydantic import BaseModel


class State(BaseModel):
    """A field with no default."""

    phone: str
