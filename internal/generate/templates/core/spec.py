"""This agent's unmute-core objects, built from agent.json. Written by `unmute compile`.

agent.json holds what agent.yaml declares, in the library's own names. This file turns it
into the objects unmute-core runs, once per process: the tools, each agent's tasks, the
prefetch and the greeting. Every name, path and line is checked here, at import, so a
broken package fails when it starts and never during a call. Edit agent.yaml and compile
again rather than this file.
"""

import importlib
import json
import logging
import re
from collections.abc import Callable, Mapping
from pathlib import Path
from typing import Any

import unmute_core
from unmute_core.agent import AgentSpec
from unmute_core.call import CallStartError, save_call_start
from unmute_core.greeting import Greeting
from unmute_core.handoff import HandoffContext
from unmute_core.idle import IdleNudges
from unmute_core.prefetch import CallValue, Clock, Lookup, Prefetch
from unmute_core.settings import env_value
from unmute_core.slng import RouterConfig, RouterModelCall
from unmute_core.state import CallInfo, CallState
from unmute_core.tasks import OpeningTool, TaskPlan, TasksSpec
from unmute_core.telephony import PhoneLine, Transfer, transfer_tool
from unmute_core.tools import Assign, ToolRegistry, ToolReply, ToolSpec, local_tool

from state import State

unmute_core.require("{{.CorePin}}")

HERE = Path(__file__).parent
SPEC: dict[str, Any] = json.loads((HERE / "agent.json").read_text())
AGENT_ID: str = SPEC["agent_id"]
ENTRY: str = SPEC["entry"]
logger = logging.getLogger("spec")
PLACEHOLDER = re.compile(r"\{\{\s*([\w.]+)\s*\}\}")


def function(ref: str) -> Callable[..., Any]:
    """Return the function a ``tools/claims.py:lookup_policy`` reference names."""
    file, name = ref.split(":")
    return getattr(importlib.import_module(file.removesuffix(".py").replace("/", ".")), name)


def embedder(embed: Mapping[str, str]) -> Callable[[], Any]:
    """Return what builds a knowledge base's embedder: the class agent.json names."""

    def build() -> Any:
        """Build the embedder once its package is imported."""
        made = getattr(importlib.import_module(embed["module"]), embed["class"])
        return made(**{embed["kwarg"]: embed["model"]})

    return build


def build_knowledge() -> dict[str, Any]:
    """Return each knowledge base. ``start_knowledge`` reads and indexes them.

    The library's knowledge needs its ``knowledge`` extra, which the project installs
    only when agent.yaml declares a knowledge base, so it is imported only then.
    """
    if not SPEC.get("knowledge"):
        return {}
    from unmute_core.knowledge import KnowledgeBase

    bases = {}
    for name, base in SPEC["knowledge"].items():
        embeds = base["mode"] != "keyword"
        bases[name] = KnowledgeBase(
            name,
            HERE / base["documents"],
            mode=base["mode"],
            chunk_size=base["chunk_size"],
            chunk_overlap=base["chunk_overlap"],
            top_k=base["top_k"],
            min_score=base["min_score"],
            embed=embedder(base["embed"]) if embeds else None,
            credential_env=(base["embed"]["credential_env"] or None) if embeds else None,
        )
    return bases


KNOWLEDGE = build_knowledge()


def start_knowledge() -> None:
    """Read and index every knowledge base. Call it once per process, before a call."""
    for base in KNOWLEDGE.values():
        base.start()


def build_tools() -> ToolRegistry:
    """Return every tool of the package, with what each result saves."""
    tools = []
    for tool in SPEC["tools"]:
        if base := tool.get("knowledge"):
            tools.append(KNOWLEDGE[base].tool(**tool["options"]))
            continue
        options = dict(tool["options"])
        options["replies"] = [ToolReply(**reply) for reply in options.get("replies", ())]
        if precheck := tool.get("precheck"):
            options["precheck"] = function(precheck)
        tools.append(local_tool(function(tool["handler"]), **options))
    assign = {
        tool["options"]["name"]: [Assign(**row) for row in tool["assign"]]
        for tool in SPEC["tools"]
        if tool.get("assign")
    }
    return ToolRegistry(tools, state=State, assign=assign)


TOOLS = build_tools()
CONFIRM: dict[str, str] = SPEC["state"]["confirm"]


def build_agent(name: str) -> tuple[str, TaskPlan | None, ToolRegistry]:
    """Return one agent's prompt, its task plan or None, and the tools it may call."""
    agent = SPEC["agents"][name]
    tasks = agent.get("tasks")
    if tasks is None:
        return agent["instructions"], None, TOOLS.only(agent["tools"])
    reachable = [*agent["tools"], *tasks["task_only_tools"]]
    tools = TOOLS.only(reachable)
    plan = TaskPlan(TasksSpec.model_validate(tasks), state=State, confirm=CONFIRM, tools=tools)
    return agent["instructions"], plan, tools


AGENTS = {name: build_agent(name) for name in SPEC["agents"]}


def build_prefetch() -> Prefetch:
    """Return what the call reads before the greeting, in the order agent.yaml lists it."""
    entries: list[Clock | CallValue | Lookup] = []
    for entry in SPEC["prefetch"]:
        if entry["kind"] == "clock":
            entries.append(Clock(entry["name"], entry["timezone"], entry["assign"]))
        elif entry["kind"] == "call_value":
            entries.append(CallValue(entry["name"], entry["source"], entry["assign"]))
        else:
            entries.append(
                Lookup(
                    entry["name"],
                    TOOLS.get(entry["tool"]),
                    entry["assign"],
                    inputs=entry.get("inputs", ()),
                    needs_confirmed=entry.get("needs_confirmed", ()),
                    background=entry.get("background", False),
                )
            )
    return Prefetch(entries, state=State)


PREFETCH = build_prefetch()
GREETING = Greeting(**SPEC["greeting"]) if "greeting" in SPEC else Greeting()
GREETING.check(PREFETCH, SPEC["state"]["call_start"])
IDLE = IdleNudges(**SPEC["idle"]) if "idle" in SPEC else None
MAX_DURATION_S: float | None = SPEC.get("max_duration_s")


async def new_call(
    call: CallInfo | None = None, call_start: Mapping[str, object] | None = None
) -> CallState[State]:
    """Return a fresh call state, with its start values saved and its prefetch read in.

    Args:
        call: What the line carries. A web call's when None.
        call_start: The dispatch's ``call_start`` values, when there are some.

    Raises:
        CallStartError: A required start value is missing, or one does not fit, or a
            lookup with ``refuse_call`` found nothing.
    """
    rules = SPEC["state"]
    state = CallState(
        State,
        confirm=rules["confirm"],
        call_start=rules["call_start"],
        dependencies=rules["dependencies"],
        call=call or CallInfo(agent_id=AGENT_ID),
    )
    save_call_start(state, call_start)
    await PREFETCH.run(state)
    for entry in SPEC["prefetch"]:
        line = entry.get("refuse_call")
        if line and all(state.plain(name) in (None, "", []) for name in entry["assign"]):
            raise CallStartError(PLACEHOLDER.sub(lambda match: str(state.plain(match[1])), line))
    return state


def from_env(value: Any) -> Any:
    """Return ``value`` with each ``{"env": NAME}`` replaced by that variable's value."""
    if isinstance(value, dict):
        if value.keys() == {"env"}:
            found = env_value(value["env"], str)
            if not found:
                raise RuntimeError(f"Set {value['env']} in the environment or in .env")
            return found
        return {key: from_env(item) for key, item in value.items()}
    if isinstance(value, list):
        return [from_env(item) for item in value]
    return value


def router(binding: Mapping[str, Any], call: CallInfo) -> RouterModelCall:
    """Return the call's SLNG Context Router, from a think binding whose provider is slng.

    The router groups requests by the call's agent and session ids. Needs ``SLNG_API_KEY``.
    """
    return RouterModelCall(
        call,
        model=binding["model"],
        world_part=binding.get("world_part"),
        config=RouterConfig.model_validate(from_env(binding["config"])),
        pure_proxy=binding.get("pure_proxy", False),
        body=binding.get("body"),
    )


def entry(state: CallState) -> str:
    """Return the agent the call starts with: by ``entry_by``'s State value, else the entry."""
    rule = SPEC.get("entry_by")
    if rule is None:
        return ENTRY
    return rule["agents"].get(str(state.plain(rule["value"])), ENTRY)


TIMEZONE: str | None = next(
    (entry["timezone"] for entry in SPEC["prefetch"] if entry["kind"] == "clock"), None
)
"""Where the business is: the zone of the package's clock prefetch, or None."""


def transfers(name: str, line: PhoneLine) -> list[ToolSpec]:
    """Return one agent's transfers to a person, on this call's phone line.

    On a call with no phone leg, like a browser call, the model reads that there is no
    phone caller to transfer. A transfer whose number is not set is left out, with a
    warning: a browser session needs no phone route to start.
    """
    built = []
    for transfer in SPEC["agents"][name].get("transfers", ()):
        rule = SPEC["transfers"][transfer]
        destination = env_value(rule["destination_env"], str)
        if not destination:
            logger.warning("%s: set %s to offer it", transfer, rule["destination_env"])
            continue
        person = Transfer(
            transfer,
            destination,
            description=rule["description"],
            mode=rule["mode"],
            briefing=rule.get("briefing", ""),
            ring_timeout_s=rule.get("ring_timeout_s"),
            on_unavailable=rule["on_unavailable"],
        )
        built.append(transfer_tool(person, line))
    return built


def agent_spec(name: str, line: PhoneLine | None = None) -> AgentSpec:
    """Return one agent's ``AgentSpec``: its prompt and its tasks, or its tools.

    Lines code speaks say dates in the language of the agent's voice, and name a day
    from today on the clock prefetch's zone. An agent with transfers gets them on
    ``line``, so its tools are built for this call.
    """
    instructions, plan, tools = AGENTS[name]
    agent = SPEC["agents"][name]
    if line is not None and (extra := transfers(name, line)):
        tools = tools.with_tools(*extra)
        if plan is not None:
            tasks = TasksSpec.model_validate(agent["tasks"])
            plan = TaskPlan(tasks, state=State, confirm=CONFIRM, tools=tools)
    common: dict[str, Any] = {
        "name": name,
        "handoffs": tuple(agent.get("handoffs", ())),
        "language": SPEC["models"]["speak"][agent["voice"]].get("language", "en"),
        "timezone": TIMEZONE,
    }
    if plan is not None:
        return AgentSpec(instructions, tasks=plan, **common)
    return AgentSpec(instructions, tools=tools, **common)


class Handoff:
    """One handoff tool: where it sends the call, and what the call carries over.

    Attributes:
        name: The tool's name, like ``to_spanish``.
        to: The agent that takes the call.
        description: When the model calls it.
        announce: A line said first, or None.
        context: What the next agent hears of the call so far.
        saves: State values saved as the call moves.
        opens_with: A tool of the next agent's own, run in code as it takes the call.
    """

    def __init__(self, name: str, spec: Mapping[str, Any]) -> None:
        """Read one handoff from agent.json."""
        self.name = name
        self.to: str = spec["to"]
        self.description: str = spec["description"]
        self.announce: str | None = spec.get("announce")
        self.context = HandoffContext(
            history=spec["history"],
            max_messages=spec.get("max_messages"),
            include_tool_calls=spec.get("include_tool_calls", True),
        )
        self.saves: dict[str, object] = dict(spec.get("saves", {}))
        opens = spec.get("opens_with")
        self.opens_with = OpeningTool(**opens) if opens else None


HANDOFFS = {name: Handoff(name, handoff) for name, handoff in SPEC["handoffs"].items()}
