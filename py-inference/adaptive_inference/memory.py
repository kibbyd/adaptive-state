"""ChromaDB-backed memory store for evidence retrieval."""

import os
import uuid
import json
import logging
import time
from dataclasses import dataclass

import chromadb

from . import ollama_client

logger = logging.getLogger(__name__)

# Max evidence items before FIFO eviction kicks in
MAX_EVIDENCE = int(os.environ.get("MAX_EVIDENCE", "500"))

# Recency half-life in seconds (evidence older than this gets 50% weight)
RECENCY_HALF_LIFE = float(os.environ.get("RECENCY_HALF_LIFE", str(6 * 3600)))  # 6 hours

# Diversity threshold: results more similar than this to an already-selected result are deduped
DIVERSITY_THRESHOLD = float(os.environ.get("DIVERSITY_THRESHOLD", "0.9"))


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

    def __init__(self, persist_dir: str, collection_name: str = "evidence", embed_model: str = "qwen3-embedding:0.6b"):
        self._client = chromadb.PersistentClient(path=persist_dir)
        self._collection = self._client.get_or_create_collection(
            name=collection_name,
            metadata={"hnsw:space": "cosine"},
        )
        self._model = embed_model
        self._base_url = ollama_client.DEFAULT_BASE_URL
        logger.info(
            "MemoryStore initialized: persist_dir=%s, collection=%s",
            persist_dir, collection_name,
        )

    async def store(self, text: str, metadata: dict) -> str:
        """Embed text via Ollama and store in ChromaDB. Returns document ID.
        Enforces FIFO eviction when collection exceeds MAX_EVIDENCE."""
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

        # FIFO eviction: remove oldest items if over capacity
        self._evict_if_over_capacity()

        return doc_id

    def _evict_if_over_capacity(self) -> None:
        """Remove oldest evidence items when collection exceeds MAX_EVIDENCE."""
        count = self._collection.count()
        if count <= MAX_EVIDENCE:
            return

        excess = count - MAX_EVIDENCE
        # Get all items with metadata to find oldest by stored_at
        all_items = self._collection.get(include=["metadatas"])
        if not all_items["ids"]:
            return

        # Sort by stored_at timestamp (oldest first), fallback to ID order
        items_with_time = []
        for i, doc_id in enumerate(all_items["ids"]):
            meta = all_items["metadatas"][i] if all_items["metadatas"] else {}
            stored_at = meta.get("stored_at", "1970-01-01T00:00:00Z") if meta else "1970-01-01T00:00:00Z"
            items_with_time.append((doc_id, stored_at))

        items_with_time.sort(key=lambda x: x[1])  # oldest first
        ids_to_delete = [item[0] for item in items_with_time[:excess]]

        if ids_to_delete:
            self._collection.delete(ids=ids_to_delete)
            logger.info("FIFO eviction: removed %d oldest evidence items (collection was %d, cap %d)",
                        len(ids_to_delete), count, MAX_EVIDENCE)

    async def search(
        self, query_text: str, top_k: int = 5, threshold: float = 0.0
    ) -> list[SearchResult]:
        """Embed query via Ollama, search ChromaDB, return results above threshold.
        Applies recency weighting and diversity deduplication."""
        count = self._collection.count()
        if count == 0:
            return []

        embedding = await ollama_client.embed(
            text=query_text, model=self._model, base_url=self._base_url,
        )

        # Fetch more than top_k to allow diversity filtering to still yield enough
        fetch_k = min(top_k * 3, count)

        results = self._collection.query(
            query_embeddings=[embedding],
            n_results=fetch_k,
            include=["documents", "distances", "metadatas"],
        )

        candidates: list[SearchResult] = []
        if not results["ids"] or not results["ids"][0]:
            return candidates

        now = time.time()

        for i, doc_id in enumerate(results["ids"][0]):
            # ChromaDB cosine distance: 0 = identical, 2 = opposite
            # Convert to similarity: similarity = 1 - distance
            distance = results["distances"][0][i]
            similarity = 1.0 - distance

            if similarity < threshold:
                continue

            metadata = results["metadatas"][0][i] if results["metadatas"] else {}

            # Recency weighting: decay score based on age
            recency_weight = self._recency_weight(metadata, now)
            weighted_score = similarity * recency_weight

            candidates.append(SearchResult(
                id=doc_id,
                text=results["documents"][0][i],
                score=weighted_score,
                metadata_json=json.dumps(metadata) if metadata else "{}",
            ))

        # Sort by weighted score descending
        candidates.sort(key=lambda r: r.score, reverse=True)

        # Diversity dedup: remove near-duplicate results
        diverse = self._diversity_filter(candidates, top_k)
        return diverse

    @staticmethod
    def _recency_weight(metadata: dict, now: float) -> float:
        """Compute recency weight from stored_at metadata. Returns 0.5-1.0.
        Evidence at half-life age gets weight 0.75. Very old evidence floors at 0.5."""
        stored_at = (metadata or {}).get("stored_at", "")
        if not stored_at:
            return 0.75  # No timestamp — neutral weight

        try:
            from datetime import datetime, timezone
            dt = datetime.fromisoformat(stored_at.replace("Z", "+00:00"))
            age_seconds = now - dt.timestamp()
        except (ValueError, TypeError):
            return 0.75  # Unparseable — neutral weight

        if age_seconds <= 0:
            return 1.0

        # Exponential decay: weight = 0.5 + 0.5 * exp(-age / half_life)
        # At age=0: weight=1.0, at age=half_life: weight≈0.80, at age=3*half_life: weight≈0.53
        import math
        decay = math.exp(-age_seconds / RECENCY_HALF_LIFE)
        return 0.5 + 0.5 * decay

    @staticmethod
    def _diversity_filter(candidates: list["SearchResult"], top_k: int) -> list["SearchResult"]:
        """Greedy diversity filter: skip candidates whose text is too similar to
        already-selected results. Uses character-level Jaccard similarity as a fast proxy."""
        if not candidates:
            return []

        selected: list[SearchResult] = []
        selected_tokens: list[set[str]] = []

        for cand in candidates:
            if len(selected) >= top_k:
                break

            cand_tokens = set(cand.text.lower().split())

            # Check against already-selected results
            is_duplicate = False
            for sel_tokens in selected_tokens:
                if not cand_tokens or not sel_tokens:
                    continue
                intersection = len(cand_tokens & sel_tokens)
                union = len(cand_tokens | sel_tokens)
                jaccard = intersection / union if union > 0 else 0.0
                if jaccard > DIVERSITY_THRESHOLD:
                    is_duplicate = True
                    break

            if not is_duplicate:
                selected.append(cand)
                selected_tokens.append(cand_tokens)

        return selected

    def get_by_ids(self, ids: list[str]) -> list[SearchResult]:
        """Fetch evidence items by their IDs. Returns results in input order."""
        if not ids:
            return []

        result = self._collection.get(ids=ids, include=["documents", "metadatas"])
        if not result["ids"]:
            return []

        # Build lookup for preserving input order
        lookup: dict[str, SearchResult] = {}
        for i, doc_id in enumerate(result["ids"]):
            meta = result["metadatas"][i] if result["metadatas"] else {}
            lookup[doc_id] = SearchResult(
                id=doc_id,
                text=result["documents"][i] if result["documents"] else "",
                score=0.0,
                metadata_json=json.dumps(meta) if meta else "{}",
            )

        # Return in input order, skipping missing IDs
        return [lookup[id] for id in ids if id in lookup]

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
