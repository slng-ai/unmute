"""This agent on LiveKit Agents, on unmute-core. Written by `unmute compile`.

spec.py builds the agent from agent.json. This file builds the LiveKit side around it,
and each call runs the same way:

1. Read what the line carries: a SIP caller's numbers, or nothing on a browser call.
2. ``spec.new_call`` builds the call's state, with its prefetch read in. A dispatch that
   lacks a required ``call_start`` value, or holds a wrong one, ends the job before the
   greeting, and the log says which value.
3. The session gets the speech and the model agent.yaml names, and its turn pace.
4. ``UnmuteAgent`` runs the entry agent's ``AgentSpec``.
5. The greeting plays, and the idle nudges and the call's longest length start.

    uv run agent.py console   # talk through your microphone
    uv run agent.py dev       # a worker for your LiveKit project
"""

import json
import logging
import os
from typing import Any

from dotenv import load_dotenv
from google import genai
from livekit import rtc
from livekit.agents import (
    AgentServer,
    AgentSession,
    APIConnectOptions,
    FunctionTool,
    JobContext,
    JobProcess,
    RunContext,
    TurnHandlingOptions,
    cli,
    function_tool,
    inference,
)
from livekit.agents.beta.tools import EndCallTool
from livekit.agents.voice import room_io
from livekit.agents.voice.agent_session import SessionConnectOptions
from livekit.plugins import google, openai, silero, slng
from unmute_core.call import CallStartError
from unmute_core.livekit import (
    ModelCallLLM,
    SipLine,
    UnmuteAgent,
    endpointing,
    greet,
    idle_nudges,
    interruption,
    max_duration,
    sip_call_info,
    yield_to_caller,
)
from unmute_core.settings import env_value
from unmute_core.slng import TrackingIds, tracking_ids
from unmute_core.state import CallState, call_phone_region, use_phone_region
from unmute_core.telemetry import Langfuse, Telemetry
from unmute_core.turn import turn_window

import spec
import speech

load_dotenv()
logger = logging.getLogger("agent")
MODELS: dict[str, Any] = spec.SPEC["models"]
WINDOW = turn_window(MODELS["turn"].get("pace", "balanced"), "livekit")


def required(name: str) -> str:
    """Return an environment variable the agent cannot run without."""
    value = env_value(name, str)
    if not value:
        raise RuntimeError(f"Set {name} in the environment or in .env")
    return value


def vertex_client(api_key: str, location: str) -> genai.Client:
    """Return a GenAI client pinned to one Vertex location, with an API key.

    LiveKit's Google plugin reaches Vertex only with Google Cloud credentials. This client
    uses an API key instead, on the location's own host, so EU requests stay in the EU.
    """
    if location in ("us", "eu"):
        host = f"aiplatform.{location}.rep.googleapis.com"
    elif location == "global":
        host = "aiplatform.googleapis.com"
    else:
        host = f"{location}-aiplatform.googleapis.com"
    return genai.Client(
        vertexai=True,
        api_key=api_key,
        location=location,
        http_options=genai.types.HttpOptions(
            base_url=f"https://{host}/v1beta1",
            base_url_resource_scope=genai.types.ResourceScope.COLLECTION,
            api_version="v1beta1",
            headers={"X-Vertex-AI-LLM-Shared-Request-Type": "priority"},
        ),
    )


class VertexLLM(google.LLM):
    """LiveKit's Google LLM plugin, with its client swapped for a Vertex API-key one."""

    def __init__(self, *, api_key: str, location: str, **kwargs: Any) -> None:
        """Build the plugin, then replace its client before any request runs."""
        super().__init__(api_key=api_key, vertexai=False, **kwargs)
        self._client.close()
        self._client = vertex_client(api_key, location)


def build_llm(
    binding: dict[str, Any], state: CallState
) -> google.LLM | openai.LLM | ModelCallLLM:
    """Return the model a think binding names: the SLNG Context Router, OpenAI or Gemini.

    Gemini runs on Vertex when the binding says so. A binding's params, like
    ``reasoning_effort``, go to the plugin as they are.
    """
    if binding["provider"] == "slng":
        return ModelCallLLM(spec.router(binding, state.call), state=state)
    params = dict(binding.get("params", {}))
    if binding["provider"] == "openai":
        return openai.LLM(api_key=required("OPENAI_API_KEY"), model=binding["model"], **params)
    key = required("GOOGLE_API_KEY")
    if params.pop("vertexai", False):
        location = params.pop("location", "global")
        return VertexLLM(api_key=key, location=location, model=binding["model"], **params)
    return google.LLM(api_key=key, model=binding["model"], **params)


def slng_host(binding: dict[str, Any]) -> str:
    """Return the SLNG gateway host of a binding's region."""
    return f"{binding.get('params', {}).get('world_part', 'eu-north')}.api.slng.ai"


def build_stt(ids: TrackingIds) -> Any:
    """Return the speech-to-text model: SLNG's with the call's tracking ids, else speech.py's."""
    if speech.LISTEN is not None:
        return speech.LISTEN()
    listen = MODELS["listen"]
    return slng.STT(
        api_key=required("SLNG_API_KEY"),
        slng_base_url=slng_host(listen),
        model=listen["model"],
        language=listen.get("language", "en"),
        **ids,
    )


def build_tts(name: str, ids: TrackingIds) -> Any:
    """Return one voice: SLNG's with the call's tracking ids, else speech.py's."""
    if (make := speech.SPEAK.get(name)) is not None:
        return make()
    voice = MODELS["speak"][name]
    return slng.TTS(
        api_key=required("SLNG_API_KEY"),
        slng_base_url=slng_host(voice),
        model=voice["model"],
        voice=voice.get("voice", ""),
        language=voice.get("language", "en"),
        warm_standby_enabled=True,
        **ids,
    )


def build_session(state: CallState, vad: silero.VAD, first: str) -> AgentSession:
    """Return the call's session: the speech and the model agent.yaml names, at its pace.

    Interruption is set, not left to LiveKit, so a deployed worker treats the caller
    speaking over the agent the same way as a local one. ``keep`` lets a "yes" said over a
    question answer it.
    """
    entry = spec.SPEC["agents"][first]
    ids = tracking_ids(state.call)
    stt = build_stt(ids)
    session = AgentSession(
        conn_options=SessionConnectOptions(llm_conn_options=APIConnectOptions(max_retry=1)),
        stt=stt,
        llm=build_llm(MODELS["think"][entry["model"]], state),
        tts=build_tts(entry["voice"], ids),
        turn_handling=TurnHandlingOptions(
            turn_detection=inference.TurnDetector(version="v1-mini"),
            endpointing=endpointing(WINDOW),
            interruption=interruption("keep"),
        ),
        vad=vad,
        user_away_timeout=None,
    )
    if isinstance(stt, slng.STT):
        stt.attach_to_session(session)
    yield_to_caller(session)
    return session


def end_call(name: str) -> list[EndCallTool]:
    """Return LiveKit's end-call tool, with agent.yaml's wording, when the agent lists one.

    The goodbye rides in the reply that calls the tool, so the call ends after one model
    request. The session deletes the room as it closes, which hangs up a phone caller.
    """
    ending = spec.SPEC["agents"][name].get("end_call")
    if ending is None:
        return []
    wording = "\n".join(part for part in (ending["description"], ending["instructions"]) if part)
    return [EndCallTool(extra_description=wording, end_instructions=None, delete_room=False)]


def unmute_agent(
    name: str, state: CallState, line: SipLine, telemetry: Telemetry | None = None
) -> UnmuteAgent:
    """Return one agent as a LiveKit agent, with its handoff tools and its end-call tool.

    Only the first agent gets the telemetry: an agent the call is handed to records into
    the first one's spans.
    """
    agent = spec.agent_spec(name, line)
    handoffs = [handoff_tool(spec.HANDOFFS[handoff], state, line) for handoff in agent.handoffs]
    return UnmuteAgent(
        agent, state=state, telemetry=telemetry, native_tools=[*handoffs, *end_call(name)]
    )


def handoff_tool(handoff: spec.Handoff, state: CallState, line: SipLine) -> FunctionTool:
    """Return one handoff as a LiveKit function tool that calls ``UnmuteAgent.hand_off``."""

    async def hand_off(context: RunContext) -> object:
        """Save what the handoff sets, then give the call to the next agent."""
        if handoff.saves:
            state.save(handoff.saves)
        current = context.session.current_agent
        if not isinstance(current, UnmuteAgent):
            raise TypeError("a handoff runs only from an UnmuteAgent")
        return await current.hand_off(
            unmute_agent(handoff.to, state, line),
            announce=handoff.announce,
            context=handoff.context,
            opens_with=handoff.opens_with,
        )

    return function_tool(hand_off, name=handoff.name, description=handoff.description)


server = AgentServer()


def prewarm(proc: JobProcess) -> None:
    """Load the voice detector and the knowledge once per worker process."""
    proc.userdata["vad"] = silero.VAD.load(min_silence_duration=WINDOW.vad_silence)
    spec.start_knowledge()


server.setup_fnc = prewarm


@server.rtc_session(agent_name=os.getenv("LIVEKIT_AGENT_NAME", ""))
async def entrypoint(ctx: JobContext) -> None:
    """Run one call: read what it carries, build its state, greet, and cap its length.

    A console or browser call has no caller ID. ``UNMUTE_CALL_START`` can simulate one.
    """
    await ctx.connect()
    participant = await ctx.wait_for_participant()
    metadata = json.loads(ctx.job.metadata or "{}")
    call = None
    if participant.kind == rtc.ParticipantKind.PARTICIPANT_KIND_SIP:
        call = sip_call_info(participant, metadata, agent_id=spec.AGENT_ID)
    try:
        state = await spec.new_call(call, metadata.get("call_start"))
    except CallStartError as error:
        logger.error("%s: the call does not start: %s", spec.AGENT_ID, error)
        ctx.shutdown(reason=str(error))
        return
    telemetry = None
    if env_value("LANGFUSE_PUBLIC_KEY", str):
        telemetry = Telemetry(
            Langfuse.from_env(), agent=spec.AGENT_ID, call_id=state.call.session_id
        )
    first = spec.entry(state)
    session = build_session(state, ctx.proc.userdata["vad"], first)
    if spec.IDLE is not None:
        idle_nudges(session, spec.IDLE)
    with use_phone_region(lambda: call_phone_region(state.call)):
        await session.start(
            agent=unmute_agent(first, state, SipLine(session), telemetry),
            room=ctx.room,
            room_options=room_io.RoomOptions(delete_room_on_close=True),
        )
    greet(session, spec.GREETING, state)
    if spec.MAX_DURATION_S is not None:
        max_duration(session, spec.MAX_DURATION_S)


if __name__ == "__main__":
    cli.run_app(server)
