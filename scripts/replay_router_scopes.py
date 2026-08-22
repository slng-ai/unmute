"""Reproduce a SLNG Context Router cache-scope collision, without a voice call.

Why this exists
---------------
The router can answer a repeated turn from its cache instead of calling the
model. Its cache key is the last (assistant, user) exchange and carries no
system prompt; the cache *scope* is the `X-Slng-Agent-Id` header. So two prompt
sites under one scope can be served each other's answers, and that is a defect
in the think request alone. Voice, transport and turn detection have nothing to
do with it.

That means it can be reproduced by sending two requests directly: same last
exchange, different system prompts, and one variable changed between two arms.

    shared   every site sends one id                  the shape before per-site scopes
    scoped   each site sends "<id>:<site name>"       the shape the compiler emits now

Nothing here is invented. The base URL, model, inline configuration and the
system prompts are all read out of a compiled package, so this cannot drift from
what the agent actually sends.

Usage
-----
    ./bin/unmute compile examples/salon-concierge --target livekit
    python3 scripts/replay_router_scopes.py examples/salon-concierge \\
        --first concierge --second booking_specialist --family try1

Needs SLNG_API_KEY and the upstream's key (OPENAI_API_KEY for an openai
upstream) in the environment. Reads nothing from a file but the build output.

Writes JSON on stdout. `--summary` prints one line per read instead.

Cost and safety
---------------
Four requests per arm at most, and the scope ids are prefixed so they cannot
collide with a real package's cache. Never point the arms at a production
`agent_id`: a shared-arm run deliberately writes an answer under one scope and
reads it back from another.
"""

import argparse
import ast
import json
import os
import pathlib
import ssl
import sys
import time
import urllib.error
import urllib.request
import uuid

# Python builds from python.org ship no CA bundle; macOS keeps one here. Falls
# back to the default context wherever that file does not exist.
_CA = "/etc/ssl/cert.pem"
SSL_CONTEXT = (
    ssl.create_default_context(cafile=_CA)
    if pathlib.Path(_CA).exists()
    else ssl.create_default_context()
)

# The module each driver writes its prompts and its router config into.
AGENT_MODULE = {"livekit": "agent.py", "pipecat": "bot.py"}

# Upstream provider -> the environment variable holding its key. Mirrors the
# credential rows in internal/target/slng_router.go; extend both together.
UPSTREAM_KEY_ENV = {
    "openai": "OPENAI_API_KEY",
    "openai-compat": None,
    "azure": None,
    "vertex": None,
    "bedrock": None,
}

UPSTREAM_URL = {"openai": "https://api.openai.com/v1"}


def env(name):
    value = os.environ.get(name)
    if not value:
        sys.exit(f"{name} is not set")
    return value


def report(build):
    path = build / "compile-report.json"
    if not path.exists():
        sys.exit(f"no {path}: run `unmute compile` for this target first")
    return json.loads(path.read_text())


def router_binding(compiled):
    """The resolved router think binding, or exit saying there is none."""
    for row in compiled.get("bindings", []):
        binding = row.get("binding", {})
        if row.get("role") == "reason" and binding.get("agent_id"):
            return binding
    sys.exit("this target has no SLNG Context Router think binding to replay")


def prompts(module):
    """Every module-level string constant in the emitted driver module.

    The prompts are read rather than retyped so this script asks with the same
    words the agent asks with. A paraphrase would change the cache key and prove
    nothing.
    """
    found = {}
    for node in ast.parse(module.read_text()).body:
        if not isinstance(node, ast.Assign):
            continue
        target = node.targets[0]
        if isinstance(target, ast.Name) and isinstance(node.value, ast.Constant):
            if isinstance(node.value.value, str):
                found[target.id] = node.value.value
    return found


def prompt_for(site, literals):
    key = site.upper() + "_PROMPT"
    if key not in literals:
        sys.exit(f"no {key} in the emitted module; is {site!r} an agent of this package?")
    return literals[key]


def slng_config(binding):
    """The inline model configuration, the shape _slng_config_*() returns."""
    provider = (binding.get("upstream") or {}).get("provider", "openai")
    key_env = UPSTREAM_KEY_ENV.get(provider)
    if key_env is None:
        sys.exit(
            f"upstream provider {provider!r} needs its credential fields spelled out; "
            "only openai is wired up here"
        )
    endpoint = {"api_key": env(key_env)}
    if provider in UPSTREAM_URL:
        endpoint["url"] = UPSTREAM_URL[provider]
    return {
        "tiers": {
            "1": [{"endpoint": endpoint, "model": binding["model"], "weight": 100}]
        }
    }


def base_url(binding):
    region = (binding.get("params") or {}).get("world_part_override")
    if not region:
        sys.exit("the binding has no params.world_part_override, so it has no base URL")
    return f"https://{region}.context-router.slng.ai/v1"


def ask(*, url, slng_key, binding, config, scope, system, tail):
    body = {
        "model": binding["model"],
        "messages": [{"role": "system", "content": system}] + tail,
        "slng_config": config,
    }
    effort = (binding.get("params") or {}).get("reasoning_effort")
    if effort:
        body["reasoning_effort"] = effort
    request = urllib.request.Request(
        url + "/chat/completions",
        data=json.dumps(body).encode(),
        headers={
            "Authorization": f"Bearer {slng_key}",
            "Content-Type": "application/json",
            "X-Slng-Agent-Id": scope,
            # One per request here. In a real call it is one per call; it groups
            # requests and scopes nothing, so it cannot affect this result.
            "X-Slng-Session-Id": str(uuid.uuid4()),
        },
        method="POST",
    )
    started = time.perf_counter()
    try:
        with urllib.request.urlopen(request, timeout=120, context=SSL_CONTEXT) as res:
            payload = json.loads(res.read())
            headers = {k.lower(): v for k, v in res.headers.items()}
    except urllib.error.HTTPError as error:
        return {
            "scope": scope,
            "http_status": error.code,
            "elapsed_ms": round((time.perf_counter() - started) * 1000, 2),
            "error_body": error.read().decode()[:2000],
        }
    return {
        "scope": scope,
        "http_status": 200,
        "elapsed_ms": round((time.perf_counter() - started) * 1000, 2),
        "answer": payload["choices"][0]["message"].get("content"),
        # The router says on the wire whether it generated or served:
        # x-slng-response-source is "llm" or "cache", and x-slng-cache-layer
        # names the layer on a hit. This is the measurement; latency is a weak
        # cross-check because a hit is not reliably fast.
        "source": headers.get("x-slng-response-source", "unknown"),
        "cache_layer": headers.get("x-slng-cache-layer", "none"),
        "request_id": headers.get("x-slng-request-id", ""),
    }


# The exchange both sites ask on. It has to end on a user message: the router
# skips the cache when the last message is not a user turn.
TAIL = [
    {"role": "assistant", "content": "Hello, how can I help you today?"},
    {"role": "user", "content": "Hi, I would like to book a haircut please."},
]


def arm(name, scope_of, *, url, slng_key, binding, config, literals, first, second):
    """The writer answers twice, then the reader asks the same exchange.

    Twice, so the arm carries its own proof the cache took an entry at all. A
    clean result with no hit anywhere proves nothing in either direction.
    """
    record = {"arm": name, "scopes": scope_of, "turns": []}
    for phase in ("write", "confirm-cache-took"):
        turn = ask(
            url=url, slng_key=slng_key, binding=binding, config=config,
            scope=scope_of[first], system=prompt_for(first, literals), tail=TAIL,
        )
        turn.update(site=first, phase=phase)
        record["turns"].append(turn)
    turn = ask(
        url=url, slng_key=slng_key, binding=binding, config=config,
        scope=scope_of[second], system=prompt_for(second, literals), tail=TAIL,
    )
    turn.update(site=second, phase="read")
    record["turns"].append(turn)
    return record


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("package", help="package directory, e.g. examples/salon-concierge")
    parser.add_argument("--target", default="livekit", choices=sorted(AGENT_MODULE))
    parser.add_argument("--first", required=True, help="the agent that answers first")
    parser.add_argument("--second", required=True, help="the agent that asks after it")
    parser.add_argument("--family", default=None, help="tag for this run's scope ids")
    parser.add_argument("--summary", action="store_true", help="one line per read")
    args = parser.parse_args()

    build = pathlib.Path(args.package) / "build" / args.target
    module = build / AGENT_MODULE[args.target]
    if not module.exists():
        sys.exit(f"no {module}: run `unmute compile {args.package}` first")

    binding = router_binding(report(build))
    literals = prompts(module)
    url, config = base_url(binding), slng_config(binding)
    slng_key = env("SLNG_API_KEY")
    family = args.family or uuid.uuid4().hex[:8]
    sites = (args.first, args.second)

    # Throwaway ids, never the authored one: the shared arm writes an answer
    # under one scope on purpose and reads it from another.
    shared, scoped = f"replay-shared-{family}", f"replay-scoped-{family}"
    out = {
        "package": args.package,
        "target": args.target,
        "authored_agent_id": binding["agent_id"],
        "base_url": url,
        "model": binding["model"],
        "tail": TAIL,
        "arms": [
            arm(
                "shared", {site: shared for site in sites},
                url=url, slng_key=slng_key, binding=binding, config=config,
                literals=literals, first=args.first, second=args.second,
            ),
            arm(
                "scoped", {site: f"{scoped}:{site}" for site in sites},
                url=url, slng_key=slng_key, binding=binding, config=config,
                literals=literals, first=args.first, second=args.second,
            ),
        ],
    }

    if not args.summary:
        print(json.dumps(out, indent=2))
        return
    print(f"{'arm':8} {'reader':24} {'source':7} {'layer':9} {'ms':>7}  verdict")
    for a in out["arms"]:
        said = {
            (t.get("answer") or "").strip()
            for t in a["turns"]
            if t["site"] == args.first
        }
        for t in a["turns"]:
            if t["phase"] != "read":
                continue
            answer = (t.get("answer") or "").strip()
            collided = answer != "" and answer in said
            print(
                f"{a['arm']:8} {t['site']:24} {t.get('source', '?'):7} "
                f"{t.get('cache_layer', '-'):9} {t['elapsed_ms']:7.0f}  "
                f"{'COLLIDED' if collided else 'clean'}"
            )


if __name__ == "__main__":
    main()
