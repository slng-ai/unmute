"""This agent's unmute-core objects, built from agent.json. Written by `unmute compile`.

agent.json holds what agent.yaml declares, in the library's own names. This file turns it
into the objects unmute-core runs, once per process: the tools, each agent's tasks, the
prefetch and the greeting. Every name, path and line is checked here, at import, so a
broken package fails when it starts and never during a call. Edit agent.yaml and compile
again rather than this file.
"""

import importlib
import json
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
from unmute_core.state import CallInfo, CallState
from unmute_core.tasks import OpeningTool, TaskPlan, TasksSpec
from unmute_core.tools import Assign, ToolRegistry, ToolReply, local_tool

from state import State

unmute_core.require("{{.CorePin}}")

SPEC: dict[str, Any] = json.loads((Path(__file__).parent / "agent.json").read_text())
AGENT_ID: str = SPEC["agent_id"]
ENTRY: str = SPEC["entry"]
PLACEHOLDER = re.compile(r"\{\{\s*([\w.]+)\s*\}\}")


def function(ref: str) -> Callable[..., Any]:
    """Return the function a ``tools/claims.py:lookup_policy`` reference names."""
    file, name = ref.split(":")
    return getattr(importlib.import_module(file.removesuffix(".py").replace("/", ".")), name)


def build_tools() -> ToolRegistry:
    """Return every tool of the package, with what each result saves."""
    tools = []
    for tool in SPEC["tools"]:
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


def entry(state: CallState) -> str:
    """Return the agent the call starts with: by ``entry_by``'s State value, else the entry."""
    rule = SPEC.get("entry_by")
    if rule is None:
        return ENTRY
    return rule["agents"].get(str(state.plain(rule["value"])), ENTRY)


def agent_spec(name: str) -> AgentSpec:
    """Return one agent's ``AgentSpec``: its prompt and its tasks, or its tools."""
    instructions, plan, tools = AGENTS[name]
    handoffs = tuple(SPEC["agents"][name].get("handoffs", ()))
    if plan is not None:
        return AgentSpec(instructions, name=name, tasks=plan, handoffs=handoffs)
    return AgentSpec(instructions, name=name, tools=tools, handoffs=handoffs)


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
