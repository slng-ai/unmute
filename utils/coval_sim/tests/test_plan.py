"""Which runs a test set ID turns into, from what Coval holds."""

from pathlib import Path

from coval_sim.cli import ReleaseCheck, Repository
from coval_sim.coval import CovalClient
from coval_sim.models import Agent


class FakeCoval(CovalClient):
    """A CovalClient that never runs coval: plan() only lists agents."""

    def __init__(self, agents: list[Agent]) -> None:
        super().__init__(api_key="test")
        self._agents = agents

    def agents(self) -> list[Agent]:
        return self._agents


AGENTS = [
    Agent(id="1", display_name="unmute-salon-concierge-livekit", test_set_ids=["SALON", "SHARED"]),
    Agent(id="2", display_name="unmute-salon-concierge-pipecat", test_set_ids=["SALON"]),
    Agent(id="3", display_name="unmute-customer-intake-livekit", test_set_ids=["SHARED"]),
    Agent(id="4", display_name="someone else's agent", test_set_ids=["SALON"]),
]


def plan(test_set_ids: list[str], targets: tuple[str, ...] = ("livekit", "pipecat")):
    check = ReleaseCheck(Repository(Path(".")), FakeCoval(AGENTS), Path("."))
    jobs, missing = check.plan(test_set_ids, list(targets))
    runs = {example: sorted((j.agent.target, j.test_set_id) for j in js) for example, js in jobs.items()}
    return runs, [row.test_set_id for row in missing]


def test_one_test_set_runs_on_every_agent_it_is_attached_to():
    assert plan(["SALON"]) == ({"salon-concierge": [("livekit", "SALON"), ("pipecat", "SALON")]}, [])


def test_a_shared_test_set_reaches_several_examples():
    runs, _ = plan(["SHARED"])
    assert runs == {"salon-concierge": [("livekit", "SHARED")], "customer-intake": [("livekit", "SHARED")]}


def test_no_test_set_means_every_attached_one():
    runs, _ = plan([])
    assert runs["salon-concierge"] == [("livekit", "SALON"), ("livekit", "SHARED"), ("pipecat", "SALON")]


def test_the_target_filter_applies():
    assert plan(["SALON"], ("pipecat",)) == ({"salon-concierge": [("pipecat", "SALON")]}, [])


def test_a_test_set_no_agent_carries_is_reported():
    assert plan(["NOWHERE"]) == ({}, ["NOWHERE"])
