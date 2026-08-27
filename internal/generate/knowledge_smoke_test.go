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
// keyword half and the merge all run for real, and only the hosted embedding
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

# Both salon bases are hybrid, so both halves get built. A single-mode base carries
# None in the other slot, which is what look_up's None checks are for.
for base in ("refunds", "services"):
    entry = knowledge._INDEXES[base]
    assert set(entry) == {"vector", "keyword"}, entry.keys()
    assert entry["vector"] is not None, f"{base} built no vector retriever"
    assert entry["keyword"] is not None, f"{base} built no keyword retriever"

# The documents rode into the artifact and were readable. A PDF read as raw bytes
# would have raised in _with_text, which is the point of the guard there.
#
# Read from the passages themselves. The store is LlamaIndex's own in-memory
# SimpleVectorStore now, with no collection to query, and _nodes is the same
# function _index splits with, so this is the content that got indexed.
for base in ("refunds", "services"):
    nodes = knowledge._nodes(base)
    assert nodes, f"{base} split into no passages"
    for node in nodes:
        text = node.get_content()
        assert not text.lstrip().startswith("%PDF-"), f"{base} indexed raw PDF bytes"
        # file_name survives and nothing else does. The reader's absolute
        # file_path is longer than the chunk size and makes SentenceSplitter
        # raise, so its absence is the property, not tidiness.
        assert set(node.metadata) == {"file_name"}, node.metadata
        assert node.metadata["file_name"].endswith(".pdf"), node.metadata
        # Hidden from the embedding and from the model, which is what decouples
        # chunk_size from how long a file name happens to be.
        assert node.excluded_embed_metadata_keys == ["file_name"], node.metadata
        assert node.excluded_llm_metadata_keys == ["file_name"], node.metadata

# The keyword half is deterministic even with a mock embedder, because BM25 ranks
# the words themselves and never touches a vector.
#
# It is asserted on ranking, not membership. The regex scan this replaced either
# matched a passage or did not, so "stopwords match nothing" was a property it
# had; BM25 scores every passage and returns its best top_k, so the equivalent
# claim is that the passage carrying the caller's own term comes back first.
keyword = knowledge._INDEXES["refunds"]["keyword"]
top_k = knowledge.SETTINGS["refunds"]["top_k"]
hits = keyword.retrieve("what does RC-2026-04 cover")
assert hits, "the keyword half found nothing for a code that is in the document"
assert len(hits) <= top_k, f"{len(hits)} hits for a top_k of {top_k}"
assert "RC-2026-04" in hits[0].node.get_content(), [
    hit.node.get_content()[:60] for hit in hits
]

# Case-insensitively, which BM25 gets from lowercasing both sides.
lower = keyword.retrieve("tell me about rc-2026-04")
assert any("RC-2026-04" in hit.node.get_content() for hit in lower), (
    "the match is case-sensitive"
)

# Interleaved, and the keyword half gets a slot it cannot be crowded out of. This is
# the property worth 15/15 against 14/15.
dense = [("d1", "dense one", "a.pdf", 0.9), ("d2", "dense two", "a.pdf", 0.8), ("d3", "dense three", "a.pdf", 0.7)]
keyword_rows = [("e1", "exact one", "a.pdf", None), ("e2", "exact two", "a.pdf", None)]
merged = knowledge._merge(dense, keyword_rows, 3)
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

# Nothing was written outside the process. The vector store is in memory and dies
# with the process, and a startup build never bakes an index.
assert not list(Path(".").glob("**/*.sqlite3")), "a database was persisted"
assert not Path("storage").exists(), "llama-index persisted a docstore"
assert not Path("knowledge_index").exists(), "the startup build wrote a baked index"

print("knowledge smoke ok")
`
