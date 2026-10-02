"""A model that holds itself, for the schema reader's tests."""

from pydantic import BaseModel


class Node(BaseModel):
    """A tree node."""

    children: list["Node"] = []


class State(BaseModel):
    """A state holding a tree."""

    root: Node | None = None
