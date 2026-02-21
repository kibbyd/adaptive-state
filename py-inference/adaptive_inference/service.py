"""InferenceService — orchestrates Ollama calls with state conditioning."""

import math
from dataclasses import dataclass

from . import ollama_client

# #region types
@dataclass
class GenerateResult:
    text: str
    entropy: float
    logits: list[float]


@dataclass
class EmbedResult:
    embedding: list[float]
# #endregion types


# #region service
class InferenceService:
    """Orchestrates inference calls, injecting state context into prompts."""

    def __init__(self, model: str = ollama_client.DEFAULT_MODEL, base_url: str = ollama_client.DEFAULT_BASE_URL, embed_model: str = "qwen2.5-coder:7b"):
        self.model = model
        self.base_url = base_url
        self.embed_model = embed_model

    async def generate(
        self, prompt: str, state_vector: list[float], evidence: list[str]
    ) -> GenerateResult:
        """Generate a response conditioned on the state vector."""
        system_preamble = self._format_state_preamble(state_vector, evidence)

        result = await ollama_client.generate(
            prompt=prompt,
            system=system_preamble,
            model=self.model,
            base_url=self.base_url,
        )

        text = result.get("response", "")
        # Phase 1: entropy estimate from response length as proxy
        entropy = self._estimate_entropy(result)

        return GenerateResult(text=text, entropy=entropy, logits=[])

    async def embed(self, text: str) -> EmbedResult:
        """Get embedding for text."""
        embedding = await ollama_client.embed(
            text=text, model=self.embed_model, base_url=self.base_url
        )
        return EmbedResult(embedding=embedding)

    def _format_state_preamble(
        self, state_vector: list[float], evidence: list[str]
    ) -> str:
        """Format state vector segments into a system prompt preamble."""
        if not state_vector or len(state_vector) < 128:
            return ""

        segments = {
            "preferences": state_vector[0:32],
            "goals": state_vector[32:64],
            "heuristics": state_vector[64:96],
            "risk_profile": state_vector[96:128],
        }

        lines = ["[Adaptive State Context]"]
        for name, values in segments.items():
            norm = math.sqrt(sum(v * v for v in values))
            lines.append(f"  {name}: norm={norm:.4f}")

        if evidence:
            lines.append("  evidence_refs: " + ", ".join(evidence))

        return "\n".join(lines)

    def _estimate_entropy(self, result: dict) -> float:
        """Phase 1 entropy proxy — derived from eval metrics if available."""
        # Ollama may return eval_count and eval_duration
        eval_count = result.get("eval_count", 0)
        if eval_count > 0:
            # Normalized to [0,1]: 200 tokens → 0.5 entropy
            return min(float(eval_count) / 400.0, 1.0)
        return 0.0
# #endregion service
