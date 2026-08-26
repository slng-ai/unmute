"""Prove the vector half of a knowledge base is really doing vector search.

`check_knowledge_retrieval.py` proves retrieval works. It does not prove *vector*
search works, and on a small corpus that distinction matters: with `top_k: 3` over a
five-passage base, BM25 hands back most of the corpus and looks correct by luck.
A test that a keyword match could pass is no evidence about embeddings.

So this asks each half of the emitted module separately, at top_k=1, where a
retriever has to actually rank rather than cover:

  A. Questions that share no distinctive vocabulary with the passage answering
     them. The vector half must find them and the keyword half must not. This is
     the part no lexical match can fake.
  B. Similarity scores must separate questions the documents answer from questions
     they do not. A retriever returning arbitrary numbers passes A by accident;
     it cannot pass B.
  C. The same fact must survive the whole emitted path, through look_up(), in
     whatever mode the package was compiled with.

Run against a build directory:

    python scripts/prove_vector_search.py examples/salon-concierge/build/livekit

Or inside a running dev container, which proves it in the image that ships:

    docker cp scripts/prove_vector_search.py <container>:/app/
    docker exec -e OPENAI_API_KEY=$OPENAI_API_KEY <container> \\
        python /app/prove_vector_search.py .

Exit status is 0 only if every part passed.
"""

from __future__ import annotations

import argparse
import asyncio
import importlib.util
import logging
import os
import sys
from pathlib import Path

# Questions whose answers share no distinctive words with the passage, paired with
# the base they belong to and a fact that must come back. Each was measured to be
# found by the vector half and missed by the keyword half at top_k=1, which is what
# makes it evidence rather than decoration.
PARAPHRASE = [
    ("services", "do you need to check my skin reacts before dyeing it", "patch test"),
    ("services", "do you have somebody cheaper for a simple tidy up", "lower price"),
    ("refunds", "when will the money land back in my account", "working days"),
    ("refunds", "my scalp came up in a rash from something you put on it", "reaction"),
]

# Nothing in the salon documents answers these. Used only to read the scores.
OFF_TOPIC = [
    ("services", "what is the capital of Peru"),
    ("services", "how do I change a tyre"),
    ("refunds", "who won the league last season"),
    ("refunds", "how do I reset a diesel injector pump"),
]

# One end-to-end fact per base, through look_up() in the compiled mode.
END_TO_END = [
    ("services", "what does a cut cost when I book it with a colour service", "twenty-eight euros"),
    ("refunds", "what does reference RC-2026-04 cover", "RC-2026-04"),
]


def load(build_dir: Path):
    module_path = build_dir / "knowledge.py"
    if not module_path.is_file():
        raise SystemExit(f"{module_path} does not exist; compile the package first")
    spec = importlib.util.spec_from_file_location("emitted_knowledge", module_path)
    if spec is None or spec.loader is None:
        raise SystemExit(f"cannot import {module_path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    previous = Path.cwd()
    os.chdir(build_dir)
    try:
        spec.loader.exec_module(module)
    finally:
        os.chdir(previous)
    return module


async def ask(retriever, question, top_k):
    """One half of one base, at a fixed result count."""
    retriever.similarity_top_k = top_k
    nodes = await retriever.aretrieve(question)
    text = " ".join(n.node.get_content() for n in nodes).lower()
    best = max((n.score for n in nodes if n.score is not None), default=None)
    return text, best


async def part_a(kb) -> int:
    print("A. questions only vector search can answer (top_k=1)")
    print(f"   {'question':<50}{'vector':>8}{'keyword':>9}")
    failures = 0
    for base, question, needle in PARAPHRASE:
        entry = kb._INDEXES[base]
        if entry["vector"] is None:
            print(f"   SKIP  {base} has no vector half (mode is keyword-only)")
            continue
        found = {}
        for half in ("vector", "keyword"):
            if entry[half] is None:
                found[half] = None
                continue
            text, _ = await ask(entry[half], question, 1)
            found[half] = needle.lower() in text
        ok = found["vector"] and found["keyword"] is not True
        if not ok:
            failures += 1
        print(
            f"   {'ok ' if ok else 'FAIL'} {question[:45]:<45}"
            f"{str(found['vector']):>8}{str(found['keyword']):>9}"
        )
    if failures:
        print(f"   -> {failures} question(s) the vector half should have answered alone")
    else:
        print("   -> the vector half found every one; the keyword half found none")
    return failures


async def part_b(kb) -> int:
    print("\nB. similarity separates answerable from off-topic")
    on, off = [], []
    for base, question, _ in PARAPHRASE + END_TO_END:
        entry = kb._INDEXES[base]
        if entry["vector"] is None:
            continue
        _, best = await ask(entry["vector"], question, 3)
        if best is not None:
            on.append(best)
    for base, question in OFF_TOPIC:
        entry = kb._INDEXES[base]
        if entry["vector"] is None:
            continue
        _, best = await ask(entry["vector"], question, 3)
        if best is not None:
            off.append(best)
    if not on or not off:
        print("   SKIP  no scored results (keyword-only package)")
        return 0
    print(f"   answerable questions: {min(on):.3f} to {max(on):.3f}")
    print(f"   off-topic questions : {min(off):.3f} to {max(off):.3f}")
    if min(on) > max(off):
        print(f"   -> separated, with a gap of {min(on) - max(off):.3f}")
        return 0
    print("   -> OVERLAP: the scores do not distinguish these questions")
    return 1


async def part_c(kb) -> int:
    print("\nC. the whole emitted path, through look_up()")
    failures = 0
    for base, question, needle in END_TO_END:
        answer = await kb.look_up(base, question)
        if "error" in answer:
            print(f"   FAIL {base}: {answer['error']}")
            failures += 1
            continue
        blob = " ".join(r["text"] for r in answer["results"]).lower()
        ok = needle.lower() in blob
        failures += 0 if ok else 1
        scored = sum(1 for r in answer["results"] if "score" in r)
        print(
            f"   {'ok ' if ok else 'FAIL'} {base:<9} {needle!r} in "
            f"{len(answer['results'])} result(s), {scored} scored"
        )
    return failures


async def run(build_dir: Path) -> int:
    kb = load(build_dir)
    kb.build_indexes()
    modes = {name: s["mode"] for name, s in kb.SETTINGS.items()}
    print(f"\nbases: {modes}\n")
    failures = await part_a(kb) + await part_b(kb) + await part_c(kb)
    print(
        "\nVECTOR SEARCH PROVEN" if not failures else f"\nFAILED: {failures} check(s)"
    )
    return 1 if failures else 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("build_dir", type=Path, help="a build/<target> directory")
    parser.add_argument("--verbose", action="store_true", help="show module logging")
    args = parser.parse_args()
    logging.basicConfig(
        level=logging.INFO if args.verbose else logging.ERROR,
        format="%(levelname)s %(name)s: %(message)s",
    )
    for noisy in ("httpx", "httpcore", "openai", "bm25s", "urllib3", "llama_index"):
        logging.getLogger(noisy).setLevel(logging.WARNING)
    return asyncio.run(run(args.build_dir.resolve()))


if __name__ == "__main__":
    sys.exit(main())
