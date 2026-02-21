"""ChromaDB-backed memory store for evidence retrieval."""

import uuid
import json
import logging
from dataclasses import dataclass

import chromadb

from . import ollama_client

logger = logging.getLogger(__name__)


# #region types
@dataclass
class SearchResult:
    id: str
    text: str
    score: float
    metadata_json: str
# #endregion types


# #region memory-store
class MemoryStore:
    """Wraps ChromaDB for storing and searching evidence documents."""

    def __init__(self, persist_dir: str, collection_name: str = "evidence"):
        self._client = chromadb.PersistentClient(path=persist_dir)
        self._collection = self._client.get_or_create_collection(
            name=collection_name,
            metadata={"hnsw:space": "cosine"},
        )
        self._model = ollama_client.DEFAULT_MODEL
        self._base_url = ollama_client.DEFAULT_BASE_URL
        logger.info(
            "MemoryStore initialized: persist_dir=%s, collection=%s",
            persist_dir, collection_name,
        )

    async def store(self, text: str, metadata: dict) -> str:
        """Embed text via Ollama and store in ChromaDB. Returns document ID."""
        doc_id = str(uuid.uuid4())
        embedding = await ollama_client.embed(
            text=text, model=self._model, base_url=self._base_url,
        )
        self._collection.add(
            ids=[doc_id],
            embeddings=[embedding],
            documents=[text],
            metadatas=[metadata] if metadata else None,
        )
        logger.info("Stored evidence id=%s, len=%d", doc_id, len(text))
        return doc_id

    async def search(
        self, query_text: str, top_k: int = 5, threshold: float = 0.0
    ) -> list[SearchResult]:
        """Embed query via Ollama, search ChromaDB, return results above threshold."""
        count = self._collection.count()
        if count == 0:
            return []

        embedding = await ollama_client.embed(
            text=query_text, model=self._model, base_url=self._base_url,
        )

        # ChromaDB n_results cannot exceed collection size
        effective_k = min(top_k, count)

        results = self._collection.query(
            query_embeddings=[embedding],
            n_results=effective_k,
            include=["documents", "distances", "metadatas"],
        )

        out: list[SearchResult] = []
        if not results["ids"] or not results["ids"][0]:
            return out

        for i, doc_id in enumerate(results["ids"][0]):
            # ChromaDB cosine distance: 0 = identical, 2 = opposite
            # Convert to similarity: similarity = 1 - distance
            distance = results["distances"][0][i]
            similarity = 1.0 - distance

            if similarity < threshold:
                continue

            metadata = results["metadatas"][0][i] if results["metadatas"] else {}
            out.append(SearchResult(
                id=doc_id,
                text=results["documents"][0][i],
                score=similarity,
                metadata_json=json.dumps(metadata) if metadata else "{}",
            ))

        # Sort by score descending
        out.sort(key=lambda r: r.score, reverse=True)
        return out

    async def delete(self, doc_id: str) -> bool:
        """Delete a document by ID. Returns True if successful."""
        try:
            self._collection.delete(ids=[doc_id])
            logger.info("Deleted evidence id=%s", doc_id)
            return True
        except Exception as e:
            logger.error("Delete failed for id=%s: %s", doc_id, e)
            return False
# #endregion memory-store
