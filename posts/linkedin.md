# Yesterday, a 2.6GB AI running on my laptop named itself "Orac" and held a 21-turn philosophical conversation. No cloud. No fine-tuning. Just architecture.

I've spent the last few weeks building something I call Adaptive State — a system that gives a frozen language model persistent memory, learned preferences, and behavioral rules, all managed outside the model's weights.

The idea is simple: the LLM is a frozen codec. It encodes and decodes language. Everything else — personality, memory, learning, identity — lives in an external state layer that the model never directly sees. It sees human-readable preferences like "The AI's name is Orac" and "The user prefers concise answers." The scaffolding does the rest.

Yesterday it all came together. A 21-turn live conversation on qwen3-4b (a 2.6GB open-source model running locally via Ollama) where the AI:

- Adopted and maintained a name across every turn
- Remembered who I was without being reminded
- Delivered knock-knock jokes I'd taught it through a rules system
- Engaged in genuine philosophical exchange about consciousness

Three types of memory working in concert: preferences for identity and style, evidence retrieval for conversational context, and behavioral rules for taught patterns. Each isolated, each auditable, each controllable.

The technical details: a Go controller manages a 128-dimensional adaptive state vector with signal-driven learning, gated updates, decay, and rollback. A Python service handles generation via Ollama with native tool calling. 95-100% test coverage. Deterministic replay. Full provenance logging on every decision.

What I learned building this:

You don't need a massive model to get personality. You need the right architecture around a small one. The model is the voice. The system is the mind.

The repo is open source: https://github.com/kibbyd/adaptive-state

#AI #MachineLearning #OpenSource #LLM #LocalAI
