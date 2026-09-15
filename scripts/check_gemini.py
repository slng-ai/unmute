"""Check emitted native Google clients without starting audio or package tools.

Offline checks record the real SDK requests and inject provider errors. --live
sends a short synthetic request through each distinct emitted constructor;
--env-file selects a credential file without copying it into the test package.
"""

import argparse
import ast
import asyncio
import json
import os
from pathlib import Path
from unittest.mock import patch

import httpx
from google import genai

from read_langfuse_trace import load_env_file

# Expected hosts are fixtures, independent of the emitted endpoint formula.
HOSTS = {
    "us": "aiplatform.us.rep.googleapis.com",
    "eu": "aiplatform.eu.rep.googleapis.com",
    "global": "aiplatform.googleapis.com",
    "us-central1": "us-central1-aiplatform.googleapis.com",
    "europe-west4": "europe-west4-aiplatform.googleapis.com",
    "unsupported-location": "unsupported-location-aiplatform.googleapis.com",
    "developer": "generativelanguage.googleapis.com",
}


async def check(package: Path, target: str, live: bool) -> None:
    filename = "agent.py" if target == "livekit" else "bot.py"
    tree = ast.parse((package / "build" / target / filename).read_text())
    scope: dict[str, object] = {"os": os}
    if target == "livekit":
        from livekit.plugins import google

        scope["google"] = google
    else:
        from pipecat.services.google.llm import GoogleLLMService

        scope["GoogleLLMService"] = GoogleLLMService
    helpers: list[ast.stmt] = [
        node
        for node in tree.body
        if isinstance(node, (ast.FunctionDef, ast.ClassDef))
        and node.name in ("_google_vertex_client", "_GoogleVertexLLM")
    ]
    exec(compile(ast.Module(body=helpers, type_ignores=[]), filename, "exec"), scope)
    constructors = {}
    for node in ast.walk(tree):
        if isinstance(node, ast.Call) and ast.unparse(node.func) in (
            "_GoogleVertexLLM", "google.LLM", "GoogleLLMService"
        ):
            constructors.setdefault(ast.dump(node), node)
    assert constructors, "no native Google constructor in compiled output"
    for constructor in constructors.values():
        location = next(
            (ast.literal_eval(kw.value) for kw in constructor.keywords if kw.arg == "location"),
            "developer",
        )
        # Only the prompt changes; model, location and generation settings stay.
        for node in ast.walk(constructor):
            if isinstance(node, ast.keyword) and node.arg == "system_instruction":
                node.value = ast.Constant(value="Reply with LOCATION_OK.")
        expression = ast.fix_missing_locations(ast.Expression(body=constructor))
        for status in ([200] if live else [200, 400, 404]):
            await check_request(expression, scope, target, location, live, status)
        print(f"{target}: {location} via {HOSTS[location]} ({'live' if live else 'offline, including errors'})")


async def check_request(expression, scope, target, location, live, status):
    requests = []
    vertex = location != "developer"
    version = "v1beta1" if vertex else "v1beta"
    model_path = "publishers/google/models" if vertex else "models"

    class RecordingTransport(httpx.AsyncBaseTransport):
        async def handle_async_request(self, request):
            requests.append(request)
            assert request.url.host == HOSTS[location], request.url.host
            assert request.url.path in (
                f"/{version}/{model_path}/gemini-3.5-flash-lite:generateContent",
                f"/{version}/{model_path}/gemini-3.5-flash-lite:streamGenerateContent",
            ), request.url.path
            assert request.headers["x-goog-api-key"] == os.environ["GOOGLE_API_KEY"]
            assert "authorization" not in request.headers
            config = json.loads(request.content)["generationConfig"]
            thinking = config["thinkingConfig"]
            assert thinking.get("thinkingLevel", thinking.get("thinking_level", "")).lower() == "minimal"
            if not live:
                assert config["temperature"] == 0.2, config
            if live:
                return await network.handle_async_request(request)
            if status != 200:
                return httpx.Response(status, json={"error": {
                    "code": status,
                    "status": "INVALID_ARGUMENT" if status == 400 else "NOT_FOUND",
                    "message": f"Location {location} unsupported" if status == 400 else "Model unavailable in requested location",
                }})
            result = {"candidates": [{
                "content": {"role": "model", "parts": [{"text": "LOCATION_OK"}]},
                "finishReason": "STOP",
            }]}
            if "streamGenerateContent" in request.url.path:
                return httpx.Response(200, text="data: " + json.dumps(result) + "\n\n",
                                      headers={"content-type": "text/event-stream"})
            return httpx.Response(200, json=result)

    original_client = genai.Client
    async with (
        httpx.AsyncHTTPTransport() as network,
        httpx.AsyncClient(transport=RecordingTransport()) as http,
    ):
        def client(**kwargs):
            options = genai.types.HttpOptions.model_validate(kwargs.get("http_options") or {})
            options.httpx_async_client = http
            kwargs["http_options"] = options
            return original_client(**kwargs)

        # LiveKit imports Client directly; Pipecat and the adapter use genai.Client.
        with (
            patch.object(genai, "Client", side_effect=client),
            patch("livekit.plugins.google.llm.Client", side_effect=client),
        ):
            service = eval(compile(expression, "<emitted Google constructor>", "eval"), scope)
        try:
            if vertex:
                assert service._client._api_client.location == location
                assert service._client.vertexai is True
            else:
                assert not service._client.vertexai
            failure = None
            try:
                if target == "livekit":
                    from livekit.agents import llm
                    from livekit.agents.types import APIConnectOptions

                    context = llm.ChatContext()
                    context.add_message(role="user", content="Reply with LOCATION_OK.")
                    async with service.chat(chat_ctx=context, conn_options=APIConnectOptions(max_retry=0)) as stream:
                        reply = "".join([
                            chunk.delta.content or "" async for chunk in stream if chunk.delta
                        ])
                else:
                    from pipecat.processors.aggregators.llm_context import LLMContext

                    reply = await service.run_inference(
                        LLMContext([{"role": "user", "content": "Reply with LOCATION_OK."}])
                    )
            except Exception as error:
                if status == 200:
                    raise
                failure = error
            assert requests, "no request made"
            if status == 200:
                assert reply and "LOCATION_OK" in reply, repr(reply)
            else:
                assert failure is not None, "provider error was swallowed"
                assert str(status) in str(failure), str(failure)
                assert len(requests) == 1, "non-retryable provider error triggered another request"
        finally:
            await service._client.aio.aclose()
            service._client.close()


async def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("packages", type=Path, nargs="+")
    parser.add_argument("--live", action="store_true")
    parser.add_argument("--env-file", type=Path)
    args = parser.parse_args()
    if args.live:
        if args.env_file:
            load_env_file(args.env_file)
        if not os.environ.get("GOOGLE_API_KEY"):
            raise SystemExit("Set GOOGLE_API_KEY or pass --env-file")
    else:
        os.environ["GOOGLE_API_KEY"] = "offline-test-key"
        # Authored routing must win over conflicting SDK environment defaults.
        os.environ["GOOGLE_CLOUD_LOCATION"] = "global"
        os.environ["GOOGLE_CLOUD_PROJECT"] = "ambient-project"
        os.environ["GOOGLE_VERTEX_BASE_URL"] = "https://aiplatform.googleapis.com"
    failed = False
    for package in args.packages:
        for target in ("livekit", "pipecat"):
            try:
                await check(package, target, args.live)
            except Exception as error:
                failed = True
                message = str(error).replace(os.environ["GOOGLE_API_KEY"], "[REDACTED]")
                print(f"{target}: {type(error).__name__}: {message}")
    if failed:
        raise SystemExit(1)


if __name__ == "__main__":
    asyncio.run(main())
