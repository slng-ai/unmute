"""A field that is a dict, for the schema reader's tests."""

from pydantic import BaseModel


class State(BaseModel):
    """A field that is a dict."""

    tags: dict[str, str] = {}
