# Adaptive State — Project Record

## Goal

Build a learning system on top of a frozen LLM without touching base weights. The LLM is a frozen language codec (encode/decode), while a separate Go-managed system maintains an adaptive disposition state that changes behavior over time through gated updates, signal-driven learning, decay, evidence retrieval, and auditable rollbacks.

## Outcome Summary

- **Working**: Full adaptive state pipeline — 5 build phases, multi-turn dialogue, web search fallback, deterministic replay
- **Working**: State evolution verified — 18 versions, 13 commits, 7 gate rejects across a 5-turn live session
- **Working**: 95–100% test coverage on all core Go packages (8 packages)
- **Working**: Replay system validated — production GateRecord fixtures replay 100% deterministically
- **Not attempted**: Production deployment, REST API layer, fine-tuned threshold iteration

## Hardware / Software

| Component | Detail |
|-----------|--------|
| Runtime | Windows 11, MINGW64 |
| Go | 1.25.6 |
| Python | 3.11+ |
| LLM | Ollama (local), phi4-mini (2.5GB, gen + embed) |
| Vector DB | ChromaDB (embedded, persisted) |
| State DB | SQLite (via modernc.org/sqlite, pure Go) |
| RPC | gRPC (Go ↔ Python) |
| Inference | Ollama HTTP API (Python → localhost:11434) |

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Go Controller                            │
│                                                                 │
│  REPL Loop (cmd/controller/main.go)                             │
│    ├── Generate (first-pass) ──────────────────────┐            │
│    ├── Triple-Gated Retrieval ─────────────────┐   │            │
│    ├── Web Search Fallback (if 0 evidence) ─┐  │   │            │
│    ├── Re-Generate (with evidence) ──────────┤  │   │            │
│    ├── Store Evidence ───────────────────────┤  │   │            │
│    ├── Produce Signals ──────────────────────┤  │   │            │
│    ├── Update (learning + decay) ────────────┤  │   │            │
│    ├── Gate (hard vetoes + soft scoring) ────┤  │   │            │
│    ├── Eval (post-commit norm bounds) ───────┤  │   │            │
│    ├── Commit / Rollback ────────────────────┤  │   │            │
│    └── Log Provenance ───────────────────────┘  │   │            │
│                                                  │   │            │
│  SQLite: state_versions, provenance_log          │   │            │
│                                                  │   │            │
│                    gRPC ─────────────────────────┘   │            │
│                      │                               │            │
└──────────────────────┼───────────────────────────────┘            │
                       │                                            │
┌──────────────────────▼──────────────────────────────┐            │
│               Python Inference Service               │            │
│                                                      │            │
│  CodecServiceServicer (threaded asyncio event loop)  │            │
│    ├── Generate → Ollama /api/generate               │            │
│    ├── Embed → Ollama /api/embed                     │            │
│    ├── Search → ChromaDB cosine similarity           │            │
│    └── StoreEvidence → ChromaDB insert               │            │
│                                                      │            │
│  ChromaDB: persistent vector store                   │            │
└──────────────────────────────────────────────────────┘            │
                       │                                            │
                       ▼                                            │
               Ollama (frozen LLM)                                  │
               phi4-mini, 2.5GB                                     │
               localhost:11434                                      │
                                                                    │
         DuckDuckGo Instant Answer API ◄────────────────────────────┘
         (web search fallback, stdlib only)
```

**Two memory layers:**
- **Short-term**: Ollama's `context` token array — native dialogue continuity per session
- **Long-term**: ChromaDB evidence — gated retrieval across sessions

**Design rationale**: Go owns all decision logic (gate, update, decay, eval, rollback). Python is a stateless codec wrapper — it calls Ollama and ChromaDB but makes no decisions. The model never sees state vector norms (small models misinterpret numbers as content).

## What Was Built

### 1. State Versioning (SQLite)

**Package**: `internal/state/`

128 float32 dimensions, segmented into 4 named regions:

| Segment | Indices | Purpose |
|---------|---------|---------|
| Preferences | 0–31 | User preference encoding (tone, style) |
| Goals | 32–63 | Active objective tracking |
| Heuristics | 64–95 | Learned strategy weights |
| Risk | 96–127 | Uncertainty calibration |

Every committed update creates a new version. Rollback is a pointer swap — nothing is deleted.

**Schema**: `state_versions` (versioned snapshots), `active_state` (singleton pointer), `provenance_log` (full audit trail with GateRecord JSON).

### 2. Triple-Gated Evidence Retrieval

**Package**: `internal/retrieval/`

| Gate | Location | Check | Default |
|------|----------|-------|---------|
| Gate 1 — Confidence | Go | Entropy > threshold | **Bypassed** (AlwaysRetrieve=true) |
| Gate 2 — Similarity | ChromaDB | Cosine similarity > 0.3 | Primary filter |
| Gate 3 — Consistency | Go | Non-empty, length limit, no duplicates | Always active |

Gate 1 is bypassed by default because low-entropy recall prompts ("what did we talk about") were missing stored evidence. Gate 2 similarity is sufficient — ChromaDB search is trivial cost.

### 3. Hierarchical Gate + Rollback

**Package**: `internal/gate/`

Hard vetoes reject immediately:
- User correction, tool failure, constraint violation
- RiskFlag (entropy >= 0.75)
- Delta L2 norm > MaxDeltaNorm per segment
- State L2 norm > MaxStateNorm
- Risk segment norm > RiskSegmentCap

Soft signals are scored but don't block: entropy drop, delta stability, segment focus.

**Tentative commit workflow**: Update → Gate → Commit → Eval → Rollback if eval fails.

### 4. Signal-Driven Learning + Decay

**Package**: `internal/update/`

| Signal | Target Segment | Rationale |
|--------|---------------|-----------|
| SentimentScore | Prefs [0–31] | Tone/style preferences |
| CoherenceScore | Goals [32–63] | Coherent objective tracking |
| NoveltyScore | Heuristics [64–95] | New strategy exploration |
| Entropy | Risk [96–127] | Uncertainty calibration |

**Delta**: `delta[i] = learning_rate * signal_strength * direction[i]`, L2-clamped per segment.

**Decay**: `state[i] *= (1 - decay_rate)` per element, only on unreinforced segments. Prevents fossilization.

**Defaults**: LearningRate=0.01, DecayRate=0.005, MaxDeltaNormPerSegment=1.0.

### 5. Heuristic Signal Producers

**Package**: `internal/signals/`

- **SentimentScore**: Embed response → cosine similarity to positive anchors
- **CoherenceScore**: Embed prompt + response → cosine similarity
- **NoveltyScore**: Embed response → cosine distance to retrieved evidence average
- **RiskFlag**: `entropy >= 0.75` (hard veto trigger)
- **UserCorrection**: `/correct` REPL command sets flag for next turn

### 6. Post-Commit Eval Harness

**Package**: `internal/eval/`

Single-response validation (no Generate calls):
- State L2 norm ≤ 50.0 (blocking)
- Per-segment L2 norm ≤ 15.0 (blocking)
- Entropy vs baseline (informational)

### 7. Provenance Logging + GateRecord

**Package**: `internal/logging/`

Every decision logs a `GateRecord` containing: exact signal values, veto booleans, delta norms, active thresholds, gate action/reason/soft score. Stored as JSON in `signals_json` column. Used by replay system for deterministic reconstruction.

### 8. Deterministic Replay System

**Packages**: `internal/replay/`, `cmd/replay/`, `cmd/fixture-export/`

In-memory replay mirrors the full pipeline (update → gate → eval → rollback) without DB writes. Purely functional — same inputs produce same outputs.

**Fixtures**:
- `internal/replay/testdata/live_session.json` — 12-turn synthetic fixture
- `internal/replay/testdata/real_session.json` — 4-turn production GateRecord export (100% deterministic)

**CLIs**:
- `cmd/replay/` — `--db path` for production DB, `--fixture path` for JSON fixtures
- `cmd/fixture-export/` — `--db path --last N --out path` extracts GateRecord rows to fixture JSON

### 9. State Inspection CLI

**Package**: `cmd/inspect/`

`--db path [--last N] [--version id] [--segment name] [--json]`

Reads state store, computes per-segment L2 norms, prints tables showing version history with drift metrics.

### 10. Web Search Fallback

**Package**: `internal/websearch/`

When retrieval returns zero evidence and entropy is above threshold (0.3), queries DuckDuckGo instant answer API. Results are formatted as `[Web Search Results]` and injected as evidence for re-generation. The prompt is still stored in ChromaDB — so repeat queries hit cached evidence instead of the web.

Pure stdlib (`net/http` + `encoding/json`). No external dependencies.

### 11. Conversation Context (Multi-Turn)

Ollama's `context` token array flows end-to-end:

```
Go REPL (var ollamaCtx []int64)
  → gRPC GenerateRequest.context
    → Python → Ollama /api/generate payload["context"]
    ← Ollama response["context"]
  ← gRPC GenerateResponse.context
← result.Context → ollamaCtx (updated for next turn)
```

Session-scoped. Initialized as nil, grows with each turn.

### 12. Python Inference Service

**Package**: `py-inference/adaptive_inference/`

- `server.py` — gRPC servicer with threaded asyncio event loop (prevents concurrent RPC deadlocks)
- `service.py` — State conditioning + Ollama integration. Entropy = visible words / 400, capped at 1.0, after stripping `<think>` blocks
- `memory.py` — ChromaDB wrapper (store, search, delete)
- `ollama_client.py` — Ollama HTTP API client (generate, embed)

## Protocol Definition

```protobuf
service CodecService {
  rpc Generate(GenerateRequest) returns (GenerateResponse);
  rpc Embed(EmbedRequest) returns (EmbedResponse);
  rpc Search(SearchRequest) returns (SearchResponse);
  rpc StoreEvidence(StoreEvidenceRequest) returns (StoreEvidenceResponse);
}
```

Key fields: `repeated int64 context` on Generate request/response for Ollama conversation continuity. `repeated float state_vector` for state injection. `repeated string evidence` for retrieved evidence.

## State Evolution (Live Test Results)

5-turn conversation with phi4-mini — state vector L2 norms over time:

| Version | Total | Prefs | Goals | Heur | Risk | Notes |
|---------|-------|-------|-------|------|------|-------|
| v0 | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 | Initial zero state |
| v3 | 0.059 | 0.016 | 0.000 | 0.040 | 0.040 | First commit |
| v8 | 0.248 | 0.199 | 0.022 | 0.104 | 0.104 | Mid-session |
| v12 | 0.415 | 0.307 | 0.085 | 0.181 | 0.195 | Evidence retrieval active |
| v17 | 0.661 | 0.445 | 0.250 | 0.276 | 0.315 | Final active state |

**Observations**: Preferences grew fastest (SentimentScore fires on every positive interaction). Goals started late (CoherenceScore needs topical similarity). Decay working — unreinforced segments grow slower. Gate rejected 7 times (RiskFlag hard veto), committed 13 times.

## Test Coverage

| Package | Coverage | Notes |
|---------|----------|-------|
| `internal/eval` | 100.0% | |
| `internal/gate` | 100.0% | |
| `internal/logging` | 100.0% | |
| `internal/replay` | 100.0% | |
| `internal/retrieval` | 100.0% | |
| `internal/signals` | 100.0% | |
| `internal/update` | 100.0% | |
| `internal/state` | 95.0% | Unreachable: json.Marshal on map, sql.Open lazy connect, tx.Commit mid-transaction |
| `internal/codec` | 96.3% | Unreachable: grpc.NewClient error path |
| `internal/websearch` | — | 15 tests, all passing (HTTP mock-based) |
| `cmd/controller` | 0.0% | REPL main loop — tested via live integration |
| `cmd/replay` | 0.0% | CLI main — tested via fixture integration |
| `cmd/fixture-export` | 0.0% | CLI main — one-shot export tool |
| `cmd/inspect` | 0.0% | CLI main — inspect tool |
| `gen/adaptive` | 0.0% | Generated protobuf stubs |

**Test patterns**: `NewStoreWithDB` / `NewCodecClientWithService` for dependency injection. `corruptDB` + `seedVersion` helpers for error-path testing. `mockEmbedder` for Embed RPC mocking. `httptest.NewServer` for web search mocking.

## Key Bugs Fixed

| # | Bug | Root Cause | Fix |
|---|-----|-----------|-----|
| 1 | Embed returns 501 | qwen3:4b lacks `/api/embed` | Added `EMBED_MODEL` env var to separate embed from gen model |
| 2 | Entropy out of range | `eval_count / 100` includes thinking tokens, capped at 5.0 | Switched to visible word count / 400, capped at 1.0, strip `<think>` blocks |
| 3 | Event loop deadlock | `run_until_complete` fails on concurrent gRPC calls | Dedicated threaded event loop with `run_coroutine_threadsafe` |
| 4 | Preamble leaking | Models interpret state norms as content | Removed norms from system prompt entirely |
| 5 | Low-entropy retrieval gap | Recall prompts blocked by Gate 1 entropy check | `AlwaysRetrieve=true` bypasses Gate 1 |
| 6 | RiskFlag too aggressive | Threshold calibrated for old 0–5.0 entropy scale | Adjusted from 1.0 to 0.75 for [0,1] range |
| 7 | Evidence regurgitation | Model appended raw Q&A pairs verbatim | Numbered entries, truncation, "do not repeat" instruction |
| 8 | gRPC timeouts too tight | Hardcoded values, cold model loads exceed limits | Configurable via `TIMEOUT_GENERATE`/`TIMEOUT_SEARCH`/`TIMEOUT_STORE`/`TIMEOUT_EMBED` |

## Model Selection

| Model | Size | Gen | Embed | Thinking | Outcome |
|-------|------|-----|-------|----------|---------|
| `qwen3:4b` | 2.5GB | Yes | No (501) | Yes | Original pick. No embed, thinking inflates entropy |
| `qwen2.5-coder:7b` | 4.7GB | Yes | Yes (4096d) | No | Good quality but too slow for 30s gRPC deadlines |
| `deepseek-r1:1.5b` | 1.1GB | Yes | Yes (1536d) | Yes | Fastest but hallucinates on trivial prompts |
| **`phi4-mini`** | **2.5GB** | **Yes** | **Yes (3072d)** | **No** | **Current default.** Single model, fast, clean responses |

## Environment Variables

### Go Controller

| Variable | Default | Purpose |
|----------|---------|---------|
| `ADAPTIVE_DB` | `adaptive_state.db` | SQLite database path |
| `CODEC_ADDR` | `localhost:50051` | gRPC target (Python service) |
| `TIMEOUT_GENERATE` | `60` | Generate RPC timeout (seconds) |
| `TIMEOUT_SEARCH` | `30` | Search RPC timeout (seconds) |
| `TIMEOUT_STORE` | `15` | StoreEvidence RPC timeout (seconds) |
| `TIMEOUT_EMBED` | `15` | Embed RPC timeout (seconds) |
| `WEB_SEARCH_ENABLED` | `true` | Enable web search fallback |
| `WEB_SEARCH_MAX_RESULTS` | `3` | Max web search results |
| `WEB_SEARCH_TIMEOUT` | `10` | Web search timeout (seconds) |
| `WEB_SEARCH_ENTROPY_THRESHOLD` | `0.3` | Entropy threshold to trigger web search |

### Python Service

| Variable | Default | Purpose |
|----------|---------|---------|
| `GRPC_PORT` | `50051` | gRPC server listen port |
| `OLLAMA_MODEL` | `phi4-mini` | Generation model |
| `EMBED_MODEL` | `phi4-mini` | Embedding model (can differ from gen) |
| `OLLAMA_URL` | `http://localhost:11434` | Ollama API base URL |
| `MEMORY_PERSIST_DIR` | `./chroma_data` | ChromaDB persistence directory |

## File Inventory

### Go Controller (`go-controller/`)

```
cmd/controller/main.go          REPL entry point, full pipeline orchestration
cmd/fixture-export/main.go      CLI: GateRecord rows → JSON fixture
cmd/inspect/main.go             CLI: state inspection, norms, drift tables
cmd/replay/main.go              CLI: standalone replay harness

internal/state/types.go         StateRecord, SegmentMap, VersionWithProvenance
internal/state/store.go         SQLite CRUD, versioning, rollback, provenance joins
internal/state/store_test.go    95.0% coverage

internal/update/types.go        UpdateContext, Signals, Decision, Metrics, UpdateConfig
internal/update/update.go       Pure update(): decay + signal-driven deltas
internal/update/update_test.go  100.0% coverage

internal/gate/types.go          VetoType, GateConfig, GateDecision
internal/gate/gate.go           Hierarchical gate: hard vetoes + soft scoring
internal/gate/gate_test.go      100.0% coverage

internal/eval/types.go          EvalConfig, EvalMetric, EvalResult
internal/eval/eval.go           Post-commit validation (norm bounds)
internal/eval/eval_test.go      100.0% coverage

internal/retrieval/types.go     RetrievalConfig, EvidenceRecord, GateResult
internal/retrieval/retrieval.go Triple-gated retrieval (confidence → similarity → consistency)
internal/retrieval/retrieval_test.go  100.0% coverage

internal/signals/types.go       ProducerConfig, ProduceInput
internal/signals/producer.go    Heuristic signal producers (embed-based)
internal/signals/producer_test.go  100.0% coverage

internal/logging/types.go       ProvenanceEntry, GateRecord, GateRecordSignals/Thresholds
internal/logging/provenance.go  LogDecision → provenance_log INSERT
internal/logging/provenance_test.go  100.0% coverage

internal/codec/client.go        gRPC client (Generate, Embed, Search, StoreEvidence)
internal/codec/client_test.go   96.3% coverage

internal/replay/harness.go      In-memory replay pipeline
internal/replay/fixture.go      JSON fixture types + loader
internal/replay/harness_test.go 100.0% coverage
internal/replay/testdata/live_session.json   12-turn synthetic fixture
internal/replay/testdata/real_session.json   4-turn production fixture (100% deterministic)

internal/websearch/websearch.go      DuckDuckGo instant answer API client
internal/websearch/websearch_test.go 15 tests, HTTP mock-based

gen/adaptive/adaptive.pb.go          Generated protobuf messages
gen/adaptive/adaptive_grpc.pb.go     Generated gRPC stubs
```

### Python Service (`py-inference/`)

```
adaptive_inference/server.py         gRPC servicer (threaded asyncio loop)
adaptive_inference/service.py        InferenceService (state conditioning, entropy)
adaptive_inference/memory.py         MemoryStore (ChromaDB wrapper)
adaptive_inference/ollama_client.py  Ollama HTTP client (generate, embed)
adaptive_inference/proto/            Generated Python protobuf stubs

tests/test_service.py               Service integration tests
tests/test_memory.py                 MemoryStore tests
```

### Shared

```
proto/adaptive.proto                 gRPC service + message definitions
scripts/gen-proto.sh                 Protobuf codegen (Go + Python)
scripts/run-dev.sh                   Dev launcher (Python service + Go REPL)
adaptive_state.md                    Original blueprint document
STRUCTURE.md                         Architecture reference, conventions
```

## Build & Run

### Prerequisites

- Go 1.25+
- Python 3.11+
- Ollama running locally with `phi4-mini` pulled
- `protoc` + Go/Python gRPC plugins (for proto regeneration only)

### Quick Start

```bash
# Terminal 1: Start Python inference service
cd py-inference
pip install -e .
python -m adaptive_inference.server

# Terminal 2: Start Go controller
cd go-controller
go build ./cmd/controller/
./controller
```

Or use the dev script:

```bash
./scripts/run-dev.sh
```

### Run Tests

```bash
# All Go tests
cd go-controller && go test ./internal/...

# Python tests
cd py-inference && pytest
```

### CLI Tools

```bash
# Inspect state versions and norms
go run ./cmd/inspect/ --db adaptive_state.db --last 10

# Export GateRecord rows to fixture
go run ./cmd/fixture-export/ --db adaptive_state.db --last 4 --out fixture.json

# Replay from fixture
go run ./cmd/replay/ --fixture fixture.json

# Replay from production DB
go run ./cmd/replay/ --db adaptive_state.db
```

## Commit History

```
0d9558d Add web search fallback for zero-evidence high-entropy turns
4db47a3 Fix retrieval pollution: store prompt-only evidence
3658a11 Add inspect CLI and store provenance query methods
056c4fe Add real session fixture export tool and regression test
1d56d71 Log full gate decision record for deterministic replay
b939a15 Add replay validation fixtures and standalone replay CLI
9eb4f99 Document conversation context architecture and data flow
3a748dd Add Ollama conversation context for multi-turn continuity
49cfe93 Make Go-side gRPC timeouts configurable via env vars
2a85edc Document evidence formatting fix, remove resolved known issues
3640e01 Improve evidence injection formatting to prevent verbatim regurgitation
2be0357 Document RiskFlag threshold adjustment and retrieval gap fix
3d548b1 Adjust RiskFlag threshold from 1.0 to 0.75 for [0,1] entropy range
60b2d16 Document AlwaysRetrieve Gate 1 bypass in retrieval gating
a862dcb Always attempt retrieval by default, bypass Gate 1 entropy check
7e12c17 Document multi-turn state evolution test results
f81b17e Document full project history, decisions, and current status
c03ba08 Switch default model to phi4-mini for both gen and embed
d79774b Remove norm injection from preamble, keep evidence only
7bae8e9 Make state preamble opaque to prevent model interpretation
8a6e7d0 Document live-testing fixes: env config, model compat, event loop
9d76e1e Use visible response tokens for entropy, strip <think> blocks
5218306 Switch default generation model to qwen2.5-coder:7b
018699b Fix embed 501, entropy range, and event loop concurrency in py-inference
81b7734 Bring state test coverage from 94.2% to 95.0%
012d7a3 Bring signals test coverage to 100%
42f3489 Add heuristic signal producers
de5ca10 Bring state test coverage from 93.3% to 94.2%
6c99a28 Bring logging test coverage to 100%
45341c9 Bring codec to 96.3% and retrieval to 100% test coverage
0380117 Bring state test coverage from 87.4% to 93.3%
92e10b0 Bring update test coverage to 100%
56fc466 Bring eval test coverage to 100%
f0c173d Bring gate test coverage to 100%
8e7107b Fix replay Summarize test to cover eval_rollback branch
b65deea Phase 5: Expand replay harness to full pipeline
f227188 Phase 4: Add signal-driven state learning with per-element decay
0049b9b Phase 3: Add hierarchical gate with hard vetoes and tentative commit/rollback
6c95182 Phase 2: Add triple-gated retrieval with ChromaDB evidence store
c4fcf0c Phase 1: Adaptive Disposition Layer skeleton
```

## For Anyone Continuing This Work

### What's solid

The core pipeline is complete and verified. State versioning, gated retrieval, signal-driven learning, decay, hierarchical gate, post-commit eval, rollback, provenance logging, deterministic replay, and web search fallback all work end-to-end. Test coverage is 95–100% on every core package. The replay system can validate any configuration change against production fixtures.

### What to explore next

1. **Threshold tuning via replay**: Use `cmd/replay/` with different configs to find optimal gate/eval thresholds. The fixture system makes this cheap — no live model needed.

2. **Additional signal producers**: Tool success metrics, user satisfaction scoring, response quality heuristics. The `signals.Producer` interface is ready for extension.

3. **REST API layer**: FastAPI + Uvicorn stubs are already in py-inference dependencies. Would enable web UI or external integrations.

4. **Multi-session state**: Currently state persists across turns but each REPL session starts fresh for Ollama context. Could persist context tokens to DB for cross-session continuity.

5. **Larger models**: The architecture is model-agnostic. Swap `OLLAMA_MODEL` for any Ollama-supported model. Larger models may need threshold recalibration (entropy scale shifts with model size).

### Key constraints to respect

- **Model never sees state norms** — small models misinterpret any numbers as content. All state conditioning is Go-side only.
- **Update function is pure** — `new_state, decision, metrics = update(old_state, context, signals, evidence)`. No globals, no hidden mutation. This enables deterministic replay.
- **Gate hard vetoes are non-negotiable** — soft signals inform but don't override. User correction always triggers rejection.
- **Decay prevents fossilization** — unreinforced segments decay every turn. Remove this and state will grow unbounded.
- **Rollback is cheap** — pointer swap, nothing deleted. Design assumes rollbacks are routine, not exceptional.
