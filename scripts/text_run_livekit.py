"""Drive a compiled LiveKit agent through a scripted text conversation.

The emitted agent runs inside LiveKit's own test harness (AgentSession.run) with
the package's real think model and its real local tools, and no STT or TTS. After
every caller line the script prints the events (messages, tool calls, tool
outputs, handoffs), the active agent, that agent's complete rendered prompt,
and the declared state. It is the layer a prompt or a seam defect lives
in, so it is where to reproduce one before asking anybody for a call.

Run it from the repository root, inside the emitted project's own environment so
the pinned framework and plugins are the ones the deployment uses:

    uv run --project <package>/build/livekit python scripts/text_run_livekit.py \
        <package> --line "Hi, I'd like a haircut tomorrow afternoon." --line "Yes."

The package's .env is loaded by the emitted module itself. Nothing here prints a
value from it. A carrier's caller id is seeded the way `unmute dev --source`
seeds one, with a reserved test number unless --from-number says otherwise.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
from pathlib import Path


def parse() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument("package", help="the package directory, compiled for livekit")
    parser.add_argument(
        "--line", action="append", default=[], help="one caller turn; repeat in order"
    )
    parser.add_argument(
        "--from-number",
        default="+15005550006",
        help="the caller id the pre-fetch reads (E.164)",
    )
    parser.add_argument(
        "--model",
        default=None,
        help="think model override; default is the package's openai binding",
    )
    return parser.parse_args()


def describe(item) -> str:
    kind = getattr(item, "type", type(item).__name__)
    if kind == "message":
        return f"[{item.role}] {item.text_content!r}"
    if kind == "function_call":
        return f"[call] {item.name}({item.arguments})"
    if kind == "function_call_output":
        out = (
            item.output
            if isinstance(item.output, str)
            else json.dumps(item.output, ensure_ascii=False)
        )
        return f"[result] {item.name}: {out[:300]}"
    if kind == "agent_handoff":
        return f"[handoff] -> {type(item.new_agent).__name__}"
    return f"[{kind}] {item!r}"[:300]


def events_of(result) -> list:
    events = getattr(result, "events", None)
    if events is not None:
        return list(events)
    out, i = [], 0
    while True:
        try:
            out.append(result.expect[i].event())
        except (IndexError, AssertionError):
            return out
        i += 1


def prompt_of(agent_obj) -> str:
    return (getattr(agent_obj, "instructions", "") or "").replace("\n", " | ")


async def settle(session, timeout: float = 60.0) -> None:
    """Wait until the agent has replied and is listening again."""
    quiet = 0
    for _ in range(int(timeout / 0.1)):
        await asyncio.sleep(0.1)
        quiet = quiet + 1 if session.agent_state == "listening" else 0
        if quiet >= 10:
            return
    raise TimeoutError("the session did not settle after the handoff")


def state_of(userdata) -> str:
    public = {
        name: value
        for name, value in vars(userdata).items()
        if not name.startswith("_")
    }
    public["_unconfirmed"] = sorted(getattr(userdata, "_unconfirmed", set()))
    return json.dumps(public, ensure_ascii=False, default=str)


def seed_unused_env(package: Path, build: Path) -> None:
    """Fill the env this run never reaches, so only the model key is needed.

    The emitted module checks every name in its compile report at import and
    exits naming the ones missing. A scripted text run reaches no STT, no TTS,
    no telephony, no tracing backend and no Redis, so demanding real values for
    those means hand-writing a fifteen-line .env before the one question this
    script answers can be asked. The smoke runners already seed placeholders
    the same way.

    Only names nothing else supplies. The emitted module calls load_dotenv()
    without override, so anything seeded here would beat the package's own .env:
    a placeholder set on top of a real key would send the run at a provider with
    a credential that cannot work, and the failure would look like the provider
    refusing the key. Names are read from the .env, values never are.
    """
    report = build / "compile-report.json"
    if not report.exists():
        return
    supplied = set()
    dotenv = package / ".env"
    if dotenv.exists():
        for line in dotenv.read_text().splitlines():
            name = line.split("=", 1)[0].strip()
            if name and not name.startswith("#"):
                supplied.add(name)
    required = json.loads(report.read_text()).get("required_env", [])
    seeded = [
        name
        for name in required
        if name not in os.environ and name not in supplied
    ]
    for name in seeded:
        os.environ[name] = "text-run-placeholder"
    if seeded:
        # Said out loud, because a placeholder that looks like a credential is
        # how somebody concludes a provider is broken.
        print(f"seeded placeholders for {len(seeded)} unused variables: {', '.join(seeded)}")


async def run(args: argparse.Namespace) -> None:
    package = Path(args.package).resolve()
    build = package / "build" / "livekit"
    if not (build / "agent.py").exists():
        sys.exit(f"{build} holds no agent.py; compile the package for livekit first")
    sys.path.insert(0, str(build))
    os.chdir(build)
    os.environ["UNMUTE_CALL_FACTS"] = json.dumps({"from_number": args.from_number})
    seed_unused_env(package, build)

    import agent as generated  # noqa: PLC0415 - after sys.path and cwd are set
    from livekit.agents import AgentSession  # noqa: PLC0415
    from livekit.plugins import openai  # noqa: PLC0415

    entry = getattr(generated, "ENTRY_AGENT_CLASS", None)
    if entry is None:
        # The entry agent is the one class whose constructor takes `initial`.
        import inspect  # noqa: PLC0415

        def takes_initial(cls) -> bool:
            # getmembers returns everything the module imported too, and some of
            # those are C-level types inspect.signature refuses outright: a
            # package declaring an EmailStr imports email_validator, whose
            # EmailNotValidError raised ValueError here and killed the run before
            # the first turn. The entry agent is defined in the generated module,
            # so anything from elsewhere is not it.
            if getattr(cls, "__module__", None) != generated.__name__:
                return False
            try:
                return "initial" in inspect.signature(cls).parameters
            except (ValueError, TypeError):
                return False

        entry = next(cls for _, cls in inspect.getmembers(generated, inspect.isclass) if takes_initial(cls))
    model = args.model or _openai_model(package / "agent.yaml")
    llm = openai.LLM(
        api_key=os.environ["OPENAI_API_KEY"], model=model, reasoning_effort="none"
    )
    async with AgentSession(userdata=generated.Userdata(), llm=llm) as session:
        # A package that declares no `prefetch:` emits no _prefetch at all.
        prefetch = getattr(generated, "_prefetch", None)
        if prefetch is not None:
            await prefetch(session.userdata, None)
            print("prefetch:", state_of(session.userdata))
        # initial=False takes the handoff branch of on_enter, which opens with a
        # model turn rather than the greeting session.say would speak; there is
        # no TTS here to speak it.
        await session.start(entry(initial=False), capture_run=True)
        transfer = getattr(generated, "_TaskTransfer", None)
        said: list = []
        session.on("conversation_item_added", lambda ev: said.append(ev.item))
        for i, line in enumerate(args.line, 1):
            print(f"\n=== turn {i}: caller says {line!r}")
            del said[:]
            try:
                result = await session.run(user_input=line)
            except Exception as escaped:
                if transfer is None or not isinstance(escaped, transfer):
                    raise
                # A group step handed off. LiveKit's RunResult reads a task that
                # ended by handoff as a failed run, one hop before the receiving
                # agent takes over; a deployed worker has no RunResult and moves
                # the caller. Let the handoff land, then carry on.
                print(
                    "   [handoff] ->",
                    type(escaped.agent).__name__,
                    "(from a group step)",
                )
                await settle(session)
                result = None
                for item in said:
                    if getattr(item, "role", None) == "assistant":
                        print("  ", describe(item))
            for item in events_of(result) if result is not None else []:
                ev = (
                    item
                    if getattr(item, "type", None) == "agent_handoff"
                    else getattr(item, "item", item)
                )
                print("  ", describe(ev))
            current = session.current_agent
            print("   active agent:", type(current).__name__)
            print("   active prompt:", prompt_of(current))
            print("   state:", state_of(session.userdata))
        print("\n=== final state")
        print(state_of(session.userdata))


def _openai_model(agent_yaml: Path) -> str:
    import re  # noqa: PLC0415

    # Comment lines between the two keys are skipped. A `#` explaining why a
    # package is on the model it is on belongs next to the model, and without
    # this the pair stopped matching and the run refused a package that is a
    # direct openai one.
    found = re.search(
        r"provider: openai\n(?:[ \t]*#.*\n)*\s+model: (\S+)", agent_yaml.read_text()
    )
    if not found:
        sys.exit("the package's think binding is not a direct openai one; pass --model")
    # `unmute init` writes the id quoted and the salon packages write it bare.
    # Both are the same YAML; the quotes are not part of the model id, and sent
    # as one the provider answers "invalid model ID" rather than naming them.
    return found.group(1).strip("\"'")


if __name__ == "__main__":
    asyncio.run(run(parse()))
