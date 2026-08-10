import logging
import os
from typing import Annotated
import httpx
from dotenv import load_dotenv
from pydantic import Field
from livekit.agents import (
    Agent,
    AgentServer,
    AgentSession,
    JobContext,
    JobProcess,
    RunContext,
    TurnHandlingOptions,
    cli,
    function_tool,
    inference,
    metrics,
)
from livekit.agents.beta.tools import EndCallTool  # prebuilt end_call (beta)
from livekit.agents.voice import MetricsCollectedEvent
from livekit.plugins import openai, silero, slng


logger = logging.getLogger("livekit")
logger.setLevel(logging.INFO)

load_dotenv()


# --- prompts ---------------------------------------------------------------

ASSISTANT_PROMPT = """You are a helpful voice assistant that helps the user find places in google via the find places tools (free text search). This is a phone call, so keep every answer to one or two short sentences.
"""


# --- webhook auth ------------------------------------------------------------
# One helper per scheme a tool actually declares; the token is read from the
# environment at call time and never appears in the spec.
def _bearer(env: str) -> dict[str, str]:
    """auth: bearer — the Authorization header read from the tool's token_env."""
    return {"Authorization": "Bearer " + os.environ[env]}


# --- agents ----------------------------------------------------------------


class Assistant(Agent):
    def __init__(self) -> None:
        super().__init__(
            instructions=ASSISTANT_PROMPT,
            tools=[
                *EndCallTool(
                    extra_description="End the call when the caller is finished or says goodbye."
                ).tools
            ],
        )

    async def on_enter(self) -> None:
        await self.session.say("Hi, thanks for calling. How can I help you today?")

    @function_tool
    async def find_places(
        self,
        ctx: RunContext,
        query: Annotated[
            str,
            Field(
                description='The place search in plain language, as the caller phrased it, including the city or area when they gave one. Example: "tapas bar in Madrid".'
            ),
        ],
    ) -> dict:
        """Search for a place by name, type, or area, for example "tapas bar in Madrid" or "pharmacy near Termini station". Call this whenever the caller asks where something is, what a place is called, or for its address or contact details. Returns the matching places."""
        async with httpx.AsyncClient() as client:
            resp = await client.post(
                os.environ["LOOKUP_PLACES_URL"],
                headers=_bearer("LOOKUP_PLACES_TOKEN"),
                json={"query": query},
            )
            resp.raise_for_status()
            return resp.json()


# --- session ---------------------------------------------------------------
def prewarm(proc: JobProcess) -> None:
    proc.userdata["vad"] = silero.VAD.load()


server = AgentServer()
server.setup_fnc = prewarm


@server.rtc_session(agent_name="livekit")
async def entrypoint(ctx: JobContext) -> None:
    session = AgentSession(
        stt=slng.STT(
            api_key=os.environ["SLNG_API_KEY"], model="slng/deepgram/nova:3-en"
        ),
        llm=openai.LLM(api_key=os.environ["OPENAI_API_KEY"], model="gpt-4.1-mini"),
        tts=slng.TTS(
            api_key=os.environ["SLNG_API_KEY"],
            voice="aura-2-thalia-en",
            model="slng/deepgram/aura:2-en",
        ),
        turn_handling=TurnHandlingOptions(
            turn_detection=inference.TurnDetector(version="v1-mini"),
            preemptive_generation={"enabled": True},
        ),
        vad=ctx.proc.userdata["vad"],
    )

    @session.on("metrics_collected")
    def _on_metrics_collected(ev: MetricsCollectedEvent) -> None:
        metrics.log_metrics(ev.metrics)

    await session.start(agent=Assistant(), room=ctx.room)
    await ctx.connect()


if __name__ == "__main__":
    cli.run_app(server)
