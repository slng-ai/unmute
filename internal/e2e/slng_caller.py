# /// script
# requires-python = "==3.12.*"
# dependencies = [
#     "livekit-agents==1.8.8",
#     "livekit-plugins-slng>=1.8.8",
# ]
# ///
"""A scripted caller for an agent SLNG runs, over a real web session.

It joins the session's LiveKit room as the caller, says each scripted line with
SLNG speech, and waits for the agent's turn by the agent's own state, as LiveKit
agents publish it in the `lk.agent.state` attribute: a line is said once the
agent is listening, and the agent's turn is over once it is listening again. It
reads what the agent says from the room's transcription stream.

Usage: slng_caller.py SCENARIO.json, with LIVEKIT_URL, LIVEKIT_TOKEN and
SLNG_API_KEY set. It prints one JSON document on stdout: the turns, and whether
the agent ended the call.
"""

import asyncio
import json
import os
import sys

import time

import aiohttp
from livekit import rtc
from livekit.plugins import slng

SAMPLE_RATE = 24000
FRAME_MS = 20
STATE = "lk.agent.state"
TRANSCRIPTION = "lk.transcription"
# A real call's whole length, so a stuck agent fails the run instead of hanging it.
CALL_LIMIT_SECONDS = 240
_STARTED = time.monotonic()


def trace(*parts: object) -> None:
    """Write one timeline line to stderr when SLNG_CALLER_TRACE is set."""
    if os.environ.get("SLNG_CALLER_TRACE"):
        print(
            f"{time.monotonic() - _STARTED:7.2f}", *parts, file=sys.stderr, flush=True
        )


class Caller:
    """The caller's side of one call."""

    def __init__(self, room: rtc.Room) -> None:
        self.room = room
        self.source = rtc.AudioSource(SAMPLE_RATE, 1)
        self.speaking = asyncio.Lock()
        self.state_changed = asyncio.Condition()
        self.agent: rtc.RemoteParticipant | None = None
        self.agent_left = asyncio.Event()
        # What the agent said, each segment with the turn it began in. A turn
        # starts when the caller has finished a line, and again each time the
        # agent takes the caller's words as final.
        self.said: list[tuple[int, str]] = []
        self.turn = 0

    def agent_state(self) -> str:
        return self.agent.attributes.get(STATE, "") if self.agent else ""

    async def notify(self) -> None:
        async with self.state_changed:
            self.state_changed.notify_all()

    async def until(self, ready) -> None:
        async with self.state_changed:
            await self.state_changed.wait_for(
                lambda: ready() or self.agent_left.is_set()
            )

    async def microphone(self) -> None:
        """Send silence between lines, as an open microphone does."""
        silence = rtc.AudioFrame.create(SAMPLE_RATE, 1, SAMPLE_RATE * FRAME_MS // 1000)
        while True:
            async with self.speaking:
                await self.source.capture_frame(silence)

    async def say(self, tts: slng.TTS, line: str) -> None:
        """Say one line, and return once the room has heard all of it."""
        trace("caller says", repr(line))
        async with self.speaking:
            async for audio in tts.synthesize(line):
                await self.source.capture_frame(audio.frame)
            await self.source.wait_for_playout()

    async def heard_caller(self) -> None:
        """The agent took the caller's words as final: its reply starts after them."""
        self.turn += 1
        await self.notify()

    def reply(self) -> list[str]:
        return [text for turn, text in self.said if turn == self.turn]

    async def agent_turn(self, after_caller: bool = True) -> str:
        """Wait until the agent has replied and is listening again, and return the reply.

        The greeting is the one turn that follows no line of the caller's, and
        it may begin before this wait does, so it keeps turn 0.

        Listening alone is not the end of a turn: between two steps of a task
        group the agent listens for a moment and has said nothing yet. Speech
        alone is not a reply either: when the agent ends the caller's turn in a
        pause, it starts a reply to the half it heard and drops it when the
        caller goes on. That speech began before the agent took the caller's
        last words, and its segment can close after them, so a segment counts
        by the turn it began in.
        """
        if after_caller:
            self.turn += 1
        await self.until(lambda: self.reply() and self.agent_state() == "listening")
        return " ".join(self.reply())


async def main(scenario_path: str) -> dict:
    scenario = json.loads(open(scenario_path).read())
    room = rtc.Room()
    caller = Caller(room)

    def joined(participant: rtc.RemoteParticipant) -> None:
        if participant.kind == rtc.ParticipantKind.PARTICIPANT_KIND_AGENT:
            caller.agent = participant
            asyncio.ensure_future(caller.notify())

    def changed(changed: dict, participant: rtc.Participant) -> None:
        if participant is caller.agent:
            trace("attributes", changed)
            asyncio.ensure_future(caller.notify())

    def left(participant: rtc.RemoteParticipant) -> None:
        if participant is caller.agent:
            caller.agent_left.set()
            asyncio.ensure_future(caller.notify())

    async def read_transcript(reader: rtc.TextStreamReader, identity: str) -> None:
        trace("stream open", identity, reader.info.attributes)
        began = caller.turn
        text = await reader.read_all()
        trace("stream closed", identity, reader.info.attributes, repr(text))
        if (
            reader.info.attributes.get("lk.transcription_final", "true") != "true"
            or not text.strip()
        ):
            return
        if identity == room.local_participant.identity:
            await caller.heard_caller()
        elif caller.agent and identity == caller.agent.identity:
            caller.said.append((began, text.strip()))
            await caller.notify()

    room.on("participant_connected", joined)
    room.on("participant_attributes_changed", changed)
    room.on("participant_disconnected", left)
    room.on(
        "disconnected",
        lambda *_: (caller.agent_left.set(), asyncio.ensure_future(caller.notify())),
    )
    room.register_text_stream_handler(
        TRANSCRIPTION,
        lambda reader, identity: asyncio.ensure_future(
            read_transcript(reader, identity)
        ),
    )

    await room.connect(os.environ["LIVEKIT_URL"], os.environ["LIVEKIT_TOKEN"])
    for participant in room.remote_participants.values():
        joined(participant)
    track = rtc.LocalAudioTrack.create_audio_track("caller", caller.source)
    await room.local_participant.publish_track(
        track, rtc.TrackPublishOptions(source=rtc.TrackSource.SOURCE_MICROPHONE)
    )
    microphone = asyncio.create_task(caller.microphone())
    http = aiohttp.ClientSession()
    tts = slng.TTS(
        model=scenario.get("caller_voice_model", "deepgram/aura:2"),
        voice=scenario.get("caller_voice", "aura-2-thalia-en"),
        sample_rate=SAMPLE_RATE,
        slng_base_url=os.environ.get("SLNG_WORLD_PART", "eu-north") + ".api.slng.ai",
        http_session=http,
    )
    turns = []
    try:
        async with asyncio.timeout(CALL_LIMIT_SECONDS):
            await caller.until(lambda: caller.agent is not None)
            greeting = await caller.agent_turn(after_caller=False)
            for turn in scenario["turns"]:
                if caller.agent_left.is_set():
                    break
                await caller.say(tts, turn["caller"])
                turns.append(
                    {"caller": turn["caller"], "agent": await caller.agent_turn()}
                )
            await caller.agent_left.wait()
    except TimeoutError:
        pass
    finally:
        microphone.cancel()
        await tts.aclose()
        await http.close()
        await room.disconnect()
    return {
        "greeting": greeting if "greeting" in locals() else "",
        "turns": turns,
        "agent_ended": caller.agent_left.is_set(),
    }


if __name__ == "__main__":
    print(json.dumps(asyncio.run(main(sys.argv[1])), indent=2))
