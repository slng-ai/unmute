"""What Coval returns, validated, and the rule that decides whether a run passed.

Only the fields this tool reads are declared. Pydantic ignores the rest, so a
field Coval adds later changes nothing here.
"""

import re
from typing import Any

from pydantic import BaseModel

# A Coval agent this tool drives is named after the example and the target it
# calls: unmute-salon-concierge-livekit. The name is the whole link between the
# repository and Coval, so nothing in the repository holds a Coval ID.
AGENT_NAME = re.compile(r"^unmute-(?P<example>.+)-(?P<target>livekit|pipecat)$")
FINISHED = {"COMPLETED", "FAILED", "CANCELLED", "ERROR"}


class Agent(BaseModel):
    """A Coval agent: the connection Coval uses to reach one target."""

    id: str
    display_name: str
    model_type: str = ""
    test_set_ids: list[str] = []
    metric_ids: list[str] = []
    metadata: dict[str, Any] = {}

    @property
    def example(self) -> str | None:
        """The example directory this agent calls, or None if it is not ours."""
        match = AGENT_NAME.match(self.display_name)
        return match["example"] if match else None

    @property
    def target(self) -> str | None:
        """The target this agent calls: livekit or pipecat."""
        match = AGENT_NAME.match(self.display_name)
        return match["target"] if match else None

    @staticmethod
    def name_for(example: str, target: str) -> str:
        """The Coval agent name for an example and target."""
        return f"unmute-{example}-{target}"


class Persona(BaseModel):
    """The simulated caller."""

    id: str
    name: str


class Message(BaseModel):
    """One line of a simulated call's transcript."""

    role: str
    content: str | None = None


class Simulation(BaseModel):
    """One simulated call."""

    simulation_id: str
    status: str
    error_message: str | None = None
    transcript: list[Message] = []

    @property
    def agent_spoke(self) -> bool:
        """Whether the agent said anything at all during the call."""
        return any(
            m.role not in ("user", "system", "tool") and m.content
            for m in self.transcript
        )


class MetricValue(BaseModel):
    """One metric's answer for one call."""

    simulation_output_id: str
    value: Any = None


class MetricResult(BaseModel):
    """One metric's answers across the run."""

    metric_name: str
    values: list[MetricValue] = []


class RunResults(BaseModel):
    """The metric answers of a finished run, keyed by metric ID."""

    metrics: dict[str, MetricResult] = {}


class Run(BaseModel):
    """A Coval run: every call placed for one agent and one test set."""

    run_id: str
    status: str
    error_status: str | None = None
    results: RunResults | None = None

    @property
    def finished(self) -> bool:
        """Whether Coval has stopped working on this run."""
        return self.status in FINISHED

    def problems(self, simulations: list[Simulation]) -> list[str]:
        """Every reason this run fails the check. Empty means it passed.

        The run's own status is not enough: a run whose every call failed can
        still end COMPLETED. A metric is not enough either: a criterion like "did
        not ask twice" passes on a call where the agent said nothing. So each
        call has to have finished, and the agent has to have spoken in it.

        A yes/no metric fails the check when it answers NO. Write each one so
        that YES means the agent did the right thing. Numbers are not judged.

        Args:
            simulations: The run's calls, each with its transcript.

        Returns:
            One line per problem, in the order they were found.
        """
        problems = []
        if self.status != "COMPLETED":
            problems.append(f"run ended {self.status}")
        if self.error_status not in (None, "", "SUCCESS"):
            problems.append(f"run error: {self.error_status}")
        if not simulations:
            problems.append("the run has no simulated calls")
        for sim in simulations:
            if sim.status != "COMPLETED":
                error = sim.error_message or "no error message"
                problems.append(f"call {sim.simulation_id} {sim.status}: {error}")
            elif not sim.agent_spoke:
                problems.append(f"call {sim.simulation_id}: the agent never spoke")
        metrics = self.results.metrics.values() if self.results else []
        for metric in metrics:
            for answer in metric.values:
                if str(answer.value).upper() == "NO":
                    problems.append(
                        f"call {answer.simulation_output_id}: "
                        f"{metric.metric_name} answered NO"
                    )
        return problems
