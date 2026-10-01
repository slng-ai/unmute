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

from pydantic_ai import Agent
from pydantic_ai.messages import (
    FunctionToolResultEvent,
    ModelMessage,
    ModelRequest,
    ModelResponse,
    PartDeltaEvent,
    PartStartEvent,
    TextPart,
    TextPartDelta,
    UserPromptPart,
)
from pydantic_ai.models.openai import (
    OpenAIChatModel,
    OpenAIChatModelSettings,
)
from pydantic_ai.providers.openai import OpenAIProvider

from tools.opening_hours import opening_hours as read_hours

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
        # A question is not a goodbye. Measured 2026-10-01: after a hold the
        # model answered one and called this in the same reply, in 19 of 90
        # runs, and a prompt line alone only took that to 5 of 60.
        if session.history[-1]["content"].rstrip().endswith("?"):
            return "The caller just asked a question, so the call goes on. Answer it and do not say goodbye."
        session.end("caller_done")
        return "The call ends after this reply."

    @agent.tool_plain
    def hold_the_line() -> str:
        """Put the caller on a short hold when they ask you to hold on or wait."""
        session.end("hold")
        return "The hold starts after this reply. Tell the caller in a few words that you are putting them on hold."

    return agent


def next_twiml(handoff: Any) -> str | None:
    """What the call does after this session. A hold says one line with
    Twilio's own voice and hands the caller back; anything else hangs up."""
    if handoff.reason == "hold":
        return handoff.resume(
            "a short hold",
            before='<Say>Please hold.</Say><Pause length="2"/>',
            greeting="Thanks for holding. What else can I help with?",
        )
    return None


# The tools that end this session. After one, the reply is the last thing said.
SESSION_ENDERS = ("end_call", "hold_the_line")


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
    # run_stream_events, not run_stream: run_stream takes the first text as the
    # final answer, so "Goodbye." followed by end_call in the same response
    # never ran the tool. The events run the whole loop, tools included, and
    # still hand over each piece of text as it arrives.
    agent = session.state["agent"]
    spoke = False
    async with agent.run_stream_events(last["content"], message_history=to_messages(earlier)) as events:
        async for event in events:
            piece = ""
            if isinstance(event, PartStartEvent) and isinstance(event.part, TextPart):
                piece = event.part.content
            elif isinstance(event, PartDeltaEvent) and isinstance(event.delta, TextPartDelta):
                piece = event.delta.content_delta
            elif isinstance(event, FunctionToolResultEvent) and event.part.tool_name in SESSION_ENDERS and spoke:
                # The goodbye is said and the session ends after it. Leaving
                # the loop stops the run, so the model is not asked for a
                # second goodbye. With nothing said yet, it gets one more round.
                return
            if piece:
                spoke = True
                yield piece
