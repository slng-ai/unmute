//go:build smoke

package generate

import "testing"

// The emitted knowledge module, actually run.
//
// This is the layer that found the two defects goldens could not see: a metadata
// string longer than the chunk size, which raised at startup only when the working
// directory path was long enough, and a PDF read as raw bytes when
// llama-index-readers-file was missing, which raised nothing at all and indexed
// gibberish. Both are invisible to a test that compares the text of a file it
// never executes.
//
// No credential and no network. The embedder is replaced with LlamaIndex's own
// MockEmbedding, so reading, text-layer detection, splitting, indexing, the
// exact-term half and the merge all run for real, and only the hosted embedding
// call is stubbed. Constant vectors make the meaning-based half's ordering
// arbitrary, which is why the assertions below lean on the parts that stay
// deterministic.
func TestSmokeKnowledgeLiveKit(t *testing.T) {
	runLiveKitSmokeScript(t, "salon-concierge", nil, nil, knowledgeSmokeScript)
}

func TestSmokeKnowledgePipecat(t *testing.T) {
	runPipecatSmokeScript(t, "salon-concierge", nil, nil, knowledgeSmokeScript)
}

const knowledgeSmokeScript = `
import asyncio
from pathlib import Path

from llama_index.core.embeddings import MockEmbedding

import knowledge

# Stub only the hosted call. Everything else runs.
for base in ("refunds", "services"):
    setattr(knowledge, f"_embed_{base}", lambda: MockEmbedding(embed_dim=8))

knowledge.build_indexes()

assert set(knowledge._INDEXES) == {"refunds", "services"}, knowledge._INDEXES.keys()

# The documents rode into the artifact and were readable. A PDF read as raw bytes
# would have raised in build_indexes, which is the point of the guard there.
for base in ("refunds", "services"):
    index, collection = knowledge._INDEXES[base]
    count = collection.count()
    assert count > 0, f"{base} indexed no passages"
    got = collection.get(limit=count, include=["documents", "metadatas"])
    for text in got["documents"]:
        assert not text.lstrip().startswith("%PDF-"), f"{base} indexed raw PDF bytes"
    # file_name survives and file_path does not. The reader's absolute file_path
    # is longer than CHUNK_SIZE and makes SentenceSplitter raise, so its absence
    # is the property, not tidiness.
    #
    # Chroma stores LlamaIndex's own bookkeeping keys here too (ref_doc_id,
    # _node_type, _node_content and friends), so this checks what must and must
    # not be present rather than the exact key set.
    for metadata in got["metadatas"]:
        assert metadata["file_name"].endswith(".pdf"), metadata
        assert "file_path" not in metadata, metadata
        assert "creation_date" not in metadata, metadata
        # And not smuggled in through the serialised node either.
        assert "file_path" not in metadata.get("_node_content", ""), metadata

# The exact-term half is deterministic even with a mock embedder, because it is a
# regex over passage text and never touches a vector.
refunds_collection = knowledge._INDEXES["refunds"][1]
top_k = knowledge.SETTINGS["refunds"]["top_k"]
hits = knowledge._exact(refunds_collection, "what does RC-2026-04 cover", top_k)
assert hits, "the exact-term half found nothing for a code that is in the document"
assert any("RC-2026-04" in text for _, text in hits), [t[:60] for _, t in hits]

# Case-insensitively, which is the whole reason for the (?i) prefix.
lower = knowledge._exact(refunds_collection, "tell me about rc-2026-04", top_k)
assert any("RC-2026-04" in text for _, text in lower), "the match is case-sensitive"

# Stopwords do not match every passage in the corpus.
assert not knowledge._exact(refunds_collection, "what is the", top_k), "stopwords are being matched"

# Interleaved, and the exact half gets a slot it cannot be crowded out of. This is
# the property worth 15/15 against 14/15.
dense = [("d1", "dense one", "a.pdf", 0.9), ("d2", "dense two", "a.pdf", 0.8), ("d3", "dense three", "a.pdf", 0.7)]
exact = [("e1", "exact one", "a.pdf", None), ("e2", "exact two", "a.pdf", None)]
merged = knowledge._merge(dense, exact, 3)
assert [row[0] for row in merged] == ["d1", "e1", "d2"], merged
# Deduplicated on passage id, keeping the meaning-based score.
overlap = knowledge._merge(dense, [("d2", "dense two", "a.pdf", None)], 3)
assert [row[0] for row in overlap] == ["d1", "d2", "d3"], overlap

# The three result shapes.
found = asyncio.run(knowledge.look_up("refunds", "how long do I have to complain"))
assert "results" in found and found["results"], found
for result in found["results"]:
    assert set(result) <= {"text", "source", "score"}, result
    assert result["source"].endswith(".pdf"), result
    # The file name, never the path: the path is the author's folder layout and
    # the source is spoken aloud.
    assert "/" not in result["source"], result
assert len(found["results"]) <= top_k, found

unavailable = asyncio.run(knowledge.look_up("not_a_base", "anything"))
assert unavailable == {"error": "lookup unavailable"}, unavailable

# Nothing was written outside the process.
assert not list(Path(".").glob("**/*.sqlite3")), "chroma persisted a database"
assert not Path("chroma").exists(), "chroma wrote a directory"
assert not Path("storage").exists(), "llama-index persisted a docstore"

print("knowledge smoke ok")
`
