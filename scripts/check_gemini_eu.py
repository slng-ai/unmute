"""Check both compiled native Gemini clients, offline unless --live is given.

Run with the compiled projects' pinned livekit-agents[google] and
pipecat-ai[google] installed. Reads the actual emitted constructors without
starting speech, tracing or the salon's tools. --live reads GOOGLE_API_KEY from
the repository root .env and sends one short request per target, only to EU.
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
from text_run_livekit import compiled_llm


async def check(package: Path, target: str, live: bool) -> None:
    filename = "agent.py" if target == "livekit" else "bot.py"
    tree = ast.parse((package / "build" / target / filename).read_text())
    scope: dict[str, object] = {"os": os, "_google_genai": genai}
    if target == "livekit":
        from livekit.agents import llm
        from livekit.plugins import google

        scope["google"] = google
    else:
        from pipecat.processors.aggregators.llm_context import LLMContext
        from pipecat.services.google.llm import GoogleLLMService

        scope["GoogleLLMService"] = GoogleLLMService
    helpers: list[ast.stmt] = [
        node
        for node in tree.body
        if isinstance(node, (ast.FunctionDef, ast.ClassDef))
        and node.name in ("_google_vertex_client", "_GoogleVertexLLM")
    ]
    assert len(helpers) == 2, "compile a native Vertex binding first"
    exec(compile(ast.Module(body=helpers, type_ignores=[]), filename, "exec"), scope)
    constructor = next(
        node
        for node in ast.walk(tree)
        if isinstance(node, ast.Call)
        and isinstance(node.func, ast.Name)
        and node.func.id == "_GoogleVertexLLM"
    )
    # A short test prompt replaces the salon prompt; all model/auth options stay.
    for node in ast.walk(constructor):
        if isinstance(node, ast.keyword) and node.arg == "system_instruction":
            node.value = ast.Constant(value="Reply with EU_OK.")
    expression = ast.fix_missing_locations(ast.Expression(body=constructor))
    requests = []

    class EUTransport(httpx.AsyncBaseTransport):
        async def handle_async_request(self, request):
            assert request.url.host == "aiplatform.eu.rep.googleapis.com", (
                request.url.host
            )
            assert (
                "/publishers/google/models/gemini-3.5-flash-lite:" in request.url.path
            )
            body = json.loads(request.content)
            assert (
                body["generationConfig"]["thinkingConfig"]
                .get(
                    "thinkingLevel",
                    body["generationConfig"]["thinkingConfig"].get(
                        "thinking_level", ""
                    ),
                )
                .lower()
                == "minimal"
            )
            requests.append(str(request.url))
            if live:
                return await network.handle_async_request(request)
            result = {
                "candidates": [
                    {
                        "content": {"role": "model", "parts": [{"text": "EU_OK"}]},
                        "finishReason": "STOP",
                    }
                ]
            }
            if "streamGenerateContent" in request.url.path:
                return httpx.Response(
                    200,
                    text="data: " + json.dumps(result) + "\n\n",
                    headers={"content-type": "text/event-stream"},
                )
            return httpx.Response(200, json=result)

    original_client = genai.Client
    async with (
        httpx.AsyncHTTPTransport() as network,
        httpx.AsyncClient(transport=EUTransport()) as http,
    ):

        def client(**kwargs):
            kwargs["http_options"]["httpx_async_client"] = http
            return original_client(**kwargs)

        with patch.object(genai, "Client", side_effect=client):
            service = (
                compiled_llm(package / "build" / target / filename, scope)
                if target == "livekit"
                else eval(compile(expression, filename, "eval"), scope)
            )
        try:
            if target == "livekit":
                context = llm.ChatContext()
                context.add_message(role="user", content="Reply with EU_OK.")
                async with service.chat(chat_ctx=context) as stream:
                    reply = "".join(
                        [
                            chunk.delta.content or ""
                            async for chunk in stream
                            if chunk.delta
                        ]
                    )
            else:
                reply = await service.run_inference(
                    LLMContext([{"role": "user", "content": "Reply with EU_OK."}])
                )
            assert reply and "EU_OK" in reply, repr(reply)
            assert requests, "no request made"
            print(
                f"{target}: native Vertex EU request passed ({'live' if live else 'offline'})"
            )
        finally:
            await service._client.aio.aclose()
            service._client.close()


async def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("package", type=Path)
    parser.add_argument("--live", action="store_true")
    args = parser.parse_args()
    if args.live:
        load_env_file(Path(__file__).resolve().parents[1] / ".env")
        if not os.environ.get("GOOGLE_API_KEY"):
            raise SystemExit("Set GOOGLE_API_KEY in the root .env")
    else:
        os.environ["GOOGLE_API_KEY"] = "offline-test-key"
    failed = False
    for target in ("livekit", "pipecat"):
        try:
            await check(args.package, target, args.live)
        except Exception as error:
            failed = True
            message = str(error).replace(os.environ["GOOGLE_API_KEY"], "[REDACTED]")
            print(f"{target}: {type(error).__name__}: {message}")
    if failed:
        raise SystemExit(1)


if __name__ == "__main__":
    asyncio.run(main())
