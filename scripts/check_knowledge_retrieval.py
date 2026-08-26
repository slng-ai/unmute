"""Prove a compiled package's knowledge retrieval works, with nobody on the phone.

This is the knowledge-base equivalent of `replay_router_scopes.py`: SELF_VERIFY's
first rule is to reproduce a defect in the layer it lives in, and retrieval is two
layers below a phone call. If a live call gives a vague answer, run this first. It
imports the emitted `knowledge.py` from a build directory, builds the indexes
exactly as the agent does at startup, and asks questions straight at `look_up`.

What it can prove:
  - the documents were compiled in and are readable
  - the indexes build (which is where a missing dependency or a bad credential
    shows up, in seconds rather than mid-call)
  - a question retrieves a passage containing an expected fact
  - a question the documents do not answer retrieves nothing convincing

What it cannot prove: that the model chose to call the tool, or that it read the
result out correctly. Those need the live call in docs/HARNESS_TEST.md.

Run it inside the built image, which is the environment that ships:

    docker build -t kb examples/salon-concierge/build/livekit
    docker run --rm -e OPENAI_API_KEY=... -v "$PWD/scripts:/s" kb \\
        python /s/check_knowledge_retrieval.py . \\
        refunds "what does reference RC-2026-04 cover" "RC-2026-04"

Or against a local virtualenv that has the project's dependencies installed:

    python scripts/check_knowledge_retrieval.py examples/salon-concierge/build/livekit \\
        services "what does a cut cost with a colour service" "twenty-eight euros"

Exit status is 0 only if every check passed, so it works as a gate.
"""

from __future__ import annotations

import argparse
import asyncio
import importlib.util
import logging
import os
import sys
from pathlib import Path


def load_knowledge(build_dir: Path):
    """Import the emitted knowledge.py from a build directory.

    Imported by path rather than by name so this runs against any build directory
    without installing it, and so two build directories can never shadow each
    other in sys.modules.
    """
    module_path = build_dir / "knowledge.py"
    if not module_path.is_file():
        raise SystemExit(
            f"{module_path} does not exist. Either the package declares no "
            "knowledge: section, or it has not been compiled yet."
        )
    spec = importlib.util.spec_from_file_location("emitted_knowledge", module_path)
    if spec is None or spec.loader is None:
        raise SystemExit(f"cannot import {module_path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    # KNOWLEDGE_ROOT resolves relative to the module file, so the documents are
    # found wherever this is run from. chdir anyway: the emitted module is written
    # to be run from its own directory and this keeps that assumption honest.
    previous = Path.cwd()
    os.chdir(build_dir)
    try:
        spec.loader.exec_module(module)
    finally:
        os.chdir(previous)
    return module


def parse_checks(triples: list[str]) -> list[tuple[str, str, str]]:
    if len(triples) % 3 != 0:
        raise SystemExit(
            "checks come in threes: base, question, expected substring. "
            f"Got {len(triples)} value(s)."
        )
    return [tuple(triples[i : i + 3]) for i in range(0, len(triples), 3)]  # type: ignore[misc]


async def run(module, checks: list[tuple[str, str, str]], verbose: bool) -> int:
    failures = 0
    for base, question, expected in checks:
        answer = await module.look_up(base, question)
        if "error" in answer:
            print(f"  ERROR  [{base}] {question}\n         -> {answer['error']}")
            failures += 1
            continue
        results = answer["results"]
        haystack = " ".join(r["text"] for r in results).lower()
        # An empty expectation is a negative control: the documents should not
        # answer this, and the check passes when nothing convincing comes back.
        if expected == "":
            scores = [r["score"] for r in results if "score" in r]
            best = max(scores) if scores else None
            verdict = "CONTROL"
            print(
                f"  {verdict}  [{base}] {question}\n"
                f"         -> {len(results)} result(s), best score "
                f"{best if best is None else round(best, 3)}; read the agent's answer, "
                "not this line: the test is whether it declines"
            )
        elif expected.lower() in haystack:
            print(f"  HIT     [{base}] {question}\n         -> found {expected!r}")
        else:
            sources = ", ".join(sorted({r["source"] for r in results})) or "nothing"
            print(
                f"  MISS    [{base}] {question}\n"
                f"         -> {expected!r} not in {len(results)} result(s) from {sources}"
            )
            failures += 1
        if verbose:
            for n, result in enumerate(results, start=1):
                score = result.get("score")
                head = " ".join(result["text"].split())[:110]
                print(
                    f"           {n}. {result['source']}"
                    f"{'' if score is None else f' score={score}'}\n             {head}"
                )
    return failures


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Ask an emitted knowledge base questions directly, with no call.",
        epilog="Each check is three values: base, question, expected substring. "
        "An empty expected substring marks a negative control.",
    )
    parser.add_argument("build_dir", type=Path, help="a build/<target> directory")
    parser.add_argument("checks", nargs="*", help="base question expected [base ...]")
    parser.add_argument(
        "--verbose", action="store_true", help="print every retrieved passage"
    )
    parser.add_argument(
        "--quiet-index",
        action="store_true",
        help="hide the module's own startup logging",
    )
    args = parser.parse_args()

    logging.basicConfig(
        level=logging.WARNING if args.quiet_index else logging.INFO,
        format="%(levelname)s %(name)s: %(message)s",
    )
    for noisy in ("httpx", "httpcore", "openai", "bm25s", "urllib3"):
        logging.getLogger(noisy).setLevel(logging.WARNING)

    checks = parse_checks(args.checks)
    module = load_knowledge(args.build_dir.resolve())

    print("building indexes (this is what the agent does at startup)")
    module.build_indexes()

    if not checks:
        print("\nindexes built. No checks given, so nothing was asked.")
        return 0

    print(f"\nasking {len(checks)} question(s)")
    failures = asyncio.run(run(module, checks, args.verbose))
    real = sum(1 for _, _, expected in checks if expected != "")
    print(f"\n{real - failures}/{real} expected fact(s) retrieved")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
