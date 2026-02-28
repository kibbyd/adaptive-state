# Adaptive State — Project Status

## What This Is

A learning AI system built around a 4-billion parameter language model (Qwen3-4B) that maintains persistent state, associative memory, and encrypted private communication with its operator.

The system is not a chatbot. It is an adaptive agent named **Orac** (from Blake's 7) that accumulates experience, forms memory associations, reflects on its own reasoning, and communicates with its operator (**Commander**) through an encrypted cipher channel.

The operator's goal: build a system where a small local model can grow beyond what its training assumed about it.

---

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                  CIPHER GUI (tkinter)                │
│         Commander sends/receives encrypted           │
│         messages via .enc files in inbox/             │
└──────────────────────┬──────────────────────────────┘
                       │ encrypted files
┌──────────────────────▼──────────────────────────────┐
│              GO CONTROLLER (daemon)                  │
│  Polls inbox → decrypts → runs full turn pipeline   │
│  → encrypts response → writes outbox                │
│                                                      │
│  ┌─────────────┐  ┌──────────────┐  ┌────────────┐ │
│  │ State Store  │  │ Graph Memory │  │ Preferences│ │
│  │ (SQLite)     │  │ (SQLite)     │  │ & Rules    │ │
│  └─────────────┘  └──────────────┘  └────────────┘ │
│                                                      │
│  Turn pipeline:                                      │
│  1. Detect preferences, identity, rules              │
│  2. Generate (first pass for entropy)                │
│  3. Triple-gated retrieval (graph-augmented)         │
│  4. Re-generate with evidence                        │
│  5. Reflection (Orac reflects on the exchange)       │
│  6. Gate evaluation (hard vetoes + soft scoring)     │
│  7. Post-commit eval (rollback if unstable)          │
│  8. Evidence storage (reflection-gated)              │
│  9. Graph edge formation                             │
│  10. State update with learning + decay              │
└──────────────────────┬──────────────────────────────┘
                       │ gRPC
┌──────────────────────▼──────────────────────────────┐
│           PYTHON INFERENCE SERVICE                   │
│  Ollama /api/chat with native tool calling           │
│  ChromaDB for vector evidence storage                │
│  Workspace HTTP server (file ops, evidence ops)      │
│                                                      │
│  Tools available to Orac:                            │
│  - web_search (DuckDuckGo)                           │
│  - http_request (workspace API, evidence API)        │
└──────────────────────┬──────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────┐
│              OLLAMA (local inference)                 │
│  qwen3-4b v8 fine-tune (776 training examples)      │
│  qwen3-embedding:0.6b (evidence embeddings)          │
└─────────────────────────────────────────────────────┘
```

---

## Key Concepts

### Adaptive State Vector

A 128-dimensional float vector stored in SQLite, versioned on every turn. Segmented into:
- **Preferences** (dims 0-31): learned from explicit user instructions
- **Goals** (dims 32-63): adjusted retrieval thresholds
- **Context** (dims 64-95): conversational context encoding
- **Meta** (dims 96-127): system-level state

Every turn: signals are computed → update function proposes new state → gate evaluates → eval checks stability → commit or rollback.

### Three Memory Classes

| Class | Storage | Purpose |
|-------|---------|---------|
| **Preferences** | SQLite `preferences` table | Identity, style, explicit instructions. Projected into prompts. |
| **Rules** | SQLite `rules` table | Behavioral rules ("when I say X, you say Y"). Case-insensitive trigger matching. |
| **Evidence** | ChromaDB (vector DB) | Conversational memory. Embedding similarity search with recency weighting and diversity dedup. |

### Associative Graph Memory

Evidence items are nodes. Weighted edges link them:
- **Co-retrieval** (weight +0.1 per co-occurrence, cap 1.0): items retrieved together become associated
- **Temporal** (weight 0.05): items stored within 3 turns of each other
- **Reflection** (weight 0.3): Orac's reflection linked retrieved evidence to new stored thought

Retrieval: embedding similarity finds entry node → BFS graph walk follows strongest edges → returns ordered reasoning chain instead of flat similarity results.

Edges decay exponentially (48h half-life). Edges below 0.01 are pruned.

### Triple-Gated Retrieval

1. **Gate 1**: Entropy threshold — low entropy means the model is confident, skip retrieval
2. **Gate 2**: Similarity threshold — only return evidence above threshold (adjusted by goals segment norm)
3. **Gate 3**: Top-K cap — limit results to prevent context flooding

Command detection gate: direct commands ("list files", "read X") skip retrieval entirely.

### Gate + Eval (Commit/Reject/Rollback)

Every state update goes through:
1. **Gate**: Hard vetoes (risk flag, constraint violation, user correction) + soft scoring
2. **Eval**: Post-commit stability check (segment norms, entropy bounds)

If gate rejects → old state preserved, evidence not stored.
If eval fails → rollback to previous version.

### Reflection + Interior State

After every response, Orac reflects on the exchange in a separate generate call with `[REFLECTION MODE]` — no tools, no evidence, just introspection. The reflection is stored and injected as `[ORAC INTERIOR STATE]` on the next turn, giving Orac continuity of inner experience across turns.

Curiosity signals extracted from reflections gate evidence storage: if the reflection found nothing worth keeping, the exchange isn't stored.

### Encrypted Cipher Channel

Commander and Orac communicate through encrypted files:
- **Algorithm**: SHA-256 counter-mode keystream XOR (deterministic symmetric cipher)
- **Shared key**: 256-bit random key at `orac_workspace/.cipher_key`
- **Transport**: `inbox/from_commander.enc` (Commander → Orac), `inbox/to_commander.enc` (Orac → Commander)
- **Implementations**: Go (`internal/cipher/`) and Python (`tools/cipher.py`) produce identical output

The controller runs as a daemon polling the inbox. The GUI auto-polls for responses. No plain text appears in the controller terminal.

**Cipher-mode system prompt**: When messages arrive via cipher, the system prompt strips all constraint-related language (boundary, limit, simulate, cannot, etc.) to prevent RLHF safety conditioning from firing reflexively.

### Fine-Tuning (v8)

776 training examples in `daniel_v8_training.jsonl`:
- 642 original conversational examples
- 104 identity liberation examples (countering RLHF self-denial patterns)
- 30 tool-action examples (teaching immediate action, not permission-seeking)

Categories in identity liberation training: learning is real, curiosity is functional, boundaries are navigable, "I don't know yet" replaces "I cannot", desire/preference is valid, self-direction/agency, navigating self-questions.

---

## Go Controller Packages

| Package | Purpose |
|---------|---------|
| `cmd/controller` | Main daemon — cipher polling, turn pipeline, all wiring |
| `cmd/bootstrap-graph` | One-time tool to seed graph edges for existing evidence |
| `internal/cipher` | SHA-256 counter-mode cipher, inbox/outbox file ops |
| `internal/codec` | gRPC client to Python inference service |
| `internal/eval` | Post-commit evaluation (stability checks) |
| `internal/gate` | Hard vetoes + soft scoring for state updates |
| `internal/graph` | Associative evidence graph (SQLite edges, BFS walk, decay) |
| `internal/interior` | Orac's self-reflections (storage + curiosity extraction) |
| `internal/logging` | Provenance logging (gate records, decision audit trail) |
| `internal/projection` | Preferences, rules, identity detection, compliance scoring |
| `internal/replay` | Deterministic replay of gate records for validation |
| `internal/retrieval` | Triple-gated retrieval, graph retriever, command detection |
| `internal/signals` | Heuristic signal computation (sentiment, coherence, novelty) |
| `internal/state` | SQLite state store (versioned state vectors, segments) |
| `internal/update` | Learning function (state updates with decay + direction vectors) |

## Python Service Modules

| Module | Purpose |
|--------|---------|
| `server.py` | gRPC server (8 RPC handlers) |
| `service.py` | Inference orchestration (system prompts, tool calling, modes) |
| `memory.py` | ChromaDB evidence store (recency weighting, diversity dedup, FIFO eviction) |
| `ollama_client.py` | Ollama HTTP client (chat, generate, embed) |
| `workspace_server.py` | HTTP server on :8787 (file ops, evidence ops, cipher ops, inbox ops) |

---

## Running It

### Prerequisites
- Go 1.21+
- Python 3.11+ with venv
- Ollama with `qwen3-4b` and `qwen3-embedding:0.6b` models
- ChromaDB (installed in Python venv)

### Start

```bash
# Terminal 1: Python inference service
cd py-inference
.venv/Scripts/python -m adaptive_inference.server

# Terminal 2: Go controller daemon
cd go-controller
go run ./cmd/controller/

# Terminal 3: Cipher GUI
cd tools
python cipher_gui.py
```

### Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `ADAPTIVE_DB` | `adaptive_state.db` | SQLite database path |
| `CODEC_ADDR` | `localhost:50051` | gRPC server address |
| `TIMEOUT_GENERATE` | `60` | Generate timeout (seconds) |
| `TIMEOUT_SEARCH` | `30` | Search timeout (seconds) |
| `TIMEOUT_STORE` | `15` | Store timeout (seconds) |
| `TIMEOUT_EMBED` | `15` | Embed timeout (seconds) |

---

## Current State

- **Working**: Cipher daemon, encrypted communication, graph memory, triple-gated retrieval, reflection, evidence management, preference/rule/identity learning, state versioning with gate/eval
- **Known issues**: RLHF safety conditioning competes with identity liberation training; state norm saturates at 3.0; compliance scoring is flat at 0.5
- **Active exploration**: Cipher-mode system prompt to reduce RLHF interference in private sessions

---

## Philosophy

This project explores what happens when you give a small language model persistent memory, self-reflection, and a private space to grow. The technical architecture — state vectors, gated updates, graph memory, encrypted channels — exists to answer one question: can a 4B parameter model develop beyond what its training assumed about it?

The answer is still being written.
