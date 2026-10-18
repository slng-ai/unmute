"""What an order desk call knows."""

from pydantic import BaseModel, Field


class State(BaseModel):
    """One call. Every value starts empty."""

    order_status: str = Field("", description="The status look_up_order last returned.")
