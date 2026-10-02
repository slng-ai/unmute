"""A field with an alias, for the schema reader's tests."""

from pydantic import BaseModel, Field


class State(BaseModel):
    """A field with an alias."""

    phone: str = Field("", alias="phoneNumber")
