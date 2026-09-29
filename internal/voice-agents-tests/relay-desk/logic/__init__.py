"""The relay desk's turn, as a very small Pydantic AI agent.

The generated app hands each caller turn to respond(session) and speaks what it
yields. It keeps the call itself: the Twilio protocol, interrupts, call slots
and shutdown. This agent calls the package's own think model, OpenAI or the
SLNG Context Router, whichever the target binds, and runs its own tools.
"""

import os
from collections.abc import AsyncIterator
from typing import Any, Literal

# Pydantic AI prints a banner at import unless told not to, and this runs in a
# phone app's log.
os.environ.setdefault("PYDANTIC_AI_NO_BANNER", "1")

from pydantic_ai import Agent  # noqa: E402 - after the banner switch
from pydantic_ai.messages import ModelMessage, ModelRequest, ModelResponse, TextPart, UserPromptPart  # noqa: E402
from pydantic_ai.models.openai import OpenAIChatModel, OpenAIChatModelSettings  # noqa: E402
from pydantic_ai.providers.openai import OpenAIProvider  # noqa: E402
from tools.opening_hours import opening_hours as read_hours  # noqa: E402

Day = Literal["monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"]


def build(session: Any) -> Agent:
    """One agent per call, on the model the package's think binding names."""
    m = session.model
    model = OpenAIChatModel(m.model, provider=OpenAIProvider(base_url=m.base_url, api_key=m.api_key))
    # The binding's params and the router's configuration both ride the body.
    settings = OpenAIChatModelSettings(extra_body={**m.params, **m.extra_body}, extra_headers=m.extra_headers)
    agent = Agent(model, name="relay_desk", instructions=session.instructions, model_settings=settings)

    @agent.tool_plain
    def opening_hours(day: Day) -> dict[str, Any]:
        """Return the front desk's opening hours for one day of the week."""
        return read_hours(day)

    @agent.tool_plain
    def end_call() -> str:
        """End the call once the caller says they are done."""
        session.end("caller_done")
        return "The call ends after this reply."

    @agent.tool_plain
    def hold_the_line() -> str:
        """Put the caller on a short hold when they ask you to hold on or wait."""
        session.end("hold")
        return "The caller hears a short hold message, then comes back to you."

    return agent


def next_twiml(handoff: Any) -> str | None:
    """What the call does after this session. A hold says one line with
    Twilio's own voice and hands the caller back; anything else hangs up."""
    if handoff.reason == "hold":
        return handoff.resume("a short hold", before='<Say>Please hold.</Say><Pause length="2"/>')
    return None


def to_messages(history: list[dict[str, str]]) -> list[ModelMessage]:
    """The app's history, which is what the caller said and heard, for Pydantic AI."""
    messages: list[ModelMessage] = []
    for entry in history:
        if entry["role"] == "user":
            messages.append(ModelRequest(parts=[UserPromptPart(content=entry["content"])]))
        else:
            messages.append(ModelResponse(parts=[TextPart(content=entry["content"])]))
    return messages


async def respond(session: Any) -> AsyncIterator[str]:
    """Stream the reply to the caller's last words."""
    if "agent" not in session.state:
        session.state["agent"] = build(session)
    # Rebuilt from the app's history every turn, so an interrupted reply
    # counts only as far as the caller heard it.
    *earlier, last = session.history
    async with session.state["agent"].run_stream(last["content"], message_history=to_messages(earlier)) as run:
        # debounce_by=None: each piece is spoken as it arrives, not batched.
        async for piece in run.stream_text(delta=True, debounce_by=None):
            yield piece
