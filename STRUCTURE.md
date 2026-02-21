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

| Gate | Location | Check |
|---|---|---|
| Gate 1 — Confidence | Go (Retriever) | Entropy > threshold → proceed |
| Gate 2 — Similarity | Python (ChromaDB) | Cosine similarity > threshold |
| Gate 3 — Consistency | Go (Retriever) | Non-empty, length limit, no duplicate IDs |

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
| Safety/policy violation | `Signals.RiskFlag` |
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

## Communication

- Go ↔ Python: gRPC on port 50051 (configurable via `CODEC_ADDR` / `GRPC_PORT`)
- Python → Ollama: HTTP on port 11434 (configurable via `OLLAMA_URL`)
- Python → ChromaDB: Embedded, persisted to `MEMORY_PERSIST_DIR`
