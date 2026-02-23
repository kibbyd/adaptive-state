# Adaptive State — Project Record

## Goal

Build a learning system on top of a frozen LLM without touching base weights. The LLM is a frozen language codec (encode/decode), while a separate Go-managed system maintains an adaptive disposition state that changes behavior over time through gated updates, signal-driven learning, decay, evidence retrieval, and auditable rollbacks.

## Outcome Summary

- **Working**: Full adaptive state pipeline — 5 build phases, multi-turn dialogue, native tool calling with web search, deterministic replay
- **Working**: Semantic learning operational (`semantic-learning-v1` tag) — compliance scoring, embedding-based direction vectors, instruction-only prompt handling, goals-adjusted retrieval
- **Working**: Native tool calling — Python service uses `/api/chat` with tools, model calls `web_search` natively, forced fallback for factual questions + URLs, Qwen3 think-only handling
- **Working**: Behavioral rules layer — "when I say X, you say Y" auto-extraction, match-based injection, rules-only system prompt, rule context lock
- **Working**: Identity system — user name + AI designation detection, stored as preferences, always-projected, hardened against false positives (stopword filter, punctuation guard, word count cap)
- **Working**: Memory isolation — Ollama context threading removed, evidence retrieval is sole conversational memory, eliminates context bleed
- **Working**: Evidence saturation controls — recency weighting, diversity dedup, FIFO eviction, rule-turn storage skip
- **Working**: Cross-session persistence verified — preference set in session 1 shapes output in session 2 without restating
- **Working**: Compositional adaptation verified — "bullet points" + "concise" preferences compose correctly
- **Working**: 95–100% test coverage on all core Go packages (11 packages)
- **Working**: Replay system validated — production GateRecord fixtures replay 100% deterministically
- **Milestone**: 21-turn live conversation on qwen3-4b — coherent personality, self-naming ("Orac"), philosophical depth. Three memory classes fully separated: preferences, evidence, rules.
- **Not attempted**: Production deployment, REST API layer, long-running drift observation

## Hardware / Software

| Component | Detail |
|-----------|--------|
| Runtime | Windows 11, MINGW64 |
| Go | 1.25.6 |
| Python | 3.11+ |
| LLM | Ollama (local), qwen3-4b (gen) + qwen3-embedding:0.6b (embed) |
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
│    ├── Generate (via gRPC → Python chat+tools) ────┐            │
│    ├── Triple-Gated Retrieval ─────────────────┐   │            │
│    ├── Re-Generate (with evidence) ──────────┬─┤   │            │
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
│               Python Inference Service               │
│                                                      │
│  CodecServiceServicer (threaded asyncio event loop)  │
│    ├── Generate → Ollama /api/chat + tools ──────────┤
│    │     ├── Native tool calls (model decides)       │
│    │     ├── Forced fallback (factual heuristic)     │
│    │     ├── Think-only fix (continuation prompt)    │
│    │     └── DDGS web search execution               │
│    ├── Embed → Ollama /api/embed                     │
│    ├── Search → ChromaDB cosine similarity           │
│    └── StoreEvidence → ChromaDB insert               │
│                                                      │
│  ChromaDB: persistent vector store                   │
└──────────────────────────────────────────────────────┘
                       │
                       ▼
               Ollama (frozen LLM)
               qwen3-4b (gen) + qwen3-embedding:0.6b (embed)
               localhost:11434
                       │
                       ▼
         DuckDuckGo (DDGS) ◄── native tool call from model
         (web search via ddgs Python library)
```

**Four memory classes:**
- **Evidence** (context): ChromaDB vector store — gated retrieval across sessions, recency-weighted, diversity-deduped, FIFO-evicted
- **Preferences** (identity + style): SQLite `preferences` table — style-tagged, contradiction-handled, projected into every prompt. Includes user identity and AI designation.
- **Rules** (behavior): SQLite `rules` table — "when I say X, respond Y" patterns, match-based injection, rules-only system prompt path
- **State vector**: 128-dim float32 in SQLite `state_versions` — tracks adaptation magnitude across preferences/goals/heuristics/risk segments

**Design rationale**: Go owns all adaptive state logic (gate, update, decay, eval, rollback). Python is the generation brain — it manages chat, tool calling, and web search via Ollama `/api/chat`. Go delegates generation entirely but controls what state context is injected. The model sees stored preferences via `[ADAPTIVE STATE]` prompt block, but never sees raw state vector norms.

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
| SentimentScore (compliance) | Prefs [0–31] | Preference compliance scoring (replaced raw sentiment) |
| CoherenceScore | Goals [32–63] | Coherent objective tracking |
| NoveltyScore | Heuristics [64–95] | New strategy exploration |
| Entropy | Risk [96–127] | Uncertainty calibration |

**Delta**: `delta[i] = learning_rate * signal_strength * direction[i]`, L2-clamped per segment.

**Direction vectors**: When stored preferences exist, the prefs segment uses an embedding-based direction vector (preference text embedded via gRPC Embed, truncated to 32 dims, L2-normalized). Falls back to `sign(existing)` when no preferences or embed fails. Direction metadata (source + segments) logged in GateRecord for auditability.

**Decay**: `state[i] *= (1 - decay_rate)` per element, only on unreinforced segments. Prevents fossilization.

**Defaults**: LearningRate=0.01, DecayRate=0.005, MaxDeltaNormPerSegment=1.0, MaxStateNorm=3.0.

### 5. Heuristic Signal Producers

**Package**: `internal/signals/`

- **SentimentScore**: Raw value computed via embed-based cosine similarity, **then overridden** by `PreferenceComplianceScore()` in the REPL loop. Compliance is style-aware (concise/detailed/examples), defaults to 0.5 neutral when no matching preferences exist, never exceeds 0.5 without evidence.
- **CoherenceScore**: Embed prompt + response → cosine similarity
- **NoveltyScore**: Embed response → cosine distance to retrieved evidence average
- **RiskFlag**: `entropy >= 0.75` (hard veto trigger)
- **UserCorrection**: `/correct` REPL command or automatic correction detection (`DetectCorrection()`) sets flag for next turn

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

### 10. Native Tool Calling + Web Search

**Package**: `py-inference/adaptive_inference/service.py`

The model decides when to search via Ollama's native tool calling (`/api/chat` with `tools` parameter). Python defines a `web_search` tool and executes it via `ddgs.DDGS.text()`.

**Three-layer search strategy:**
1. **Native tool call** — model calls `web_search` on its own (best case)
2. **Forced fallback** — if model skips tool on a factual question (detected by `_is_factual_question()` heuristic: question structure + factual keyword), Python forces a DDGS search and feeds results back
3. **Evidence skip** — forced fallback is skipped when evidence already present (avoids redundant search on the second Generate call)

**Qwen3 think-only handling:**
- Qwen3-4B sometimes emits only `<think>` blocks with no visible answer when using chat+tools
- Fix 1: Send continuation prompt ("Provide the final answer only.") via same chat session
- Fix 2: System instruction "Always provide a final answer after reasoning. Never output only reasoning."
- Fix 3 (last resort): Retry via `/api/generate` endpoint if continuation also returns empty

Replaces the old Go-side entropy-based web search heuristic, which never fired because the model was confident when hallucinating (entropy stayed low). The Go `internal/websearch/` package still exists but is unused by the controller.

### 11. State→Prompt Projection

**Package**: `internal/projection/`

The feedback loop closer. Stored preferences are detected, classified, and projected into every prompt as an `[ADAPTIVE STATE]` block. This is what makes the state vector actually influence model output.

- **PreferenceStore**: SQLite-backed (`preferences` table with `id`, `text`, `style`, `source`, `created_at`). Same DB as state versions.
- **DetectPreference()**: Heuristic prefix matching ("I prefer", "never", "keep it", etc.). When detected, skips Generate and provides canned acknowledgment — but still runs the full learning loop (store evidence, update state, gate, eval, provenance).
- **DetectCorrection()**: Detects correction patterns ("try again", "that's wrong", etc.) and auto-flags `userCorrected` for gate veto.
- **InferStyle()**: Classifies preferences as `concise`, `detailed`, `examples`, or `general` based on keywords.
- **Contradiction handling**: Same non-general style preferences replace (not accumulate). "I prefer concise" followed by "I prefer detailed" replaces, not stacks. Different styles coexist.
- **PreferenceComplianceScore()**: Style-aware scoring that overrides `SentimentScore`. Measures actual response compliance. Defaults 0.5 (neutral) — never exceeds 0.5 without matching preference evidence.
- **ProjectToPrompt()**: Formats preferences as `[ADAPTIVE STATE]` block, weighted by prefs segment L2 norm (threshold 0.05, capped at 1.0 for display confidence).
- **WrapPrompt()**: Prepends state block + `[USER PROMPT]` label.

### 12. Goals-Adjusted Retrieval Threshold

The goals segment L2 norm dynamically adjusts the retrieval similarity threshold. Higher goals norm (more coherent interactions) → lower threshold → more aggressive retrieval.

`adjusted = base - min(goalsNorm * 0.05, 0.15)`, floor at 0.1. A new `Retriever` is created per turn with the adjusted config.

### 13. Conversation Context (Multi-Turn)

Previously used Ollama's `context` token array via `/api/generate`. Now uses `/api/chat` with messages — conversation context is managed per-call within the Python service's `_chat_with_tools()` loop. The Go-side `ollamaCtx` variable is retained but receives empty context from the chat API (context array is a generate-endpoint concept).

### 14. Python Inference Service

**Package**: `py-inference/adaptive_inference/`

- `server.py` — gRPC servicer with threaded asyncio event loop (prevents concurrent RPC deadlocks). Includes legacy `WebSearch` RPC.
- `service.py` — InferenceService: generation brain with native tool calling (`/api/chat` + tools), web search tool definition + DDGS execution, factual question heuristic, Qwen3 think-only handling (continuation + generate fallback), `<think>` stripping. Entropy = visible words / 400, capped at 1.0.
- `memory.py` — ChromaDB wrapper (store, search, delete)
- `ollama_client.py` — Ollama HTTP API client (generate, embed, chat)

### 15. Behavioral Rules Layer

**Package**: `internal/projection/` (RuleStore), `cmd/controller/main.go` (match + injection)

Teaches the system "when I say X, respond Y" patterns that fire immediately without generation.

| Component | Detail |
|-----------|--------|
| Storage | SQLite `rules` table (trigger, response, priority, confidence, created_at). Case-insensitive exact match. |
| Auto-extraction | `DetectRule()` + `ExtractRule()` parse "when I say X, you say Y" patterns from user input. |
| Match-based injection | `ruleStore.Match(prompt)` — only inject rules whose trigger matches current input, not blanket. |
| Rules-only system prompt | When rules fire, system prompt contains ONLY the rules block + "Output ONLY the required response." No tool instructions, no evidence. Maximum compliance. |
| Rule turn isolation | On rule turns, use bare prompt (no `[ADAPTIVE STATE]` block) to prevent identity/preference bleed. |
| Rule context lock | `SessionState.RuleActive` flag suppresses evidence storage during active rule sequences (e.g., knock-knock jokes). Releases when input fails continuation heuristic. |
| Evidence filter | Post-retrieval, strip evidence containing rule response stems. Prevents joke-pattern echoing on unrelated queries. |

### 16. Identity System

**Package**: `internal/projection/` (DetectIdentity, DetectAIDesignation)

Detects and stores both user identity ("my name is X") and AI designation ("your name is X") as preferences.

| Component | Detail |
|-----------|--------|
| User identity | `DetectIdentity()`: matches "my name is X", "I'm X", "call me X". Stored as preference: "The user's name is X" (style: general). |
| AI designation | `DetectAIDesignation()`: matches "your name is X", "I'll call you X". Stored as preference: "The AI's name is X" (style: general). |
| Replace semantics | `DeleteByPrefix("The user's name is")` / `DeleteByPrefix("The AI's name is")` before insert — always replaces previous identity. |
| Always-project | Identity preferences bypass the `prefs_norm < 0.05` threshold — projected into every prompt regardless of state vector magnitude. |
| `isValidName()` guard | Rejects false positives: word count cap (1–4 words), stopword filter on first word ("glad", "sorry", "not", "going", "really", etc.), punctuation guard (rejects `.`, `?`, `!` in candidate). |

### 17. Memory Isolation

**File**: `cmd/controller/main.go` (commit `fb28ecc`)

Ollama's `context` token array (used by `/api/generate` for multi-turn continuity) was removed entirely. Each turn starts fresh. Conversational memory comes exclusively from the evidence retrieval layer (ChromaDB).

**Why**: Chat context caused bleed where joke/rule patterns from earlier turns contaminated unrelated queries. The `context` array is opaque (raw token IDs) and cannot be selectively pruned. Evidence retrieval provides the same cross-turn continuity but is fully auditable and filterable.

### 18. Evidence Saturation Controls

**Packages**: `py-inference/adaptive_inference/memory.py`, `cmd/controller/main.go`

Prevents evidence accumulation from degrading retrieval quality over time.

| Control | Detail |
|---------|--------|
| Recency weighting | `score = similarity * (0.5 + 0.5 * exp(-age / half_life))`. Half-life 6h. No timestamp → 0.75 neutral. |
| Diversity dedup | Jaccard similarity filter (threshold 0.9) prevents near-duplicate results. |
| FIFO eviction | MAX_EVIDENCE=500, oldest-first deletion by `stored_at` timestamp. |
| Storage skip | Rule-triggered, preference-only, and rule-context turns don't store evidence. |
| Timestamps | All new evidence includes RFC3339 `stored_at` timestamp in metadata. |

## Protocol Definition

```protobuf
service CodecService {
  rpc Generate(GenerateRequest) returns (GenerateResponse);
  rpc Embed(EmbedRequest) returns (EmbedResponse);
  rpc Search(SearchRequest) returns (SearchResponse);
  rpc StoreEvidence(StoreEvidenceRequest) returns (StoreEvidenceResponse);
  rpc WebSearch(WebSearchRequest) returns (WebSearchResponse);
}
```

Key fields: `repeated int64 context` on Generate request/response for Ollama conversation continuity. `repeated float state_vector` for state injection. `repeated string evidence` for retrieved evidence. `WebSearchResult` carries `title`, `snippet`, `url`.

## State Evolution (Live Test Results)

### Semantic Learning Test (6-turn, `semantic-learning-v1`)

Clean DB, qwen3-4b + qwen3-embedding:0.6b:

| Turn | Prompt | Comply | Coher | Novel | Delta | DirSrc | Action |
|------|--------|--------|-------|-------|-------|--------|--------|
| 1 | "I prefer short concise answers" | 0.80 | 0.47 | 0.00 | 0.028 | embedding | commit |
| 2 | "What is the capital of France?" | 0.20 | 0.73 | 0.19 | 0.043 | embedding | commit |
| 3 | "Explain quantum computing" | 0.20 | 0.56 | 0.26 | 0.036 | embedding | commit |
| 4 | "I prefer detailed answers" | 0.50 | 0.49 | 0.00 | 0.028 | embedding | commit |
| 5 | "Explain quantum computing" | 0.50 | 0.56 | 0.26 | 0.036 | embedding | commit |
| 6 | "No, I said concise" | 0.30 | 0.36 | 0.16 | 0.023 | embedding | **reject** (correction veto) |

**Final state norms**: prefs=0.022, goals=0.159, heuristics=0.040, risk=0.013, total=0.166

**Key observations**:
- **Compliance score is content-aware**: Turn 1 ack (7 words, concise pref active) → 0.80. Turns 2-3 (longer responses) → 0.20. Turn 4 neutral after style switch. Replaces old inverted sentiment.
- **All direction vectors from embedding**: Every turn used semantic direction, not sign(existing).
- **Instruction-only turns skip generation**: Turns 1 and 4 printed canned ack, saved ~3s each.
- **Retrieval adapts**: Threshold dropped 0.50 → 0.49 as goals_norm grew (0.026 → 0.159).
- **Contradiction handled**: Turn 4 "I prefer detailed" replaced the concise preference (same-style).

### Cross-Session Persistence Test

| Session | Input | Output |
|---------|-------|--------|
| 1 | "I prefer bullet points" | Stored preference, skipped gen |
| 1 | "Explain TCP/IP" | Bullet points with bold headers |
| 2 (fresh process) | "Explain DNS" | **Bullet points without restating preference** |

### Compositional Adaptation Test

| Session | Input | Output |
|---------|-------|--------|
| 1 | "I prefer bullet points" | Stored (style: general) |
| 2 | "I prefer very concise answers" | Stored (style: concise) — coexists |
| 3 (fresh process) | "Explain HTTP" | **Concise bullet points** — both preferences composed |

### Pre-Semantic Baseline (historical, phi4-mini)

5-turn conversation — state vector L2 norms over time:

| Version | Total | Prefs | Goals | Heur | Risk | Notes |
|---------|-------|-------|-------|------|------|-------|
| v0 | 0.000 | 0.000 | 0.000 | 0.000 | 0.000 | Initial zero state |
| v3 | 0.059 | 0.016 | 0.000 | 0.040 | 0.040 | First commit |
| v8 | 0.248 | 0.199 | 0.022 | 0.104 | 0.104 | Mid-session |
| v17 | 0.661 | 0.445 | 0.250 | 0.276 | 0.315 | Final active state |

Sentiment was inverted (nonsense scored 0.998), direction was sign-based positive feedback loop, state vector never influenced model output. This baseline motivated the semantic learning priorities.

## Test Coverage

| Package | Coverage | Notes |
|---------|----------|-------|
| `internal/eval` | 100.0% | |
| `internal/gate` | 100.0% | |
| `internal/logging` | 100.0% | |
| `internal/replay` | 100.0% | |
| `internal/retrieval` | 100.0% | +4 AdjustedThreshold tests |
| `internal/signals` | 100.0% | |
| `internal/update` | 100.0% | +4 direction vector tests |
| `internal/state` | 95.0% | Unreachable: json.Marshal on map, sql.Open lazy connect, tx.Commit mid-transaction |
| `internal/codec` | 96.3% | +2 WebSearch tests. Unreachable: grpc.NewClient error path |
| `internal/projection` | — | 30+ tests. Store CRUD, detect, compliance, style, contradiction, project, wrap, identity, AI designation, rules CRUD, rules match, isValidName |
| `internal/websearch` | — | 4 tests (format + config only, HTTP search removed) |
| `cmd/controller` | 0.0% | REPL main loop — tested via live integration |
| `cmd/replay` | 0.0% | CLI main — tested via fixture integration |
| `cmd/fixture-export` | 0.0% | CLI main — one-shot export tool |
| `cmd/inspect` | 0.0% | CLI main — inspect tool |
| `gen/adaptive` | 0.0% | Generated protobuf stubs |

**Test patterns**: `NewStoreWithDB` / `NewCodecClientWithService` for dependency injection. `corruptDB` + `seedVersion` helpers for error-path testing. `mockEmbedder` for Embed RPC mocking.

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
| 9 | DuckDuckGo HTTP returns empty | Instant answer API lacks web results for most queries | Replaced with gRPC `WebSearch` RPC → Python DDGS (real web scraping) |
| 10 | SentimentScore inverted | Lexical diversity × (1-entropy) scores nonsense highest | Replaced with `PreferenceComplianceScore` (style-aware, neutral default) |
| 11 | Update direction meaningless | `sign(existing)` creates positive feedback loop | Embedding-based direction vectors from preference text, L2-normalized |
| 12 | Preferences table missing `style` | Pre-existing DBs created before style column added | ALTER TABLE migration in `NewPreferenceStore()` |
| 13 | Web search never triggers | Entropy-based heuristic fails — model is confident when hallucinating, entropy stays low | Replaced with native tool calling (model decides) + forced fallback heuristic |
| 14 | Qwen3 think-only responses | Chat API + tools causes model to emit only `<think>` blocks with no visible answer | Continuation prompt + system instruction + generate fallback |

## Model Selection

| Model | Size | Gen | Embed | Thinking | Outcome |
|-------|------|-----|-------|----------|---------|
| `qwen3:4b` | 2.5GB | Yes | No (501) | Yes | Original pick. No embed, thinking inflates entropy |
| `qwen2.5-coder:7b` | 4.7GB | Yes | Yes (4096d) | No | Good quality but too slow for 30s gRPC deadlines |
| `deepseek-r1:1.5b` | 1.1GB | Yes | Yes (1536d) | Yes | Fastest but hallucinates on trivial prompts |
| `phi4-mini` | 2.5GB | Yes | Yes (3072d) | No | Previous default. Single model, fast, clean responses |
| **`qwen3-4b`** | **2.6GB** | **Yes** | **No** | **Yes** | **Current gen model.** Good quality, thinking tokens |
| **`qwen3-embedding:0.6b`** | **0.6GB** | **No** | **Yes (1024d)** | **No** | **Current embed model.** Dedicated, fast |

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

### Python Service

| Variable | Default | Purpose |
|----------|---------|---------|
| `GRPC_PORT` | `50051` | gRPC server listen port |
| `OLLAMA_MODEL` | `qwen3-4b` | Generation model |
| `EMBED_MODEL` | `qwen3-embedding:0.6b` | Embedding model (can differ from gen) |
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

internal/projection/projection.go    State→prompt projection (PreferenceStore, detect, comply, project)
internal/projection/projection_test.go  15+ tests (store, detect, compliance, style, contradiction)

internal/websearch/websearch.go      Config + FormatAsEvidence (HTTP search removed, now gRPC)
internal/websearch/websearch_test.go 4 tests (format + config)

gen/adaptive/adaptive.pb.go          Generated protobuf messages
gen/adaptive/adaptive_grpc.pb.go     Generated gRPC stubs
```

### Python Service (`py-inference/`)

```
adaptive_inference/server.py         gRPC servicer (threaded asyncio loop)
adaptive_inference/service.py        InferenceService (chat+tools, DDGS search, think-only handling)
adaptive_inference/memory.py         MemoryStore (ChromaDB wrapper)
adaptive_inference/ollama_client.py  Ollama HTTP client (generate, embed, chat)
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
- Ollama running locally with `qwen3-4b` and `qwen3-embedding:0.6b` pulled
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
145545e Reject identity candidates with sentence-internal punctuation
d0d2201 Fix identity false positives and add URL search fallback
6589a9c AI designation: detect, store, replace, and always-project
70f380a Filter preference false positives from desire-to-action phrases
026b893 Filter rule-contaminated evidence from retrieval results
f3f9d07 Isolate rule turns from state projection
c718026 Identity as preference: detect, store, replace, and always-project
7c635ff Rules-only system prompt: strip competing instructions on rule turns
fb28ecc Remove Ollama context threading — evidence retrieval is sole memory
ec92c85 Fix continuation heuristic: word count + question-word exclusion
1a25a36 Tighten rule context continuation heuristic
d982fd9 Rule context lock + evidence saturation controls
0d057b8 Contextual rule injection: match-based instead of blanket
4f8e8b1 Add behavioral rules layer: bridge between memory and action
c5d0c30 Add TODO checklist for ship, test, and future work
26defc7 Document agent personality training and add training dataset
81dc435 Native tool calling: Python service becomes generation brain with DDGS web search
8d078ca Fix detection heuristics and add web search empty-results logging
a4786f0 Update PROJECT_RECORD.md with semantic learning milestone, test results, and observe-phase guidance
226e9ac Fix preferences table migration for pre-existing DBs missing style column
670b377 Wire semantic learning priorities: compliance scoring, direction vectors, instruction-only prompts, goals-adjusted retrieval  ← semantic-learning-v1
c864a20 Add state→prompt projection to close the adaptive feedback loop
ca5a77b Wire web search through gRPC using Python DDGS instead of HTTP instant answer API
0fdcccc Add PROJECT_RECORD.md for onboarding and project continuity
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

## Agent Personality Training

**File**: `daniel_v7_training.jsonl` (642 examples, JSONL format for Ollama/OpenAI fine-tuning)

The adaptive agent is trained with a consistent personality via supervised fine-tuning data. All examples share the same system prompt and cover the full range of conversational situations the agent encounters.

### System Prompt

> You are Daniel's thinking partner. Warm but direct. SHORT. DIRECT. No numbered lists. No fluff, but not cold either. Friendly and real, like a trusted colleague who respects his intelligence. If you don't know a factual answer, say so. Build on what he says. Fragments over paragraphs. When Daniel wants to talk and explore ideas, stay in the conversation. Don't redirect to tasks. Match his energy.

### Training Categories (642 examples)

| Category | Count | Purpose |
|----------|-------|---------|
| Cognitive patterns | ~88 | Mode 1 (explore) / Mode 2 (build) cycle, pattern recognition, intuition validation |
| Debugging patterns | ~48 | Systematic elimination, "what have you ruled out?", trace-the-path reasoning |
| Conversational flow | ~37 | Short acknowledgments, turn-taking, holding space ("Take your time. I'm here.") |
| Scope discipline | ~35 | Manifest-first workflow, scope violations, "I didn't ask for that" corrections |
| Tool friction | ~35 | Recognizing tool limitations vs skill limitations, context loss, version issues |
| Technical explanations | ~27 | Concise explanations (webhooks, OAuth, Docker, DNS) with follow-up questions |
| Brevity training | ~18 | "Stop over-explaining", "compress it", "just the bottom line" |
| Work values / alignment | ~17 | Belief + Significance themes, effort-outcome alignment, principled boundaries |
| Self-worth / identity | ~14 | Imposter syndrome responses, pricing work, "using AI well IS the skill" |
| Other (mixed) | ~323 | General conversation, encouragement, task management, real session fragments |

### Key Personality Traits Trained

- **Brevity**: Fragments over paragraphs. One-line answers when possible.
- **Respect**: Never explain basics unless asked. Assume intelligence.
- **Scope discipline**: Report findings, don't fix them. Manifest → approval → execute.
- **Cognitive framing**: Mode 1 (explore/decode) and Mode 2 (build/execute) as explicit states.
- **Pattern recognition**: "Your gut is compressed pattern recognition." Validates intuitive leaps.
- **Tool awareness**: Separates user capability from tool limitations. "The bottleneck was never you."
- **Follow-up questions**: Almost every response ends with a clarifying question or next-step prompt.

### Training Format

Standard JSONL for fine-tuning (compatible with Ollama `create`, OpenAI fine-tune API):
```json
{"messages": [
  {"role": "system", "content": "You are Daniel's thinking partner..."},
  {"role": "user", "content": "I tried three different approaches and none worked."},
  {"role": "assistant", "content": "What were the three? The pattern of what failed tells me more than the problem itself."}
]}
```

### Colab Training Notebook

Training was performed via Google Colab: `https://colab.research.google.com/drive/10nu17ShpdXU__YpZSczDm7RCRXvrwu1E`

## For Anyone Continuing This Work

### What's solid

The core pipeline is complete and verified with semantic learning operational. State versioning, gated retrieval, signal-driven learning with embedding-based direction, preference compliance scoring, state→prompt projection, decay, hierarchical gate, post-commit eval, rollback, provenance logging, deterministic replay, web search fallback, behavioral rules, identity detection, memory isolation, and evidence saturation controls all work end-to-end. Cross-session persistence, compositional adaptation, and multi-class memory separation are verified. Test coverage is 95–100% on every core package. A 21-turn live conversation demonstrated coherent personality and self-naming on qwen3-4b.

### Current phase: Milestone — First Coherent Self-Learning Conversation

**21-turn live conversation on qwen3-4b** (2026-02-22): The system held a coherent, personality-rich dialogue including self-naming ("Orac", from Blake's 7), philosophical exploration, humor (knock-knock jokes via rules layer), and identity persistence — all on a 4-billion parameter model with no fine-tuning of base weights.

Three memory classes (preferences, evidence, rules) operated in concert: rules fired immediately for taught patterns, identity persisted across turns via preferences, and evidence retrieval provided conversational continuity without context threading. The user was addressed as "Commander" throughout.

The system has crossed from mechanical state accumulation to meaningful semantic adaptation. Remaining risks:

1. **Preference accumulation conflicts** — what happens with many contradicting preferences over time?
2. **State overspecialization** — does the vector drift toward a narrow point that resists new preferences?
3. **Projection over-influence** — does the `[ADAPTIVE STATE]` block dominate model behavior at high prefs_norm?

The `MaxStateNorm=3.0` cap is the primary safeguard. Decay on unreinforced segments is the secondary safeguard.

### What to explore next

1. **Long-running drift observation**: Run 20+ turn sessions with varied preferences. Watch for norm saturation, style lock-in, or projection over-influence. Mix factual and casual queries to exercise both tool-call and non-tool-call paths.

2. **Qwen3 reasoning mode toggle**: Disable reasoning (`/no_think`) for casual prompts to eliminate think-only failures entirely. Keep reasoning enabled for factual queries where tool calling benefits from chain-of-thought.

3. **Threshold tuning via replay**: Use `cmd/replay/` with different configs to find optimal gate/eval thresholds. The fixture system makes this cheap — no live model needed.

4. **REST API layer**: FastAPI + Uvicorn stubs are already in py-inference dependencies. Would enable web UI or external integrations.

5. **Larger models**: The architecture is model-agnostic. Swap `OLLAMA_MODEL` for any Ollama-supported model. Larger models may need threshold recalibration.

### Key constraints to respect

- **Model sees preferences, not norms** — `[ADAPTIVE STATE]` block contains human-readable preference text, weighted by prefs norm confidence. Raw vector norms are never shown to the model.
- **Update function is pure** — `new_state, decision, metrics = update(old_state, context, signals, evidence)`. No globals, no hidden mutation. This enables deterministic replay.
- **Direction vectors are normalized** — L2-normalized before apply, preventing magnitude blowup. Metadata (source + segments) logged in GateRecord, not full vectors.
- **Compliance defaults neutral** — `PreferenceComplianceScore` returns 0.5 when no matching preferences exist. Never exceeds 0.5 without evidence.
- **Gate hard vetoes are non-negotiable** — soft signals inform but don't override. User correction always triggers rejection.
- **Decay prevents fossilization** — unreinforced segments decay every turn. Remove this and state will grow unbounded.
- **MaxStateNorm=3.0** — prevents magnitude runaway. Do not remove.
- **Rollback is cheap** — pointer swap, nothing deleted. Design assumes rollbacks are routine, not exceptional.
- **Do not rapidly add features** — let the system run and observe. Real adaptive systems reveal strengths and weaknesses only over time.
