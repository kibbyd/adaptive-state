# Show HN: Adaptive State — persistent personality for frozen LLMs without weight modification

Adaptive State is an open-source system that gives a frozen LLM persistent memory, learned preferences, and behavioral rules — managed entirely outside the model.

A Go controller maintains a 128-dim adaptive state vector with signal-driven learning, gated updates, decay, and rollback. A Python inference service handles generation via Ollama. The model sees human-readable preferences, not raw vectors.

Three memory classes, fully separated:
- **Preferences** (SQLite): identity, style preferences, projected into every prompt
- **Evidence** (ChromaDB): semantic retrieval with recency weighting, diversity dedup, FIFO eviction
- **Rules** (SQLite): "when I say X, respond Y" — auto-extracted, match-injected, isolated system prompt

Key design decisions:
- Removed Ollama context threading entirely. Evidence retrieval is the sole source of conversational memory. Opaque token context caused uncontrollable bleed.
- Rules fire with a stripped system prompt (no preferences, no evidence, no tool instructions). Otherwise the model editorializes instead of complying.
- Identity detection ("my name is X") is hardened against false positives with stopword filters, punctuation guards, and word count caps.

Result: a 21-turn live conversation on qwen3-4b (2.6GB, local) with coherent self-naming, personality persistence, humor, and philosophical depth. No fine-tuning.

95-100% test coverage on 11 Go packages. Deterministic replay system. Full provenance logging.

Stack: Go + Python + Ollama + SQLite + ChromaDB + gRPC

https://github.com/kibbyd/adaptive-state
