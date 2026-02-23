# A 4B parameter model just held a 21-turn conversation with coherent personality, self-naming, and philosophical depth — no fine-tuning of base weights

I've been building an adaptive state system that sits on top of a frozen LLM (qwen3-4b via Ollama) and gives it persistent memory, learned preferences, and behavioral rules — without touching the model's weights.

Yesterday it held a 21-turn live conversation where it:
- Named itself "Orac" (from Blake's 7, after I suggested it)
- Maintained that identity across every subsequent turn
- Remembered my name ("Commander") without being reminded
- Told knock-knock jokes I'd taught it earlier via a rules system
- Had a genuinely interesting philosophical exchange about consciousness and self-awareness

All on a **2.6GB model running locally on my machine**.

## How it works

The architecture separates memory into three classes:

1. **Preferences** (identity + style) — stored in SQLite, projected into every prompt as an `[ADAPTIVE STATE]` block. "The user prefers concise answers", "The AI's name is Orac", etc. Detected automatically from conversation ("my name is X", "I prefer Y").

2. **Evidence** (context) — stored in ChromaDB as embeddings. Each turn, relevant past evidence is retrieved by cosine similarity with recency weighting. This is the *only* source of conversational memory — I removed Ollama's native context threading entirely because it caused bleed between unrelated topics.

3. **Rules** (behavior) — stored in SQLite. "When I say X, respond Y." Auto-extracted from conversation. When a rule fires, the system uses a rules-only system prompt with no other instructions — maximum compliance.

A Go controller manages all the adaptive state logic: a 128-dim state vector with signal-driven learning, gated updates, decay on unreinforced segments, hard vetoes, post-commit eval, and rollback. The model never sees raw state vectors — it sees human-readable preference text, weighted by adaptation magnitude.

The Python inference service handles generation via Ollama's `/api/chat` with native tool calling (web search via DuckDuckGo).

## What I learned

- **Context threading is the enemy of controllable memory.** Ollama's opaque token context caused joke patterns to leak into serious queries. Evidence retrieval gives you the same continuity but you can filter, weight, and audit it.
- **Rules need total isolation.** When a knock-knock joke rule fires, the system strips all other context — no preferences, no evidence, no tool instructions. Otherwise the model tries to "be helpful" instead of just delivering the punchline.
- **Identity detection needs hardening.** "I'm glad you think so" was being parsed as the user's name being "glad". Took a stopword filter, punctuation guard, and word count cap to fix.
- **Small models can have personality** if you give them the right scaffolding. qwen3-4b isn't doing anything magical — the architecture is doing the heavy lifting.

## Stats

- 95-100% test coverage on 11 Go packages
- Deterministic replay system (same inputs = same outputs, no model needed)
- ~30 commits since the behavioral rules layer was added
- 642-example training dataset for personality (JSONL, not yet fine-tuned — all results above are on the stock model)

Repo: [github.com/kibbyd/adaptive-state](https://github.com/kibbyd/adaptive-state)

Happy to answer questions about the architecture or any specific component.
