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
│   │   ├── replay/
│   │   │   └── harness.go                # Replay scaffold (iterates interactions)
│   │   └── codec/
│   │       ├── client.go                 # gRPC client to Python inference
│   │       └── client_test.go
│   └── gen/adaptive/                     # Generated protobuf Go stubs
├── py-inference/
│   ├── pyproject.toml
│   ├── adaptive_inference/
│   │   ├── __init__.py
│   │   ├── server.py                     # gRPC server (CodecServiceServicer)
│   │   ├── service.py                    # InferenceService (state conditioning)
│   │   ├── ollama_client.py              # Ollama HTTP API (generate, embed)
│   │   └── proto/                        # Generated Python protobuf stubs
│   └── tests/
│       └── test_service.py
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

## Communication

- Go ↔ Python: gRPC on port 50051 (configurable via `CODEC_ADDR` / `GRPC_PORT`)
- Python → Ollama: HTTP on port 11434 (configurable via `OLLAMA_URL`)
