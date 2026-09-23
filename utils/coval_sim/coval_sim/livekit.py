"""The LiveKit token server Coval asks for a room before each simulated call."""

import base64
import hashlib
import hmac
import json
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from types import TracebackType
from typing import Self

# Coval sends the simulation ID to the token server under this header. The
# emitted agent reads it back out of dispatch metadata under coval.simulation_id.
SIMULATION_HEADER = "X-Coval-Simulation-Id"


def _b64url(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode()


_JWT_HEADER = _b64url(b'{"alg":"HS256","typ":"JWT"}')


def mint_token(
    api_key: str,
    api_secret: str,
    room: str,
    identity: str,
    agent_names: list[str],
    simulation: str,
    now: float,
) -> str:
    """A LiveKit participant token that also dispatches the named agents.

    Same claim shape as internal/cli/livekit_token.go: HS256 over the API secret,
    the video grant, and roomConfig agent dispatch. An explicitly dispatched
    worker joins only a room whose token names it.

    Args:
        api_key: The LiveKit API key, the token's issuer.
        api_secret: The LiveKit API secret the token is signed with.
        room: The room the caller may join.
        identity: The caller's participant identity.
        agent_names: The workers to dispatch into the room.
        simulation: Coval's simulation ID, or "" when there is none.
        now: The time the token becomes valid, in Unix seconds.

    Returns:
        The signed JWT.
    """
    agents: list[dict[str, str]] = [{"agentName": name} for name in agent_names]
    if simulation:
        metadata = json.dumps({"coval.simulation_id": simulation})
        for agent in agents:
            agent["metadata"] = metadata
    claims = {
        "iss": api_key,
        "sub": identity,
        "nbf": int(now),
        "exp": int(now) + 3600,
        "video": {
            "room": room,
            "roomJoin": True,
            "canPublish": True,
            "canSubscribe": True,
            "canPublishData": True,
        },
        "roomConfig": {"agents": agents},
    }
    signing_input = f"{_JWT_HEADER}.{_b64url(json.dumps(claims).encode())}"
    signature = hmac.new(api_secret.encode(), signing_input.encode(), hashlib.sha256).digest()
    return f"{signing_input}.{_b64url(signature)}"


def dispatch_names(body: dict) -> list[str]:
    """The agents a token request asks to dispatch.

    Coval's docs say it sends room_config with the agent to dispatch. Measured on
    2026-09-23 it does not: the body is room_name and participant_name only, and
    the room sat empty. So the Coval agent also carries the name in its
    token_request_payload, which Coval does merge into the body.
    """
    names = [
        a["agent_name"]
        for a in (body.get("room_config") or {}).get("agents") or []
        if a.get("agent_name")
    ]
    if not names and body.get("agent_name"):
        names = [body["agent_name"]]
    return names


class TokenServer:
    """Serves POST /livekit/token on localhost, in a background thread.

    Every request must carry this session's secret in X-Auth, because the tunnel
    in front of it is public for as long as the check runs. Each request and its
    body is written to the log, never the secret or the token, so a room nobody
    joined can be traced to what Coval asked for.

    Args:
        url: The LiveKit server URL returned to the caller.
        api_key: The LiveKit API key.
        api_secret: The LiveKit API secret.
        auth: The shared secret Coval sends in X-Auth.
        log: Where each request is written.
        port: The local port to listen on.
    """

    def __init__(self, url: str, api_key: str, api_secret: str, auth: str, log: Path, port: int) -> None:
        self.port = port
        server = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, format: str, *args: object) -> None:
                server._note(format % args)

            def do_POST(self) -> None:
                if not hmac.compare_digest(self.headers.get("X-Auth", ""), auth):
                    self._reply(401, {"error": "bad or missing X-Auth"})
                    return
                length = int(self.headers.get("Content-Length") or 0)
                body = json.loads(self.rfile.read(length) or b"{}")
                simulation = self.headers.get(SIMULATION_HEADER, "")
                server._note(f"{SIMULATION_HEADER}={simulation or '-'} body={json.dumps(body)}")
                room = body.get("room_name")
                if not room:
                    self._reply(400, {"error": "room_name is required"})
                    return
                token = mint_token(
                    api_key,
                    api_secret,
                    room,
                    body.get("participant_name") or "simulated_user",
                    dispatch_names(body),
                    simulation,
                    time.time(),
                )
                self._reply(200, {"token": token, "serverUrl": url, "room_name": room})

            def _reply(self, code: int, body: dict) -> None:
                data = json.dumps(body).encode()
                self.send_response(code)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

        self._log = log
        self._http = ThreadingHTTPServer(("127.0.0.1", port), Handler)

    def _note(self, line: str) -> None:
        with self._log.open("a") as out:
            out.write(f"{time.strftime('%H:%M:%S')} {line}\n")

    def __enter__(self) -> Self:
        threading.Thread(target=self._http.serve_forever, daemon=True).start()
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        self._http.shutdown()
        self._http.server_close()
