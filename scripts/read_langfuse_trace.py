"""Read one Langfuse trace back as an evaluation-ready summary.

This is the local loop's other half. You change something, somebody runs
`unmute dev` and talks to the agent, and this reads what actually happened:
what was said, which tools ran, and where the time went. No dial-in, no
tunnel, no person needed on the second read.

With no arguments it takes the newest trace in the last two hours, which is
almost always the call that just finished:

    python3 scripts/read_langfuse_trace.py --env examples/salon-concierge/.env

Pin one instead with `--trace-id`, and use `--sessions` to see what is there
before choosing.

Credentials come from the environment, or from the `--env` file: the same
LANGFUSE_PUBLIC_KEY, LANGFUSE_SECRET_KEY and LANGFUSE_BASE_URL a package
declares. LANGFUSE_HOST is accepted for the base URL too, because that is what
Langfuse's own CLI calls it.

Two things worth knowing about the API, both found the hard way:

  - `GET /api/public/traces/{id}` is Langfuse v3 and refuses on Cloud. Spans
    come from `GET /api/public/v2/observations` instead, filtered by traceId.
  - `metadata` values are truncated at 200 characters unless asked for by key,
    which this script does not need and so does not ask for.
"""

from __future__ import annotations

import argparse
import base64
import datetime as dt
import json
import os
import pathlib
import ssl
import sys
import urllib.error
import urllib.parse
import urllib.request
from collections import Counter, defaultdict

# Python builds from python.org ship no CA bundle; macOS keeps one here. Falls
# back to the default context wherever that file does not exist.
_CA = "/etc/ssl/cert.pem"
SSL_CONTEXT = (
    ssl.create_default_context(cafile=_CA)
    if pathlib.Path(_CA).exists()
    else ssl.create_default_context()
)

# Everything the summary reads. `core` and `basic` come back anyway; the rest
# is what turns a span list into a transcript with timings.
FIELDS = "core,basic,time,io,model,usage,metrics,trace_context"

# A local `unmute dev` room is named this way by the emitted token server. A
# real SIP call is `call-_+<number>_XXXX`, and a bare UUID is a Pipecat session.
LOCAL_SESSION_PREFIX = "unmute-"


def load_env_file(path: pathlib.Path) -> None:
    """Put KEY=VALUE lines into os.environ without overwriting what is set."""
    for line in path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        os.environ.setdefault(key.strip(), value.strip().strip("'\""))


def api_get(host: str, auth: str, path: str, params: dict[str, str]) -> dict:
    url = f"{host}{path}?{urllib.parse.urlencode(params)}"
    request = urllib.request.Request(url, headers={"Authorization": f"Basic {auth}"})
    try:
        with urllib.request.urlopen(request, timeout=60, context=SSL_CONTEXT) as res:
            return json.loads(res.read())
    except urllib.error.HTTPError as error:
        body = error.read().decode()[:500]
        raise SystemExit(f"Langfuse returned {error.code} for {path}: {body}") from error


def fetch_observations(host: str, auth: str, params: dict[str, str]) -> list[dict]:
    """Walk every cursor page, because one call is 50 spans and a turn is more."""
    out: list[dict] = []
    cursor = None
    while True:
        page = dict(params, limit="1000", fields=FIELDS)
        if cursor:
            page["cursor"] = cursor
        body = api_get(host, auth, "/api/public/v2/observations", page)
        out.extend(body.get("data", []))
        cursor = (body.get("meta") or {}).get("nextCursor")
        if not cursor:
            return out


def group_by_trace(spans: list[dict]) -> list[dict]:
    groups: dict[str, dict] = defaultdict(
        lambda: {"spans": 0, "first": None, "last": None, "session": None, "name": None}
    )
    for span in spans:
        entry = groups[span.get("traceId")]
        entry["spans"] += 1
        entry["session"] = entry["session"] or span.get("sessionId")
        entry["name"] = entry["name"] or span.get("traceName")
        start = span.get("startTime")
        if start:
            entry["first"] = min(entry["first"] or start, start)
            entry["last"] = max(entry["last"] or start, start)
    rows = [dict(v, trace_id=k) for k, v in groups.items()]
    return sorted(rows, key=lambda r: r["last"] or "", reverse=True)


def text_of(value: object) -> str:
    """Span io is sometimes a bare string, sometimes a JSON-encoded structure."""
    if value is None:
        return ""
    if isinstance(value, str):
        stripped = value.strip()
        if stripped[:1] in "[{":
            try:
                return text_of(json.loads(stripped))
            except json.JSONDecodeError:
                return stripped
        return stripped
    if isinstance(value, dict):
        for key in ("content", "text", "output", "summary"):
            if key in value:
                return text_of(value[key])
        return json.dumps(value, separators=(",", ":"))
    if isinstance(value, list):
        return " ".join(t for t in (text_of(v) for v in value) if t)
    return str(value)


def print_transcript(spans: list[dict]) -> None:
    """Rebuild the call from the speech spans, which carry the plain words.

    `stt` output is what the caller said and `tts` input is what the agent
    said, so the two together are the conversation with no prompt scaffolding
    around it. The LLM spans hold the same words wrapped in system prompts and
    tool-call envelopes, which is the wrong shape to read a call in.
    """
    turns = []
    for span in spans:
        name, start = span.get("name"), span.get("startTime")
        if name == "stt":
            turns.append((start, "caller", text_of(span.get("output")), span))
        elif name == "tts":
            turns.append((start, "agent", text_of(span.get("input")), span))
    turns.sort(key=lambda t: t[0] or "")

    print("\n=== transcript ===")
    if not turns:
        print("  (no stt or tts spans: the call produced no speech)")
        return
    for start, who, said, _span in turns:
        clock = (start or "")[11:19]
        if said:
            print(f"  {clock}  {who:<6}  {said}")


def print_tools(spans: list[dict]) -> None:
    """Tool calls, matched to their results.

    The TOOL span carries the result but not reliably the name or arguments;
    the model's own `llm_request` output carries the name and arguments. So
    read the request for what was asked and the TOOL span for what came back.
    """
    print("\n=== tool calls ===")
    asked = []
    for span in sorted(spans, key=lambda s: s.get("startTime") or ""):
        if span.get("name") != "llm_request":
            continue
        parsed = span.get("output")
        if isinstance(parsed, str):
            try:
                parsed = json.loads(parsed)
            except json.JSONDecodeError:
                continue
        if not isinstance(parsed, dict):
            continue
        for call in parsed.get("tool_calls") or []:
            if isinstance(call, str):
                try:
                    call = json.loads(call)
                except json.JSONDecodeError:
                    continue
            function = (call or {}).get("function") or {}
            asked.append((span.get("startTime"), function.get("name"), function.get("arguments")))

    results = [
        (s.get("startTime"), text_of(s.get("output")))
        for s in sorted(spans, key=lambda s: s.get("startTime") or "")
        if s.get("type") == "TOOL"
    ]

    if not asked and not results:
        print("  (none)")
        return
    for start, name, arguments in asked:
        print(f"  {(start or '')[11:19]}  call    {name}({arguments or ''})")
    for start, output in results:
        if output:
            print(f"  {(start or '')[11:19]}  result  {output[:160]}")


def print_usage(spans: list[dict]) -> None:
    """Per-span usage, which is where a context change shows up.

    This is the number to read a `context.history` or a `requires:` change
    against: trimming a step's context moves its input tokens and nothing
    else. Langfuse reports what the provider reported, so on the SLNG router
    `output` comes back 0 rather than absent, and a total that equals the
    input is that, not a request with no reply. TTS reports characters instead
    of tokens, so its row says so and is never added to a token figure.
    """
    buckets: dict[str, dict] = defaultdict(
        lambda: {"n": 0, "input": 0, "output": 0, "total": 0, "max": 0, "unit": "tokens"}
    )
    for span in spans:
        details = span.get("usageDetails") or {}
        if span.get("type") != "GENERATION" or not details:
            continue
        row = buckets[span.get("name") or "?"]
        total = int(details.get("total") or 0)
        row["n"] += 1
        row["input"] += int(details.get("input") or 0)
        row["output"] += int(details.get("output") or 0)
        row["total"] += total
        row["max"] = max(row["max"], total)
        if "characters" in details:
            row["unit"] = "characters"

    print("\n=== usage ===")
    if not buckets:
        print("  (no span reported any usage)")
        return
    print(f"  {'span':<8} {'n':>3} {'input':>8} {'output':>7} {'total':>8} {'mean':>7} {'max':>7}  unit")
    for name, row in sorted(buckets.items(), key=lambda kv: -kv[1]["total"]):
        print(
            f"  {name:<8} {row['n']:>3} {row['input']:>8} {row['output']:>7} "
            f"{row['total']:>8} {row['total'] // row['n']:>7} {row['max']:>7}  {row['unit']}"
        )


def print_latency(spans: list[dict]) -> None:
    """Per-span-name latency, worst first.

    `agent_turn` is the number a caller feels. Do not add `user_turn` to it:
    the endpointing wait is already inside the turn, so summing the two counts
    the same silence twice.
    """
    buckets: dict[str, list[float]] = defaultdict(list)
    for span in spans:
        latency = span.get("latency")
        if isinstance(latency, (int, float)):
            buckets[span.get("name") or "?"].append(float(latency))
    print("\n=== latency, seconds ===")
    print(f"  {'span':<22} {'n':>3} {'mean':>7} {'p50':>7} {'max':>7}")
    rows = sorted(buckets.items(), key=lambda kv: -(sum(kv[1]) / len(kv[1])))
    for name, values in rows:
        values.sort()
        mean = sum(values) / len(values)
        p50 = values[len(values) // 2]
        print(f"  {name:<22} {len(values):>3} {mean:>7.3f} {p50:>7.3f} {values[-1]:>7.3f}")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--trace-id", help="read this trace instead of the newest one")
    parser.add_argument(
        "--hours", type=float, default=2.0, help="how far back to look (default 2)"
    )
    parser.add_argument(
        "--sessions", action="store_true", help="list recent traces and stop"
    )
    parser.add_argument(
        "--local-only",
        action="store_true",
        help=f"only sessions named {LOCAL_SESSION_PREFIX}*, which are unmute dev rooms",
    )
    parser.add_argument("--env", type=pathlib.Path, help="read credentials from this .env")
    args = parser.parse_args()

    if args.env:
        if not args.env.exists():
            print(f"no such env file: {args.env}", file=sys.stderr)
            return 1
        load_env_file(args.env)

    host = (os.environ.get("LANGFUSE_BASE_URL") or os.environ.get("LANGFUSE_HOST") or "").rstrip("/")
    public, secret = os.environ.get("LANGFUSE_PUBLIC_KEY"), os.environ.get("LANGFUSE_SECRET_KEY")
    if not (host and public and secret):
        print(
            "set LANGFUSE_BASE_URL, LANGFUSE_PUBLIC_KEY and LANGFUSE_SECRET_KEY, "
            "or pass --env pointing at a package .env that has them",
            file=sys.stderr,
        )
        return 1
    auth = base64.b64encode(f"{public}:{secret}".encode()).decode()

    trace_id = args.trace_id
    if not trace_id or args.sessions:
        since = dt.datetime.now(dt.UTC) - dt.timedelta(hours=args.hours)
        recent = fetch_observations(
            host, auth, {"fromStartTime": since.strftime("%Y-%m-%dT%H:%M:%SZ")}
        )
        rows = group_by_trace(recent)
        if args.local_only:
            rows = [r for r in rows if str(r["session"] or "").startswith(LOCAL_SESSION_PREFIX)]
        if not rows:
            print(f"no traces in the last {args.hours}h. Was tracing configured for the run?")
            return 1
        if args.sessions:
            print(f"{'started':<20} {'spans':>5}  {'session':<24} trace")
            for row in rows:
                print(
                    f"{(row['first'] or '')[:19]:<20} {row['spans']:>5}  "
                    f"{str(row['session'] or '-'):<24} {row['trace_id']}  {row['name'] or ''}"
                )
            return 0
        trace_id = rows[0]["trace_id"]

    spans = fetch_observations(host, auth, {"traceId": trace_id})
    if not spans:
        print(f"trace {trace_id} has no observations")
        return 1

    sessions = {s.get("sessionId") for s in spans if s.get("sessionId")}
    names = {s.get("traceName") for s in spans if s.get("traceName")}
    starts = sorted(s["startTime"] for s in spans if s.get("startTime"))
    print(f"trace   {trace_id}")
    print(f"agent   {', '.join(sorted(n for n in names if n)) or '-'}")
    print(f"session {', '.join(sorted(s for s in sessions if s)) or '-'}")
    print(f"started {starts[0] if starts else '-'}")
    print(f"spans   {len(spans)}  {dict(Counter(s.get('type') for s in spans))}")

    errors = [s for s in spans if s.get("level") == "ERROR"]
    if errors:
        print(f"\n=== {len(errors)} ERROR spans ===")
        for span in errors[:10]:
            print(f"  {span.get('name')}: {span.get('statusMessage')}")

    print_transcript(spans)
    print_tools(spans)
    print_latency(spans)
    print_usage(spans)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
