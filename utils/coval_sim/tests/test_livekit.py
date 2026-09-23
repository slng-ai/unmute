"""The token Coval gets, and which agent it dispatches."""

import base64
import hashlib
import hmac
import json

import pytest

from coval_sim.livekit import dispatch_names, mint_token


def decode(token: str, secret: str) -> dict:
    """The claims of a token, after checking its signature."""
    header, payload, signature = token.split(".")
    expected = hmac.new(secret.encode(), f"{header}.{payload}".encode(), hashlib.sha256).digest()
    assert base64.urlsafe_b64decode(signature + "=" * (-len(signature) % 4)) == expected
    return json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)))


def test_token_dispatches_the_agent_with_the_simulation():
    claims = decode(mint_token("key", "secret", "room-1", "caller", ["salon-sim-livekit"], "sim-9", 1000), "secret")
    assert claims["iss"] == "key"
    assert claims["video"]["room"] == "room-1"
    assert claims["video"]["roomJoin"] is True
    agent = claims["roomConfig"]["agents"][0]
    assert agent["agentName"] == "salon-sim-livekit"
    assert json.loads(agent["metadata"]) == {"coval.simulation_id": "sim-9"}


def test_token_without_a_simulation_carries_no_metadata():
    claims = decode(mint_token("key", "secret", "r", "c", ["a"], "", 1000), "secret")
    assert "metadata" not in claims["roomConfig"]["agents"][0]


@pytest.mark.parametrize(
    ("body", "names"),
    [
        ({"room_config": {"agents": [{"agent_name": "a"}]}, "agent_name": "b"}, ["a"]),
        # What Coval actually sends, with our token_request_payload merged in.
        ({"room_name": "r", "participant_name": "p", "agent_name": "b"}, ["b"]),
        ({"room_name": "r"}, []),
    ],
)
def test_dispatch_names(body, names):
    assert dispatch_names(body) == names
