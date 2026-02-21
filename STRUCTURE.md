# STRUCTURE.md — Adaptive State Project Layout

## Directory Tree

```
C:\adaptive_state\
├── proto/
│   └── adaptive.proto                    # gRPC service definitions (CodecService)
├── go-controller/
│   ├── go.mod / go.sum
│   ├── cmd/controller/main.go            # Entry point: store init, gRPC connect, REPL
│   ├── internal/
│   │   ├── state/
│   │   │   ├── types.go                  # StateRecord, SegmentMap, ProvenanceTag
│   │   │   ├── store.go                  # SQLite state store (CRUD, versioning)
│   │   │   └── store_test.go
│   │   ├── update/
│   │   │   ├── types.go                  # UpdateContext, Signals, Decision, Metrics
│   │   │   ├── update.go                 # Pure update() function (no-op Phase 1)
│   │   │   └── update_test.go
│   │   ├── logging/
│   │   │   ├── types.go                  # ProvenanceEntry
│   │   │   └── provenance.go             # LogDecision → provenance_log table
│   │   ├── gate/
│   │   │   ├── types.go                  # VetoType, VetoSignal, GateConfig, GateDecision
│   │   │   ├── gate.go                   # Gate: hard veto + soft scoring
│   │   │   └── gate_test.go
│   │   ├── eval/
│   │   │   ├── types.go                  # EvalConfig, EvalMetric, EvalResult
│   │   │   ├── eval.go                   # EvalHarness: post-commit validation
│   │   │   └── eval_test.go
│   │   ├── replay/
│   │   │   └── harness.go                # Replay scaffold (iterates interactions)
│   │   ├── retrieval/
│   │   │   ├── types.go                  # RetrievalConfig, EvidenceRecord, GateResult
│   │   │   ├── retrieval.go              # Retriever: triple-gated evidence retrieval
│   │   │   └── retrieval_test.go
│   │   └── codec/
│   │       ├── client.go                 # gRPC client to Python inference (Generate, Embed, Search, StoreEvidence)
│   │       └── client_test.go
│   └── gen/adaptive/                     # Generated protobuf Go stubs
├── py-inference/
│   ├── pyproject.toml
│   ├── adaptive_inference/
│   │   ├── __init__.py
│   │   ├── server.py                     # gRPC server (CodecServiceServicer + Search/StoreEvidence)
│   │   ├── service.py                    # InferenceService (state conditioning)
│   │   ├── memory.py                     # MemoryStore: ChromaDB wrapper (store, search, delete)
│   │   ├── ollama_client.py              # Ollama HTTP API (generate, embed)
│   │   └── proto/                        # Generated Python protobuf stubs
│   └── tests/
│       ├── test_service.py
│       └── test_memory.py
├── scripts/
│   ├── gen-proto.sh                      # Protobuf codegen (Go + Python)
│   └── run-dev.sh                        # Dev launcher (both services)
├── adaptive_state.md                     # Blueprint document
└── STRUCTURE.md                          # This file
```

## Region Convention

All source files use `#region` / `#endregion` markers. Regions are stable identifiers — do not rename, reorder, or restructure without explicit approval.

## SQLite Tables

| Table | Purpose |
|---|---|
| `state_versions` | Versioned state vector snapshots (128 float32s as BLOB) |
| `provenance_log` | Decision audit trail per version |
| `active_state` | Singleton pointer to current active version |

## State Vector Layout

128 float32 dimensions, segmented:

| Segment | Indices | Purpose |
|---|---|---|
| Preferences | 0–31 | User preference encoding |
| Goals | 32–63 | Active goal state |
| Heuristics | 64–95 | Learned heuristic weights |
| Risk | 96–127 | Risk profile parameters |

## Retrieval Gating (Phase 2)

Triple-gated evidence retrieval orchestrated from Go:

| Gate | Location | Check | Default |
|---|---|---|---|
| Gate 1 — Confidence | Go (Retriever) | Entropy > threshold → proceed | **Bypassed** (`AlwaysRetrieve=true`) |
| Gate 2 — Similarity | Python (ChromaDB) | Cosine similarity > threshold | Primary filter (threshold 0.3) |
| Gate 3 — Consistency | Go (Retriever) | Non-empty, length limit, no duplicate IDs | Always active |

**Gate 1 bypass**: `AlwaysRetrieve` (default `true`) skips the entropy check. Low-entropy recall prompts ("what did we talk about") were missing stored evidence because Gate 1 blocked retrieval. Gate 2 similarity is sufficient — ChromaDB search cost is trivial. Set `AlwaysRetrieve=false` to restore the original entropy-gated behavior.

Evidence flow: Go calls Generate (get entropy) → Retriever.Retrieve() → re-Generate with evidence → StoreEvidence.

ChromaDB persistence: configurable via `MEMORY_PERSIST_DIR` (default: `./chroma_data`).

## Gate + Rollback (Phase 3)

Hierarchical gate with hard vetoes decides whether to commit or reject state updates.

### Hard Veto Signals (reject immediately)
| Signal | Source |
|---|---|
| User correction | `Signals.UserCorrection` |
| Tool/verifier failure | `Signals.ToolFailure` |
| Constraint violation | `Signals.ConstraintViolation` |
| Safety/policy violation | `Signals.RiskFlag` (entropy >= 0.75) |
| Delta norm exceeded | Computed from old vs proposed state |
| Risk segment norm exceeded | Computed from proposed state risk segment |

### Soft Signals (logged, do not block)
- Entropy drop (lower entropy = better)
- Delta stability (smaller delta norm = more stable)
- Segment focus (fewer segments hit = more focused)

### Tentative Commit Workflow
1. Update() produces proposed state (no-op delta in Phase 3)
2. Gate.Evaluate() checks hard vetoes + scores soft signals
3. If rejected → log, keep old state
4. If passed → tentative commit via CommitState()
5. EvalHarness.Run() validates committed state (norm bounds, segment norms)
6. If eval fails → Rollback() to previous version
7. If eval passes → state stays committed

### Eval Checks (single-response, no Generate calls)
| Check | Blocking | Threshold |
|---|---|---|
| State L2 norm | Yes | MaxStateNorm (default 50.0) |
| Per-segment L2 norm | Yes | MaxSegmentNorm (default 15.0) |
| Entropy vs baseline | No (informational) | EntropyBaseline (default 2.0) |

## State Learning + Decay (Phase 4)

### Signal → Segment Mapping

| Signal | Target Segment | Indices | Rationale |
|---|---|---|---|
| `SentimentScore` | Prefs | 0–31 | Tone/style preferences |
| `CoherenceScore` | Goals | 32–63 | Coherent objective tracking |
| `NoveltyScore` | Heuristics | 64–95 | New strategy exploration |
| Entropy (`UpdateContext.Entropy`) | Risk | 96–127 | Uncertainty calibration |

### Delta Formula

```
delta[i] = learning_rate * signal_strength * direction[i]
```

Where `direction[i]` = sign of existing state value (+1 for zero elements). Delta is L2-clamped to `MaxDeltaNormPerSegment` per segment.

### Decay Formula

Applied per-element before delta computation:
```
state[i] *= (1 - decay_rate)
```

Decay only applies to segments NOT reinforced this turn (no mapped signal > 0). Reinforced segments are preserved.

### UpdateConfig Defaults

| Parameter | Default | Purpose |
|---|---|---|
| `LearningRate` | 0.01 | Magnitude of signal-driven deltas |
| `DecayRate` | 0.005 | Per-element multiplicative decay |
| `MaxDeltaNormPerSegment` | 1.0 | L2 clamp per segment |

### Update Flow

1. Copy old state vector
2. **Decay pass**: For each unreinforced segment, apply `state[i] *= (1 - DecayRate)`
3. **Delta pass**: For each signal > 0, compute bounded delta across target segment, clamp to `MaxDeltaNormPerSegment`
4. Compute metrics (per-segment delta/decay norms, total delta norm)
5. Decision: `"commit"` if total delta > 0, else `"no_op"`

## Full Replay Harness (Phase 5)

In-memory replay system that mirrors the main loop pipeline without DB writes.

### ReplayConfig

Bundles all three stage configs into one struct:

| Field | Type | Source |
|---|---|---|
| `UpdateConfig` | `update.UpdateConfig` | Learning rate, decay, delta clamp |
| `GateConfig` | `gate.GateConfig` | Delta/state norm caps, risk cap |
| `EvalConfig` | `eval.EvalConfig` | State/segment norm bounds |

`DefaultReplayConfig()` returns defaults from all three packages.

### Replay Pipeline (per interaction)

```
1. update.Update(current, ctx, signals, evidence, config.UpdateConfig)
2. If no_op → record, keep current state, continue
3. gate.Evaluate(current, proposed, signals, metrics, entropy)
4. If reject → record "gate_reject", keep current state, continue
5. eval.Run(proposed, entropy)
6. If !passed → record "eval_rollback", keep current state, continue
7. Passed → record "commit", advance current = proposed
```

### Result Actions

| Action | Meaning | State Effect |
|---|---|---|
| `commit` | All stages passed | Advance to proposed |
| `gate_reject` | Hard veto triggered | Keep current |
| `eval_rollback` | Norm bounds exceeded | Keep current |
| `no_op` | No state change from update | Keep current |

### ReplaySummary

`Summarize(results, finalState)` returns aggregate counts: TotalTurns, Commits, GateRejects, EvalRollbacks, NoOps, and the final StateRecord.

### Key Properties

- **In-memory only**: No DB commits, rollbacks, or provenance writes
- **No Store dependency**: Takes a `StateRecord` directly as starting state
- **No error return**: All operations are in-memory and infallible
- **Deterministic**: Same inputs produce same outputs

## Environment Configuration

| Variable | Default | Purpose |
|---|---|---|
| `OLLAMA_MODEL` | `phi4-mini` | Generation model for `/api/generate` |
| `EMBED_MODEL` | `phi4-mini` | Embedding model for `/api/embed` (separate from gen model) |
| `OLLAMA_URL` | `http://localhost:11434` | Ollama API base URL |
| `GRPC_PORT` | `50051` | Python gRPC server listen port |
| `CODEC_ADDR` | `localhost:50051` | Go controller gRPC target |
| `MEMORY_PERSIST_DIR` | `./chroma_data` | ChromaDB persistence directory |

### Model Compatibility

Not all Ollama models support `/api/embed`. The `EMBED_MODEL` env var allows using a separate model for embeddings when the generation model lacks embed support.

| Model | Size | Generate | Embed | Thinking | Notes |
|---|---|---|---|---|---|
| `phi4-mini` | 2.5GB | Yes | Yes (3072d) | No | **Current default.** Single model for full pipeline |
| `qwen2.5-coder:7b` | 4.7GB | Yes | Yes (4096d) | No | Previous default. Good quality but slower, tight on 30s gRPC deadlines |
| `qwen3:4b` | 2.5GB | Yes | No (501) | Yes | Original model. No embed support, inflated eval_count |
| `deepseek-r1:1.5b` | 1.1GB | Yes | Yes (1536d) | Yes | Fastest. Emits `<think>` blocks, hallucinates on trivial prompts |

### Thinking Model Support

The entropy estimator strips `<think>...</think>` blocks from responses before counting visible tokens. This prevents internal reasoning tokens from inflating entropy and triggering false risk flags in the gate. Thinking models are supported but not recommended as defaults due to eval_count inflation and preamble leaking.

### State Preamble Design

The system prompt does **not** inject state vector norms. Small models interpret any numbers in the system prompt as content to reason about — tested with opaque keys, `<ignore>` tags, and "do not interpret" instructions. All approaches failed with models under 4B params.

State conditioning happens through the Go-side pipeline (gate thresholds, retrieval gating, update deltas, decay). The model only receives:
- A base system instruction ("You are a helpful assistant.")
- Evidence from ChromaDB (when retrieved), formatted as numbered entries with an instruction to use as context without repeating verbatim. Each entry is truncated at 500 chars.

### Event Loop Architecture

`CodecServiceServicer` runs a dedicated asyncio event loop in a daemon thread. gRPC thread pool workers schedule coroutines via `asyncio.run_coroutine_threadsafe()`, avoiding "event loop already running" errors from concurrent RPCs.

## Communication

- Go ↔ Python: gRPC on port 50051 (configurable via `CODEC_ADDR` / `GRPC_PORT`)
- Python → Ollama: HTTP on port 11434 (configurable via `OLLAMA_URL`)
- Python → ChromaDB: Embedded, persisted to `MEMORY_PERSIST_DIR`

## Project History

### Phase 1: Skeleton
- Go controller + versioned state store (SQLite)
- Python inference service (Ollama calls) + embeddings via gRPC
- Pure `update()` function with no-op delta
- Logging + replay harness scaffold

### Phase 2: Retrieval Gating
- ChromaDB vector store for evidence
- Triple-gated retrieval (confidence → similarity → consistency)
- `StoreEvidence` and `Search` RPCs added to protobuf + servicer

### Phase 3: Gate + Rollback
- Hierarchical gate with hard vetoes + soft scoring
- Tentative commit workflow with post-commit eval harness
- Rollback to previous version on eval failure

### Phase 4: State Learning + Decay
- Signal-driven deltas mapped to state segments (sentiment→prefs, coherence→goals, novelty→heuristics, entropy→risk)
- Per-element multiplicative decay on unreinforced segments
- Bounded delta with L2 clamp per segment

### Phase 5: Replay Harness
- Full pipeline replay (update → gate → eval → rollback) without DB writes
- `ReplayConfig` bundles update/gate/eval configs
- `ReplaySummary` tracks commits, rejects, rollbacks, no-ops

### Test Coverage Push
- Brought all Go packages to 95-100% coverage
- Added heuristic signal producers (SentimentScore, CoherenceScore, NoveltyScore, RiskFlag, UserCorrection)
- Mock injection patterns: `NewStoreWithDB`, `NewCodecClientWithService`, `mockEmbedder`

### Live Integration Testing (current)
- First end-to-end test of Go REPL → gRPC → Python → Ollama pipeline
- Discovered and fixed 6 issues:
  1. **Embed 501**: `qwen3:4b` lacks `/api/embed` → added `EMBED_MODEL` env var
  2. **Entropy out of range**: `eval_count / 100` capped at 5.0 → switched to visible word count / 400 capped at 1.0, with `<think>` block stripping
  3. **Event loop concurrency**: `run_until_complete` fails on concurrent gRPC calls → dedicated threaded event loop with `run_coroutine_threadsafe`
  4. **Preamble leaking**: Models interpret state norms as content → removed norms from system prompt entirely
  5. **Low-entropy retrieval gap**: Recall prompts missed stored evidence → `AlwaysRetrieve=true` bypasses Gate 1 entropy check
  6. **RiskFlag too aggressive**: Threshold was calibrated for old 0-5.0 entropy scale → adjusted from 1.0 to 0.75 for [0,1] range

### Model Selection Decisions

| Decision | Reason |
|---|---|
| Started with `qwen3:4b` | Blueprint specified it. Fast, good quality |
| Dropped `qwen3:4b` as default | No embed support (501), thinking tokens inflate entropy |
| Tried `qwen2.5-coder:7b` | Supports gen + embed, no thinking. But 7b too slow for 30s gRPC deadlines on re-generate |
| Tried `deepseek-r1:1.5b` | Fast, supports embed. But thinking tokens + hallucinations on trivial prompts |
| Settled on `phi4-mini` | 2.5GB, supports gen + embed, no thinking tokens, fast enough for deadlines, clean responses |

### Multi-Turn State Evolution Test

5-turn conversation testing state learning across turns with `phi4-mini`:

**Prompts**: "what is machine learning" → "how does a neural network work" → "what are the main types" → "how would I build one from scratch" → "what did we talk about"

**State evolution** (total L2 norm of 128-dim vector):

| Version | Total | Prefs | Goals | Heur | Risk | Notes |
|---|---|---|---|---|---|---|
| v0 | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 | Initial zero state |
| v3 | 0.059 | 0.016 | 0.000 | 0.040 | 0.040 | First commit (earlier tests) |
| v8 | 0.248 | 0.199 | 0.022 | 0.104 | 0.104 | Mid-session |
| v12 | 0.415 | 0.307 | 0.085 | 0.181 | 0.195 | Evidence retrieval active |
| v17 | 0.661 | 0.445 | 0.250 | 0.276 | 0.315 | Final active state |

**Observations**:
- **Preferences** grew fastest (0→0.445) — SentimentScore fires on every positive interaction
- **Goals** started late — CoherenceScore only nonzero when embed similarity detects topical coherence
- **Heuristics + Risk** grew steadily from NoveltyScore and entropy signals
- **Decay working** — goals grew slower than prefs (unreinforced segments decay each turn)
- **Gate** rejected 7 times (RiskFlag hard veto on high-entropy responses), committed 13 times
- **Evidence retrieval** activated on turn 4 (entropy 0.82 > threshold 0.5), injected 5 evidence items
- **Provenance log** captured all 22 decisions with reasons

**Pipeline per turn**: Generate → Retrieve (triple-gated) → Re-generate with evidence → StoreEvidence → Signals → Update → Gate → Eval → Commit/Reject

## Current Status

**Phase**: Live integration testing complete — all 5 build phases and multi-turn validation done.

**Verified**:
- Full REPL pipeline end-to-end across multiple turns
- State vector evolves: segments grow from signal-driven deltas, decay on unreinforced segments
- All RPCs functional (Generate, Embed, Search, StoreEvidence)
- Entropy in [0,1] range, gate commits/rejects correctly
- Evidence accumulates in ChromaDB and influences re-generation
- Provenance log captures full audit trail
- Gate correctly applies hard vetoes (RiskFlag) and soft scoring

**Known Issues**:
- Go-side gRPC timeouts (30s generate, 15s search, 10s store) can be tight depending on model and prompt length
