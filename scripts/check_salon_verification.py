"""Check salon verification re-entry with the real model and synthetic state.

Compile examples/salon-concierge for LiveKit, then run:
    uv run --project examples/salon-concierge/build/livekit \
        python scripts/check_salon_verification.py

Uses the package's direct OpenAI model. Makes paid model requests, without audio
or tracing. A saved identity must finish silently; an explicit phone correction
must still ask for confirmation.
"""

import asyncio
import os
import sys
from pathlib import Path

from text_run_livekit import _openai_model, describe, events_of


async def main():
    package = Path(__file__).resolve().parents[1] / "examples/salon-concierge"
    build = package / "build/livekit"
    sys.path.insert(0, str(build))
    os.chdir(build)

    import agent as generated  # noqa: PLC0415
    from livekit.agents import AgentSession, llm  # noqa: PLC0415
    from livekit.plugins import openai  # noqa: PLC0415

    for status, correction in (
        ("created", False),
        ("existing", False),
        ("created", True),
    ):
        state = generated.Userdata()
        generated._save_result(
            "verify_customer",
            state,
            {
                "customer_phone": "+15005550006",
                "customer_status": status,
            },
        )
        context = llm.ChatContext()
        context.add_message(
            role="user",
            content=(
                "Please use a different phone number: +15005550009."
                if correction
                else "Can we switch my appointment to another day around the same time?"
            ),
        )
        model = openai.LLM(
            model=_openai_model(package / "agent.yaml"), reasoning_effort="none"
        )
        async with AgentSession(userdata=state, llm=model) as session:
            task = generated.VerifyCustomer(chat_ctx=context)
            result = await session.start(task, capture_run=True)
            await result
            items = [getattr(event, "item", event) for event in events_of(result)]
            items = [
                item
                for item in items
                if item.type
                in (
                    "message",
                    "function_call",
                    "function_call_output",
                )
            ]
            for item in items:
                print(describe(item))
            calls = [item.name for item in items if item.type == "function_call"]
            speech = [
                item.text_content
                for item in items
                if item.type == "message" and item.role == "assistant"
            ]
            if correction:
                assert speech and not task.done(), (
                    "a changed number needs caller confirmation"
                )
                assert not calls, f"acted before the caller confirmed: {calls}"
            else:
                assert calls == ["finish"] and task.done(), calls
                assert not speech, f"verified caller was asked again: {speech}"
            assert state.customer_phone == "+15005550006"
            assert state.customer_status == status
            print(f"PASS: status={status}, phone_correction={correction}")


if __name__ == "__main__":
    asyncio.run(main())
