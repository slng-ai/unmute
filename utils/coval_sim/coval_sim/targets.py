"""An example package, and the two targets Coval can call on this laptop."""

import re
import shutil
import subprocess
from abc import ABC, abstractmethod
from dataclasses import dataclass
from pathlib import Path
from typing import Any, ClassVar

from coval_sim.livekit import SIMULATION_HEADER
from coval_sim.runtime import Process, answers_http

# Twilio's own magic test number, sent as the caller on the Pipecat route.
CALLER = "+15005550006"


def read_env(path: Path) -> dict[str, str]:
    """KEY=VALUE lines of a .env file. A missing file is empty."""
    env = {}
    if path.exists():
        for line in path.read_text().splitlines():
            line = line.strip()
            if line and not line.startswith("#") and "=" in line:
                key, _, value = line.partition("=")
                env[key.strip()] = value.strip().strip("'\"")
    return env


@dataclass(frozen=True)
class Endpoints:
    """Where Coval reaches this laptop during one check.

    Attributes:
        livekit_url: The LiveKit Cloud project the worker registers with.
        token_host: The public host in front of the token server.
        pipecat_host: The public host in front of the Pipecat bot.
        pipecat_port: The local port the Pipecat bot listens on.
        auth: The secret Coval sends to the token server in X-Auth.
    """

    livekit_url: str = ""
    token_host: str = ""
    pipecat_host: str = ""
    pipecat_port: int = 7860
    auth: str = ""


class Target(ABC):
    """One compiled target of an example, run locally for Coval to call.

    Args:
        project: The compiled project directory, such as build/livekit.
    """

    kind: ClassVar[str]
    """Our name for the target, and the last part of the Coval agent's name."""
    coval_type: ClassVar[str]
    """The Coval agent type that reaches it."""
    entry: ClassVar[str]
    """The file the compiler writes into the project."""

    def __init__(self, project: Path) -> None:
        self.project = project

    @abstractmethod
    def start(self, env: dict[str, str], endpoints: Endpoints, log: Path) -> Process:
        """Start the target and return once Coval can call it."""

    @abstractmethod
    def connection(self, endpoints: Endpoints) -> dict[str, Any]:
        """The Coval agent's connection settings that reach this target."""


class LiveKitTarget(Target):
    """The LiveKit worker, registered with LiveKit Cloud.

    Coval asks the token server for a room token, and the token dispatches this
    worker by name. The worker only connects outbound, so it needs no tunnel.
    """

    kind = "livekit"
    coval_type = "livekit"
    entry = "agent.py"

    @property
    def agent_name(self) -> str:
        """The name the emitted worker registers under."""
        source = (self.project / self.entry).read_text()
        match = re.search(r"rtc_session\(agent_name=['\"]([^'\"]+)", source)
        if not match:
            raise ValueError(f"{self.project / self.entry} names no agent")
        return match.group(1)

    def start(self, env: dict[str, str], endpoints: Endpoints, log: Path) -> Process:
        # Pinned to 3.12, the same pin scripts/text_run_livekit.py needs to run
        # an emitted LiveKit project on this machine.
        worker = Process(
            "LiveKit worker",
            ["uv", "run", "--python", "3.12", "python", "-m", "livekit.agents", "start", self.entry],
            self.project,
            env,
            log,
        )
        worker.wait_until(lambda: "registered worker" in worker.output(), 600)
        return worker

    def connection(self, endpoints: Endpoints) -> dict[str, Any]:
        return {
            "generate_token_endpoint": f"https://{endpoints.token_host}/livekit/token",
            "livekit_url": endpoints.livekit_url,
            "livekit_agent_name": self.agent_name,
            # Coval sends no agent name of its own; see livekit.dispatch_names.
            "token_request_payload": {"agent_name": self.agent_name},
            "generate_token_headers": {
                SIMULATION_HEADER: "{{simulation_output_id}}",
                "X-Auth": endpoints.auth,
            },
        }


class PipecatTarget(Target):
    """The Pipecat bot on the runner's Twilio route (`bot.py -t twilio`).

    Coval plays Twilio: it posts to the TwiML webhook, reads the <Stream> URL
    out of the reply, then sends `connected` and `start` before any audio, which
    is the handshake the runner's websocket route waits for.
    """

    kind = "pipecat"
    coval_type = "websocket"
    entry = "bot.py"

    def start(self, env: dict[str, str], endpoints: Endpoints, log: Path) -> Process:
        port = endpoints.pipecat_port
        bot = Process(
            "Pipecat bot",
            ["uv", "run", "python", self.entry, "-t", "twilio", "-x", endpoints.pipecat_host, "--port", str(port)],
            self.project,
            env,
            log,
        )
        bot.wait_until(lambda: answers_http(f"http://127.0.0.1:{port}/"), 600)
        return bot

    def connection(self, endpoints: Endpoints) -> dict[str, Any]:
        return {
            "connection_mode": "twiml_webhook",
            "voice_url": f"https://{endpoints.pipecat_host}/",
            "voice_http_method": "POST",
            "twilio_account_sid": "AC" + "0" * 32,
            "twilio_from_number": CALLER,
            "twilio_to_number": CALLER,
            "send_sample_rate_hertz": 8000,
            "receive_sample_rate_hertz": 8000,
            "receive_audio_channels": 1,
            "audio_encoding": "ulaw",
            "message_type_path": "event",
            "audio_message_type_value": "media",
            "audio_data_path": "media.payload",
            "send_audio_template": '{"event":"media","streamSid":"{{stream_sid}}","media":{"payload":"{{audio_data}}"}}',
        }


TARGETS: dict[str, type[Target]] = {t.kind: t for t in (LiveKitTarget, PipecatTarget)}


class Example:
    """A package under examples/.

    Args:
        path: The package directory, the one holding agent.yaml.
    """

    def __init__(self, path: Path) -> None:
        if not (path / "agent.yaml").exists():
            raise ValueError(f"{path} is not a package: it has no agent.yaml")
        self.path = path
        self.name = path.name

    def env(self, base: dict[str, str]) -> dict[str, str]:
        """base, with the package's own .env on top.

        A package's .env holds what only it needs, such as the salon's manager
        number, so it wins over the repository-root one.
        """
        return {**base, **read_env(self.path / ".env")}

    def compile(self, unmute: Path, workdir: Path) -> dict[str, Target]:
        """Compile a scratch copy and return its targets, keyed by kind.

        The copy's `name:` gets a -sim suffix. A deployed worker with the real
        name may already be registered on the LiveKit project, and LiveKit
        would share the simulated calls between it and this laptop.

        Raises:
            RuntimeError: If `unmute compile` fails.
        """
        copy = workdir / self.name
        shutil.rmtree(copy, ignore_errors=True)
        shutil.copytree(self.path, copy, ignore=shutil.ignore_patterns("build", ".env"))
        spec = copy / "agent.yaml"
        spec.write_text(re.sub(r"^name:\s*(\S+)", r"name: \1-sim", spec.read_text(), count=1, flags=re.MULTILINE))
        out = subprocess.run([str(unmute), "compile", str(copy)], capture_output=True, text=True, check=False)
        if out.returncode != 0:
            raise RuntimeError(f"unmute compile {self.name} failed:\n{out.stdout}{out.stderr}")
        targets = {}
        for kind, cls in TARGETS.items():
            entries = sorted(copy.glob(f"build/*/{cls.entry}"))
            if entries:
                targets[kind] = cls(entries[0].parent)
        return targets
