"""The call state of hotel-concierge: every value its agents and tasks share."""

from pydantic import BaseModel, Field


class State(BaseModel):
    """What one call knows, shared by every agent and task in it."""

    hotel_name: str = Field(
        "Hotel Lumbre",
        description="The property this deployment answers for. Spoken in the greeting.",
    )
    neighbourhood: str = Field(
        "Sol, in the centre of Madrid",
        description="Where the hotel is, in words a places query can use.",
    )
    city: str = Field("Madrid", description="The city.")
    hotel_website: str = Field(
        "lumbre-hotels.example",
        description="The hotel's website, spoken on request without a scheme.",
    )
    caller_phone: str | None = Field(
        None,
        description="The guest's mobile number for a text message, in international format starting with a plus sign, recorded only after the guest has heard it read back and confirmed it.",
    )
