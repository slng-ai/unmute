"""What one Wasla call knows: one Pydantic model, typed field by field.

Only the holder's name is read before the greeting. The account's details are saved by
the ``identify`` step, from a date-of-birth check that matched. Until then a prompt that
names ``{{state.merchant}}`` reads it as empty, so a relative who picks up hears nothing
about the account.
"""

from datetime import date, time
from typing import Annotated, Literal

from pydantic import BaseModel, StringConstraints

ConcernKind = Literal["hardship", "legal-matter", "purchase-dispute", "refusal-to-pay"]
"""What a holder raised. Each is said as words: "a purchase dispute"."""

AccountId = Annotated[str, StringConstraints(pattern=r"^WA-\d{4}$")]
"""An account id: WA and four digits."""


class State(BaseModel):
    """One reminder call.

    Attributes:
        account_id: The account the call is about, like ``WA-1001``. The dispatch's
            ``call_start`` must carry it.
        first_name: The holder's first name, read before the greeting.
        last_name: The holder's last name.
        amount_words: The overdue amount in words, saved once the date of birth matched.
        merchant: Where the purchase was made.
        purchase_spoken: The day of the purchase, in words.
        due_spoken: The day it was due, in words.
        today: Today on the Dubai clock.
        weekday: Today's weekday.
        deadline: The last day a promise may name: today plus ten days.
        deadline_spoken: The deadline in words.
        birth_date: The holder's date of birth, confirmed by the ``identify`` step.
        said: The line code says once the holder agreed to talk.
        day: The day a promise or a callback names, read back before it is recorded.
        at: The time of a callback.
        when_said: When a recorded promise or callback happens, in words.
        callback_day: A callback day the main agent already heard.
        callback_time: A callback time the main agent already heard.
        concern: What the holder raised, when they cannot promise.
        concern_note: What they said about it, in their own words.
        outcome: The outcome code the call recorded last, like ``PTP``.
    """

    account_id: AccountId | None = None
    first_name: str | None = None
    last_name: str | None = None
    amount_words: str | None = None
    merchant: str | None = None
    purchase_spoken: str | None = None
    due_spoken: str | None = None
    today: date | None = None
    weekday: str | None = None
    deadline: date | None = None
    deadline_spoken: str | None = None
    birth_date: date | None = None
    said: str | None = None
    day: date | None = None
    at: time | None = None
    when_said: str | None = None
    callback_day: date | None = None
    callback_time: time | None = None
    concern: ConcernKind | None = None
    concern_note: str | None = None
    outcome: str | None = None
