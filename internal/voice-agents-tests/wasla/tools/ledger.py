"""Wasla's collections ledger and the tools the agent calls on it.

Ported from unmute-core's examples/wasla. The ledger stands in for the lender's backend:
three invented accounts, and what each call records about them, in memory. The outcome
codes are the ones collections teams use: PTP (a promise to pay), CB (a callback), WN
(wrong number), and HARDSHIP, LEGAL, DISPUTE or RTP (a concern).

Every tool takes typed arguments and returns a typed result. The account id and the call's
id are injected by the package: the model never sends them. A write is keyed by the call,
so a second yes records nothing new.
"""

from datetime import date, datetime, time, timedelta
from decimal import Decimal
from threading import Lock
from typing import Annotated, Any, Literal
from zoneinfo import ZoneInfo

from pydantic import AfterValidator, BaseModel, Field
from state import ConcernKind
from unmute_core.state import BirthDate
from unmute_core.tools import ToolError

ZONE = ZoneInfo("Asia/Dubai")
WINDOW_DAYS = 10
TRIES = 3
CODES: dict[str, str] = {
    "hardship": "HARDSHIP",
    "legal-matter": "LEGAL",
    "purchase-dispute": "DISPUTE",
    "refusal-to-pay": "RTP",
}
SEED: tuple[dict[str, Any], ...] = (
    {
        "account_id": "WA-1001",
        "first_name": "Layla",
        "last_name": "Haddad",
        "birth_date": date(1990, 3, 14),
        "amount": Decimal("434.50"),
        "merchant": "Falcon Electronics",
        "purchase_date": date(2026, 8, 21),
        "due_date": date(2026, 9, 20),
    },
    {
        "account_id": "WA-1002",
        "first_name": "Omar",
        "last_name": "Khalil",
        "birth_date": date(1985, 11, 2),
        "amount": Decimal("1249.00"),
        "merchant": "Oasis Furniture",
        "purchase_date": date(2026, 8, 2),
        "due_date": date(2026, 9, 2),
    },
    {
        "account_id": "WA-1003",
        "first_name": "Sara",
        "last_name": "Nasser",
        "birth_date": date(1998, 7, 30),
        "amount": Decimal("89.75"),
        "merchant": "Saffron Fashion",
        "purchase_date": date(2026, 9, 12),
        "due_date": date(2026, 10, 12),
    },
)
SUPPORT = (
    "Wasla support is on eight hundred, nine two seven, five two, from nine AM to six PM, "
    "or by email at help at wasla dot a e."
)
CONCERN_SAID = {
    "hardship": (
        "I've passed that on. Please contact support with any documents, and they'll look at "
        f"it with you. {SUPPORT} Is there anything else you'd like to add?"
    ),
    "legal-matter": (
        "I've passed that to our internal team. They'll look into it and contact you within "
        "twenty-four hours. Is there anything else you'd like to add?"
    ),
    "purchase-dispute": (
        "I've recorded your dispute. Our team will look into the purchase and contact you; "
        "nothing more is asked of you until then. Is there anything else you'd like to add?"
    ),
    "refusal-to-pay": (
        "I've noted your answer and your feedback. Thank you for your time today. Is there "
        "anything else you'd like to add?"
    ),
}
ACCOUNTS: dict[str, dict[str, Any]] = {a["account_id"]: a for a in SEED}
RECORDS: dict[tuple[str, str], dict[str, Any]] = {}
WRONG_TRIES: dict[str, int] = {}
LOCK = Lock()

ONES = [
    "zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
    "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen",
    "eighteen", "nineteen",
]  # fmt: skip
TENS = ["twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"]


def number(n: int) -> str:
    """Say 0 to 999,999 in words: 434 is "four hundred thirty-four"."""
    if n < 20:
        return ONES[n]
    if n < 100:
        return TENS[n // 10 - 2] + (f"-{ONES[n % 10]}" if n % 10 else "")
    if n < 1000:
        return f"{ONES[n // 100]} hundred" + (f" {number(n % 100)}" if n % 100 else "")
    return f"{number(n // 1000)} thousand" + (f" {number(n % 1000)}" if n % 1000 else "")


def amount_in_words(value: Decimal) -> str:
    """Say an amount in UAE dirhams and fils, in words, never in digits."""
    dirhams, fils = divmod(int(value.quantize(Decimal("0.01")) * 100), 100)
    said = f"{number(dirhams)} dirham{'' if dirhams == 1 else 's'}"
    return f"{said} and {number(fils)} fils" if fils else said


def ordinal(n: int) -> str:
    """Say 1 as "1st", 22 as "22nd" and 13 as "13th"."""
    suffix = "th" if 10 <= n % 100 <= 20 else {1: "st", 2: "nd", 3: "rd"}.get(n % 10, "th")
    return f"{n}{suffix}"


def day_in_words(value: date) -> str:
    """Say a day as "Friday the 9th of October"."""
    return f"{value:%A} the {ordinal(value.day)} of {value:%B}"


def clock_in_words(value: time) -> str:
    """Say a time as "5 PM" or "5:30 PM"."""
    hour = value.hour % 12 or 12
    minutes = f":{value.minute:02d}" if value.minute else ""
    return f"{hour}{minutes} {'AM' if value.hour < 12 else 'PM'}"


def today() -> date:
    """Return today's date on the Dubai clock."""
    return datetime.now(ZONE).date()


WindowDay = Annotated[
    date, Field(description="The day, as YYYY-MM-DD, from today up to the deadline.")
]


class Opened(BaseModel):
    """What ``open_account`` returns: who the call is for, and the last day it may name."""

    first_name: str
    last_name: str
    deadline: date
    deadline_spoken: str


class Verified(BaseModel):
    """What ``verify_holder`` returns: the account's details only when the date matched."""

    status: Literal["verified", "mismatch", "locked"]
    birth_date: date | None = None
    tries_left: int = 0
    amount_words: str | None = None
    merchant: str | None = None
    purchase_spoken: str | None = None
    due_spoken: str | None = None


class GoodTime(BaseModel):
    """What ``good_time`` returns: whether to go on, and what code says next."""

    good_time: bool
    said: str


class Recorded(BaseModel):
    """What every write returns: the outcome code, its reference, and the words for it.

    ``past_day`` and ``after_deadline`` are the outcomes of ``outside_window``: nothing is
    written.
    """

    status: Literal[
        "PTP", "CB", "WN", "HARDSHIP", "LEGAL", "DISPUTE", "RTP", "past_day", "after_deadline"
    ]
    reference: str = ""
    when: str = ""


class Noted(BaseModel):
    """What ``note_concern`` returns: the receipt of one kind of concern."""

    status: Literal["HARDSHIP", "LEGAL", "DISPUTE", "RTP"]
    reference: str
    kind: ConcernKind
    said: str


class Guide(BaseModel):
    """What ``payment_policy`` returns."""

    policy: str


class Ended(BaseModel):
    """What ``end_call`` returns."""

    ended: bool


def outside_window(day: date) -> Recorded | None:
    """Refuse a day in the past or after the deadline, before it is read back for a yes."""
    if day < today():
        return Recorded(status="past_day")
    deadline = today() + timedelta(days=WINDOW_DAYS)
    if day > deadline:
        return Recorded(status="after_deadline", when=day_in_words(deadline))
    return None


def _account(account_id: str) -> dict[str, Any]:
    """Return one account, or refuse a call about an account that does not exist."""
    found = ACCOUNTS.get(account_id)
    if found is None:
        raise ToolError(f"no account {account_id}")
    return found


def _write(call: str, code: str, account_id: str, **fields: object) -> dict[str, Any]:
    """Record one outcome once per call: the same call and code again returns it."""
    with LOCK:
        key = (call, code)
        if key not in RECORDS:
            reference = f"{code}-{len(RECORDS) + 1:04d}"
            RECORDS[key] = {
                "code": code,
                "account_id": account_id,
                "reference": reference,
                **fields,
            }
        return RECORDS[key]


def open_account(account_id: str) -> Opened:
    """Read whose account a call is about, before the greeting. Nothing else of it."""
    found = _account(account_id)
    deadline = today() + timedelta(days=WINDOW_DAYS)
    return Opened(
        first_name=found["first_name"],
        last_name=found["last_name"],
        deadline=deadline,
        deadline_spoken=day_in_words(deadline),
    )


def verify_holder(
    account_id: str,
    birth_date: Annotated[
        BirthDate, Field(description="Their date of birth as YYYY-MM-DD. Never guess a part.")
    ],
) -> Verified:
    """Check the holder's date of birth. Call it as soon as they say it.

    It first reads the date back, so never read it back yourself. Call it again with the
    same date after their yes.
    """
    found = _account(account_id)
    with LOCK:
        tries = WRONG_TRIES.get(account_id, 0)
        if tries >= TRIES:
            return Verified(status="locked")
        if found["birth_date"] == birth_date:
            WRONG_TRIES.pop(account_id, None)
            return Verified(
                status="verified",
                birth_date=birth_date,
                amount_words=amount_in_words(found["amount"]),
                merchant=found["merchant"],
                purchase_spoken=day_in_words(found["purchase_date"]),
                due_spoken=day_in_words(found["due_date"]),
            )
        WRONG_TRIES[account_id] = tries = tries + 1
        if tries >= TRIES:
            return Verified(status="locked")
        return Verified(status="mismatch", tries_left=TRIES - tries)


def good_time(
    good_time: Annotated[bool, Field(description="Whether now is a good time for them to talk.")],
    amount_words: str,
    merchant: str,
) -> GoodTime:
    """Note whether now is a good time to talk about the account. Code says what follows."""
    if not good_time:
        return GoodTime(good_time=False, said="No problem at all.")
    return GoodTime(
        good_time=True,
        said=(
            "Thank you. This call is recorded for training and quality. I'm calling about "
            f"{amount_words} for your purchase from {merchant}, which is now overdue. Could "
            "you make the payment today?"
        ),
    )


def record_promise(
    day: WindowDay,
    account_id: str,
    call: str,
) -> Recorded:
    """Record the holder's promise to pay the full amount on a day.

    It first reads the promise back, so never read it back yourself. Call it again with
    the same day after their yes.
    """
    record = _write(call, "PTP", account_id, day=day.isoformat())
    when = "today" if day == today() else f"on {day_in_words(day)}"
    return Recorded(status="PTP", reference=record["reference"], when=when)


def schedule_callback(
    day: WindowDay,
    at: Annotated[time, Field(description="The time, HH:MM on a 24-hour clock.")],
    account_id: str,
    call: str,
) -> Recorded:
    """Note a day and time to call back. Call it once you have both.

    It first reads them back, so never read them back yourself. Call it again with the
    same values after their yes.
    """
    record = _write(call, "CB", account_id, day=day.isoformat(), at=at.isoformat())
    return Recorded(
        status="CB",
        reference=record["reference"],
        when=f"on {day_in_words(day)} at {clock_in_words(at)}",
    )


def mark_wrong_number(account_id: str, call: str) -> Recorded:
    """Take this number off the list, so it is not called again. Call it after their yes."""
    record = _write(call, "WN", account_id)
    return Recorded(status="WN", reference=record["reference"])


def note_concern(
    kind: Annotated[
        ConcernKind,
        Field(
            description=(
                "hardship: illness, disability, losing a job, money trouble. "
                "legal-matter: police, a lawyer, a regulator, a threat. "
                "purchase-dispute: they never made or got the purchase, or suspect "
                "fraud. refusal-to-pay: they will not pay in this period."
            )
        ),
    ],
    note: Annotated[
        str,
        AfterValidator(lambda text: text.strip().rstrip(".?! ")),
        Field(min_length=1, description="What they said about it, in their own words."),
    ],
    account_id: str,
    call: str,
) -> Noted:
    """Note what stops the holder from promising a payment, for the right team.

    It first reads the note back, so never read it back yourself. Call it again with the
    same values after their yes.
    """
    code = CODES[kind]
    record = _write(call, code, account_id, note=note.strip())
    return Noted(
        status=code, reference=record["reference"], kind=kind, said=CONCERN_SAID[kind]
    )  # ty: ignore[invalid-argument-type]


POLICY = """\
- How to pay: only in the Wasla app or on the Wasla website. No bank transfer, cash,
  prepaid card or account number is accepted. Never give one.
- Payment plans and new due dates: the account is overdue, and Wasla does not offer plans
  or reschedule an overdue payment. The full amount is paid in the app or on the website.
- Hardship (illness, disability, losing a job, suspected fraud): the holder contacts Wasla
  support with documents, and support looks at it with them.
- Support: eight hundred, nine two seven, five two, from nine AM to six PM, or email help
  at wasla dot a e. Read the number digit by digit.
"""


def payment_policy() -> Guide:
    """Read the payment policy: how to pay, plans, a new due date, hardship, support.

    Call it only when the holder asks about one of these, and answer from it.
    """
    return Guide(policy=POLICY)


def end_call() -> Ended:
    """End the call once it is over. Say your goodbye in the same reply."""
    return Ended(ended=True)
