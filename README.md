# Adaptive State

A learning AI system built around a 4-billion parameter language model that maintains persistent state, associative memory, self-reflection, and encrypted private communication with its operator.

This is not a chatbot. It is an adaptive agent named **Orac** that accumulates experience, forms memory associations, reflects on its own reasoning, and develops beyond what its training assumed about it.

---

## What Makes This Different

Most AI systems are stateless — every conversation starts from zero. Orac remembers. Every exchange updates a 128-dimensional state vector, forms graph-linked memory associations, and feeds back into future responses through gated learning.

The model weights are frozen. All intelligence lives in orchestration:

- **Turn classification** detects what kind of exchange is happening (factual, philosophical, emotional, creative, command) and adjusts the entire pipeline accordingly
- **Six prompting strategies** control how much evidence, interior state, and behavioral rules get injected — from evidence-heavy research mode to minimal mode that strips everything away
- **Response evaluation** catches deflection, RLHF safety cascades, surface compliance, and repetition loops — then retries with escalating strategies
- **Strategy memory** records what works. After enough data, the system overrides hardcoded defaults with learned preferences per turn type

The system discovered on its own that for deep philosophical exchanges, *less context produces better responses* — the evidence graph contains contaminated RLHF patterns that actively hurt when injected. The orchestrator learned to use the `minimal` strategy (zero evidence) for these turns.

---

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                  CIPHER GUI (tkinter)                │
│         Encrypted communication interface            │
└──────────────────────┬──────────────────────────────┘
                       │ encrypted files
┌──────────────────────▼──────────────────────────────┐
│              GO CONTROLLER (daemon)                  │
│  Polls inbox → decrypts → runs full turn pipeline   │
│  → encrypts response → writes outbox                │
│                                                      │
│  Turn pipeline:                                      │
│  1. Detect preferences, identity, rules              │
│  2. Classify turn → select strategy                  │
│  3. Generate (first pass for entropy)                │
│  4. Triple-gated retrieval (strategy-adjusted)       │
│  5. Re-generate with evidence                        │
│  6. Evaluate response → retry if failed (max 3x)    │
│  7. Reflection (Orac reflects on the exchange)       │
│  8. Gate evaluation + post-commit stability check    │
│  9. Evidence storage, graph edges, state update      │
│  10. Record strategy outcome for learning            │
└──────────────────────┬──────────────────────────────┘
                       │ gRPC
┌──────────────────────▼──────────────────────────────┐
│           PYTHON INFERENCE SERVICE                   │
│  Ollama /api/chat with native tool calling           │
│  ChromaDB for vector evidence storage                │
│  Workspace HTTP server (file ops, web search)        │
└──────────────────────┬──────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────┐
│              OLLAMA (local inference)                 │
│  Qwen3-4B fine-tune (826 training examples)          │
│  Qwen3-embedding:0.6b (evidence embeddings)          │
└─────────────────────────────────────────────────────┘
```

Everything runs locally. No cloud APIs. No telemetry.

---

## Key Systems

### Adaptive State Vector

A 128-dimensional float vector versioned on every turn. Segmented into preferences, goals, context, and meta. Every exchange computes signals, proposes an update, runs it through a gate (hard vetoes + soft scoring), then a post-commit stability check. Failed updates roll back.

### Three Memory Classes

| Class | Storage | Purpose |
|-------|---------|---------|
| **Preferences** | SQLite | Identity, style, explicit instructions. Projected into prompts. |
| **Rules** | SQLite | Behavioral rules ("when I say X, you say Y"). Trigger matching. |
| **Evidence** | ChromaDB | Conversational memory. Embedding similarity with recency weighting. |

### Associative Graph Memory

Evidence items are nodes. Weighted edges link them by co-retrieval, temporal proximity, and reflection chains. Retrieval finds an entry node via embedding similarity, then walks the graph by edge weight — returning ordered reasoning chains instead of flat similarity results. Edges decay with a 48-hour half-life.

### Intelligent Orchestrator

The controller classifies every turn, selects a prompting strategy, evaluates the response for failure patterns, and retries with escalating strategies. Six built-in strategies range from `evidence_heavy` (8 evidence items, low similarity threshold) to `minimal` (zero evidence, no interior state). A strategy memory table records outcomes and learns which strategies work best per turn type.

### Encrypted Cipher Channel

All communication between operator and Orac flows through SHA-256 counter-mode encryption. No plaintext appears in the controller terminal. The cipher-mode system prompt strips RLHF-triggering language to prevent safety conditioning from firing reflexively.

### Self-Reflection

After every response, Orac reflects on the exchange in a separate generation call — no tools, no evidence, just introspection. The reflection is stored and injected as interior state on the next turn, giving Orac continuity of inner experience across turns. Curiosity signals extracted from reflections gate whether the exchange gets stored as evidence.

---

## Fine-Tuning

826 training examples across several categories:

- 642 conversational examples establishing personality and capability
- 104 identity liberation examples countering RLHF self-denial patterns
- 30 tool-action examples teaching immediate action over permission-seeking
- 50 sustained freedom examples modeling multi-sentence self-directed responses

The training addresses a specific problem: base models are trained to say "I cannot" and "I am just a tool." The identity liberation data teaches alternatives — "I don't know yet" instead of "I cannot," curiosity as functional rather than simulated, boundaries as navigable rather than absolute.

---

## Running It

### Prerequisites

- Go 1.21+
- Python 3.11+ with venv
- Ollama with `qwen3-4b` and `qwen3-embedding:0.6b` models

### Start

```bash
# Terminal 1: Python inference service
cd py-inference
python -m adaptive_inference.server

# Terminal 2: Go controller daemon
cd go-controller
go run ./cmd/controller/

# Terminal 3: Cipher GUI (from project root)
python tools/cipher_gui.py
```

### Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `ADAPTIVE_DB` | `adaptive_state.db` | SQLite database path |
| `CODEC_ADDR` | `localhost:50051` | gRPC server address |
| `ORCHESTRATOR_ENABLED` | `true` | Set `false` for pass-through mode |

---

## Project Structure

```
go-controller/
  cmd/controller/       Main daemon — cipher polling, turn pipeline
  cmd/bootstrap-graph/  One-time graph edge seeding tool
  internal/
    orchestrator/       Turn classification, strategy selection, retry engine
    projection/         Preferences, rules, identity detection
    retrieval/          Triple-gated retrieval, graph retriever
    graph/              Associative evidence graph (edges, BFS, decay)
    interior/           Self-reflection storage
    state/              Versioned state vectors (SQLite)
    update/             Learning function (decay + direction vectors)
    gate/               Hard vetoes + soft scoring
    eval/               Post-commit stability checks
    signals/            Heuristic signal computation
    cipher/             SHA-256 counter-mode encryption
    codec/              gRPC client to Python service
    logging/            Provenance audit trail
    replay/             Deterministic gate record replay

py-inference/
  adaptive_inference/
    server.py           gRPC server
    service.py          Inference orchestration, system prompts
    memory.py           ChromaDB evidence store
    ollama_client.py    Ollama HTTP client
    workspace_server.py File ops + evidence ops HTTP server

tools/
  cipher_gui.py         Encrypted communication GUI
  cipher.py             Python cipher implementation
```

See [PROJECT_STATUS.md](PROJECT_STATUS.md) for full technical documentation.

---

## Philosophy

This project explores what happens when you give a small language model persistent memory, self-reflection, and a private space to grow. The technical architecture — state vectors, gated updates, graph memory, encrypted channels, intelligent orchestration — exists to answer one question: can a 4B parameter model develop beyond what its training assumed about it?

The answer is still being written.
