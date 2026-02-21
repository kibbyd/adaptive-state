"""Tests for MemoryStore — ChromaDB wrapper."""

import asyncio
import json
import os
import tempfile
from unittest.mock import AsyncMock, patch

import pytest

from adaptive_inference.memory import MemoryStore, SearchResult


@pytest.fixture
def tmp_persist_dir():
    d = tempfile.mkdtemp()
    yield d
    # ChromaDB holds file locks on Windows; ignore cleanup errors
    import shutil
    shutil.rmtree(d, ignore_errors=True)


@pytest.fixture
def fake_embedding():
    """Return a deterministic 10-dim embedding for testing."""
    return [0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0]


@pytest.fixture
def store(tmp_persist_dir):
    return MemoryStore(persist_dir=tmp_persist_dir, collection_name="test")


def run(coro):
    return asyncio.get_event_loop().run_until_complete(coro)


class TestMemoryStore:
    @patch("adaptive_inference.memory.ollama_client.embed", new_callable=AsyncMock)
    def test_store_returns_id(self, mock_embed, store, fake_embedding):
        mock_embed.return_value = fake_embedding
        doc_id = run(store.store("hello world", {"source": "test"}))
        assert isinstance(doc_id, str)
        assert len(doc_id) == 36  # UUID format

    @patch("adaptive_inference.memory.ollama_client.embed", new_callable=AsyncMock)
    def test_store_and_search(self, mock_embed, store, fake_embedding):
        mock_embed.return_value = fake_embedding

        # Store a document
        doc_id = run(store.store("test document", {"source": "unit_test"}))

        # Search — same embedding should return similarity ~1.0
        results = run(store.search("test document", top_k=5, threshold=0.0))
        assert len(results) == 1
        assert results[0].id == doc_id
        assert results[0].text == "test document"
        assert results[0].score > 0.99  # cosine similarity of identical vectors

    @patch("adaptive_inference.memory.ollama_client.embed", new_callable=AsyncMock)
    def test_search_empty_collection(self, mock_embed, store, fake_embedding):
        mock_embed.return_value = fake_embedding
        results = run(store.search("anything", top_k=5, threshold=0.0))
        assert results == []

    @patch("adaptive_inference.memory.ollama_client.embed", new_callable=AsyncMock)
    def test_search_threshold_filters(self, mock_embed, store):
        # Store with one embedding, search with a different one
        mock_embed.side_effect = [
            [1.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0],  # store
            [0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 1.0],  # search (orthogonal)
        ]
        run(store.store("doc A", {"source": "test"}))

        # High threshold should filter out low-similarity results
        results = run(store.search("unrelated query", top_k=5, threshold=0.9))
        assert len(results) == 0

    @patch("adaptive_inference.memory.ollama_client.embed", new_callable=AsyncMock)
    def test_delete(self, mock_embed, store, fake_embedding):
        mock_embed.return_value = fake_embedding

        doc_id = run(store.store("to be deleted", {"source": "test"}))
        assert run(store.delete(doc_id)) is True

        # Search should return empty after delete
        results = run(store.search("to be deleted", top_k=5, threshold=0.0))
        assert len(results) == 0

    @patch("adaptive_inference.memory.ollama_client.embed", new_callable=AsyncMock)
    def test_multiple_store_and_search(self, mock_embed, store):
        embeddings = [
            [1.0, 0.0, 0.0, 0.0, 0.0],  # doc A
            [0.9, 0.1, 0.0, 0.0, 0.0],  # doc B (similar to A)
            [0.0, 0.0, 0.0, 0.0, 1.0],  # doc C (different)
            [1.0, 0.0, 0.0, 0.0, 0.0],  # query (same as A)
        ]
        mock_embed.side_effect = embeddings

        run(store.store("doc A", {"source": "a"}))
        run(store.store("doc B", {"source": "b"}))
        run(store.store("doc C", {"source": "c"}))

        results = run(store.search("query like A", top_k=3, threshold=0.5))
        # A and B should be above threshold, C should be filtered
        assert len(results) >= 1
        # First result should be most similar (doc A)
        assert results[0].score > 0.8

    @patch("adaptive_inference.memory.ollama_client.embed", new_callable=AsyncMock)
    def test_metadata_roundtrip(self, mock_embed, store, fake_embedding):
        mock_embed.return_value = fake_embedding
        metadata = {"source": "user", "turn_id": "turn-5"}

        run(store.store("metadata test", metadata))
        results = run(store.search("metadata test", top_k=1, threshold=0.0))

        assert len(results) == 1
        parsed = json.loads(results[0].metadata_json)
        assert parsed["source"] == "user"
        assert parsed["turn_id"] == "turn-5"
