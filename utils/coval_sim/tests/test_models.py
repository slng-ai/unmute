"""Reading Coval's answers, and the rule that passes or fails a run."""

from coval_sim.models import Agent, Run, Simulation


def call(status: str = "COMPLETED", said: str = "Hello, Sage and Stone.") -> Simulation:
    transcript = [{"role": "user", "content": "Hi"}]
    if said:
        transcript.append({"role": "assistant", "content": said})
    return Simulation.model_validate({"simulation_id": "s1", "status": status, "transcript": transcript})


def test_agent_name_links_an_agent_to_an_example_and_target():
    agent = Agent(id="a", display_name="unmute-salon-concierge-livekit")
    assert (agent.example, agent.target) == ("salon-concierge", "livekit")
    assert Agent(id="b", display_name="salon-concierge pipecat (twiml)").example is None
    assert Agent.name_for("customer-intake", "pipecat") == "unmute-customer-intake-pipecat"


def test_a_good_run_passes():
    assert Run(run_id="r", status="COMPLETED").problems([call()]) == []
    assert Run(run_id="r", status="COMPLETED", error_status="SUCCESS").problems([call()]) == []


def test_a_silent_agent_fails_even_when_the_run_completed():
    assert Run(run_id="r", status="COMPLETED").problems([call(said="")]) == ["call s1: the agent never spoke"]


def test_a_failed_call_and_a_run_error_both_fail():
    run = Run(run_id="r", status="COMPLETED", error_status="EXECUTION_FAILURE")
    assert len(run.problems([call(status="FAILED")])) == 2


def test_a_no_answer_fails_and_a_number_does_not():
    run = Run.model_validate({
        "run_id": "r",
        "status": "COMPLETED",
        "results": {"metrics": {
            "a": {"metric_name": "Booked", "values": [{"simulation_output_id": "s1", "value": "NO"}]},
            "b": {"metric_name": "Turns", "values": [{"simulation_output_id": "s1", "value": 0.0}]},
        }},
    })
    assert run.problems([call()]) == ["call s1: Booked answered NO"]


def test_a_run_with_no_calls_fails():
    assert Run(run_id="r", status="COMPLETED").problems([])
