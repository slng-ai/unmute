"""The call state of slng_mcp_server: every value its agents and tasks share."""

from pydantic import BaseModel


class State(BaseModel):
    """What one call knows, shared by every agent and task in it."""

    customer_name: str = "there"
    customer_id: str = "unknown"
