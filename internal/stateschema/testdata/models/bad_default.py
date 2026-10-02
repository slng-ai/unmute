"""A default its own type refuses, for the schema reader's tests."""

from typing import Literal

from pydantic import BaseModel


class State(BaseModel):
    """A default its own type refuses."""

    enquiry: Literal["new", "existing"] = ""  # ty: ignore[invalid-assignment]
