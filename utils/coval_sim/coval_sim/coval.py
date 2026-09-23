"""The one place this tool talks to Coval, through the `coval` CLI."""

import json
import os
import subprocess
import time
from typing import Any

from pydantic import TypeAdapter

from coval_sim.models import Agent, Persona, Run, Simulation


class CovalError(RuntimeError):
    """A coval command failed."""


class CovalClient:
    """Reads and changes Coval resources with `coval --format json`.

    Args:
        api_key: The Coval API key every command runs with.
        binary: The coval executable.
    """

    def __init__(self, api_key: str, binary: str = "coval") -> None:
        if not api_key:
            raise ValueError("a Coval API key is required")
        self._env = {**os.environ, "COVAL_API_KEY": api_key}
        self._binary = binary

    def _call(self, *args: str) -> Any:
        out = subprocess.run(
            [self._binary, "--format", "json", *args],
            capture_output=True,
            text=True,
            env=self._env,
            check=False,
        )
        if out.returncode != 0:
            raise CovalError(f"coval {' '.join(args[:2])}: {out.stderr.strip()}")
        return json.loads(out.stdout) if out.stdout.strip() else None

    def agents(self) -> list[Agent]:
        """Every agent in the organization."""
        return TypeAdapter(list[Agent]).validate_python(self._call("agents", "list") or [])

    def create_agent(self, name: str, kind: str, metadata: dict[str, Any]) -> Agent:
        """Create an agent of a Coval type (livekit, websocket) with its connection."""
        return Agent.model_validate(
            self._call("agents", "create", "--name", name, "--type", kind, "--metadata", json.dumps(metadata))
        )

    def update_connection(self, agent: Agent, metadata: dict[str, Any]) -> Agent:
        """Replace an agent's connection settings. Test sets and metrics stay."""
        return Agent.model_validate(
            self._call("agents", "update", agent.id, "--metadata", json.dumps(metadata))
        )

    def persona(self, name: str) -> Persona:
        """The persona with this name.

        Raises:
            CovalError: If no persona has that name.
        """
        personas = TypeAdapter(list[Persona]).validate_python(self._call("personas", "list") or [])
        for persona in personas:
            if persona.name == name:
                return persona
        raise CovalError(f"no Coval persona named {name!r}")

    def launch(
        self,
        agent: Agent,
        persona: Persona,
        test_set_id: str,
        *,
        iterations: int,
        concurrency: int,
        label: str,
    ) -> str:
        """Start a run of one test set against one agent, with the agent's metrics.

        Returns:
            The new run's ID.
        """
        run = self._call(
            "runs", "launch",
            "--agent-id", agent.id,
            "--persona-id", persona.id,
            "--test-set-id", test_set_id,
            "--iterations", str(iterations),
            "--concurrency", str(concurrency),
            "--name", label,
            "--tags", "release-check",
        )
        return run["run_id"]

    def wait(self, run_id: str, *, timeout_s: float, poll_s: float = 15) -> Run:
        """Poll a run until Coval stops working on it or the timeout passes."""
        deadline = time.monotonic() + timeout_s
        run = Run.model_validate(self._call("runs", "get", run_id))
        while not run.finished and time.monotonic() < deadline:
            time.sleep(poll_s)
            run = Run.model_validate(self._call("runs", "get", run_id))
        return run

    def simulations(self, run_id: str) -> list[Simulation]:
        """Every call in a run, each with its transcript.

        The list endpoint leaves the transcript out, so each call is read again.
        """
        listed = self._call("simulations", "list", "--run-id", run_id) or []
        return [
            Simulation.model_validate(self._call("simulations", "get", item["simulation_id"]))
            for item in listed
        ]
