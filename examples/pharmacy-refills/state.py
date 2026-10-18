"""What a pharmacy-refills call keeps: nothing yet.

The realtime model holds the conversation itself, and the tools answer from the
pharmacy's own records, so no value needs saving between turns. `agent.yaml`
still reads a State class from this file.
"""

from pydantic import BaseModel


class State(BaseModel):
    """One call. It holds no values."""
