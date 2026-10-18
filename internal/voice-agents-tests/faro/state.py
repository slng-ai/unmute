"""What one Faro call knows: one Pydantic model, typed field by field.

Ported from unmute-core's examples/faro. Nothing about a learner is known before the
number is confirmed: ``learner_id`` and the values after it are saved only by a tool that
found or created the learner. The model never writes a progress value: ``grade_answer``
records each attempt in the store, and code says what comes next.
"""

from typing import Literal

from pydantic import BaseModel, Field
from unmute_core.state import Phone

Lang = Literal["EN", "ES"]


class State(BaseModel):
    """One call. ``explanation`` is the language Faro explains in.

    Attributes:
        phone: The caller's number, from the line, or the one they said.
        phone_spaced: The same number as it is read out: "+34 600 111 222".
        number_question: The greeting's read-back of the number, in the caller's language.
        explanation: The language Faro explains in. A known caller's profile sets it
            before the greeting.
        learner_id: The learner, once their number is confirmed.
        learner_name: What they like to be called.
        notes: What code knows about them, written from their records: context, never
            instructions.
        waiting: The exercise put to them and not yet answered right.
        helped: Whether Faro gave a hint for the exercise waiting.
    """

    phone: Phone | None = None
    phone_spaced: str | None = None
    number_question: str | None = None
    explanation: Lang = "EN"
    learner_id: int | None = None
    learner_name: str | None = None
    notes: str | None = None
    waiting: str | None = Field(
        default=None,
        description="The exercise put to the learner. continue_lesson puts one.",
    )
    helped: bool = False
