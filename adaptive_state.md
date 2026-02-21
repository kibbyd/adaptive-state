Blueprint: Adaptive Disposition Layer on Top of a Frozen LLM Codec
Goal

Build a learning system without touching base weights: the LLM is a frozen language codec (encode/decode), while a separate system maintains an adaptive disposition state that changes behavior over time through gated updates, decay, and auditable rollbacks.

1) Components
A) Frozen LLM (Codec)

Role: Convert tokens ⇄ internal representations; generate logits given (prompt + injected state + retrieved evidence).

Immutable: Same model version, same weights.

Runtime: Ollama Qwen3-4B (your current setup) or any frozen model.

B) Disposition State (Persistent, Compact)

Type: Fixed-size float vector, e.g. 128–512 floats.

Segmented (named): For interpretability + targeted drift detection.

Example segments:

prefs (tone/style, “peer mode”)

goals (current objective priors)

heuristics (working strategies)

risk / uncertainty calibration

Stored in: Go-managed persistence (SQLite + snapshots).

C) Memory Store (Experience/Evidence)

Vector DB: embeddings + metadata, plus optional SQLite for structured records.

Role: Long-term experience storage.

Important: Memory is noisy and large; only influences behavior when retrieval-gated.

D) Gate (Decision Logic for Updates)

Hierarchical gate with hard vetoes (not weighted averaging).

Hard veto signals (reject immediately):

user correction contradicts update

tool/verifier failure

detected contradiction with constraints

safety/policy violation (if applicable)

Soft signals (rank among survivors):

self-consistency (agreement across samples)

stability (no regressions across small eval batch)

usefulness (task success proxies)

low uncertainty after update (entropy drop)

E) Evaluator / Verifiers (Signals)

Plug-in system for “success” signals:

tool execution success (code runs, API returns expected)

consistency checks

user feedback/corrections

lightweight local checks (regex, schema validation, etc.)

Truth is variable: verifier suite should be task-aligned, not universal truth.

F) Update Rule (Writes to State)

Produces bounded deltas to state segments.

Must be rate-limited, normalized, and auditable.

The update is committed only if gate accepts.

G) Decay (Prevents Fossilization)

Context-weighted multiplicative decay per update-step (not time).

State fades unless reinforced in relevant contexts.

Prevents “scar tissue” and stale heuristics.

H) Provenance Logging (Auditability)

Every state element update should log:

what context triggered it

which signals supported it

which memory evidence contributed

decision outcome (commit/reject)

parent version id

metrics snapshot

I) Versioned Snapshots + Rollback

Every committed update creates a new state version.

Rollback is pointer swap to last-known-good version.

Nothing deleted; rejected states marked.

J) Orchestration Layer (Go) + ML Layer (Python)

Go: controller, gate, persistence, rollback, replay harness orchestration.

Python: model calls, embeddings, optional scoring utilities.

Interface: gRPC/HTTP with simple payloads.

2) Inference Flow (Runtime)
Step 0: Inputs

user prompt

current state version id

optional session context

Step 1: Uncertainty / Pattern-mismatch detection

compute cheap uncertainty proxy:

logit entropy

disagreement across N samples

“I don’t have a pattern” heuristic via low confidence

Step 2: Memory Retrieval (Triple-Gated)

Only retrieve evidence if:

low confidence OR explicit “unknown”

high similarity to stored experiences (embedding threshold)

quick consistency check passes (schema/constraint sanity)
Then inject retrieved evidence into prompt.

Step 3: Codec generation

LLM generates response using: prompt + retrieved evidence + injected state conditioning.

Step 4: Produce signals

run verifiers/tools

collect self-consistency stats if needed (N candidates)

capture user correction if present

Step 5: Update proposal (optional)

proposer outputs bounded delta for state segments (or “no update”)

delta proposal is evaluated by gate

Step 6: Gate decision

apply hard vetoes

if survives, run short eval harness batch (seconds) before promotion

commit or reject

Step 7: Commit / Rollback

if commit: store new state snapshot + provenance + metrics

if regression detected post-commit: rollback to prior good snapshot

3) State Update Function (Pure, Replayable)

Design requirement:
new_state, decision, metrics = update(old_state, context, signals, retrieved_evidence)

No globals. No hidden mutation.
This enables:

deterministic replay

offline tuning

easy rollback

4) Decay Spec (Context-Weighted)

Multiplicative decay per update step:

state[i] *= (1 - decay_rate * context_mismatch)

context_mismatch is high when:

state element is repeatedly unhelpful in similar contexts

contradicted by user correction / verifier failures

reinforced when:

update contributes to verified success in similar contexts

Starting values:

state size: 128–512

decay_rate: 0.001–0.01

delta magnitude clamp: small (e.g. max L2 norm per segment)

5) Rollback Policy
When to rollback (fast)

immediate verifier/tool failure increase vs baseline

increased user corrections

decreased consistency or score on short eval batch

state norm spikes outside bounds

How quickly

commit tentatively

run eval batch within seconds

confirm or revert

6) Observability (Minimal Sufficient Metrics)

Track over time:

state norm (overall and per segment)

update magnitude (sparsity + L2 size per update)

verifier score (pass/fail rate or scalar objective)

Healthy system:

bounded norms

sparse bounded updates

improving or stable verifier scores

7) Data Structures (Recommended)
State record

version_id

parent_version_id

state_vector (float32 array)

segment_map (offsets)

created_at

metrics_snapshot

provenance_tags

Provenance tags (examples)

context_hash

trigger_type: uncertainty / user correction / tool fail / success reinforce

signals: list + values

evidence_refs: memory ids used

decision: commit/reject

reason: veto type or promotion rationale

8) Implementation Phases
Phase 1: Skeleton

Go controller + versioned state store (SQLite)

Python inference service (Ollama calls) + embeddings

Pure update() function with no-op delta

Logging + replay harness scaffold

Phase 2: Retrieval gating

vector DB

triple-gated retrieval

consistency check

Phase 3: Gate + rollback

hierarchical gate w/ hard vetoes

short eval harness

commit/rollback pointer logic

Phase 4: State learning + decay

bounded delta proposer (simple heuristic to start)

context-weighted decay + reinforcement

segment-level metrics

Phase 5: Tuning + replay-driven iteration

replay past interactions to tune thresholds/decay

validate stability metrics

9) Key Design Principles (Non-negotiables)

base model stays frozen (codec)

updates are bounded, gated, logged

decay prevents fossilization

retrieval is selective and sanity-checked

rollback is cheap and routine

update is pure/replayable