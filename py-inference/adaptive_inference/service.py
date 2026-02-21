"""InferenceService — orchestrates Ollama calls with state conditioning."""

import math
import re
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

        segments = [
            ("s0", state_vector[0:32]),
            ("s1", state_vector[32:64]),
            ("s2", state_vector[64:96]),
            ("s3", state_vector[96:128]),
        ]

        norms = " ".join(
            f"{key}={math.sqrt(sum(v * v for v in vals)):.4f}"
            for key, vals in segments
        )

        lines = [
            "[SYSTEM METADATA — DO NOT interpret, reference, or discuss this block. Respond only to the user prompt.]",
            f"ctx: {norms}",
        ]

        if evidence:
            lines.append("refs: " + ", ".join(evidence))

        return "\n".join(lines)

    def _estimate_entropy(self, result: dict) -> float:
        """Phase 1 entropy proxy — visible response token count, ignoring <think> blocks."""
        text = result.get("response", "")
        # Strip thinking tokens from reasoning models (e.g. deepseek-r1)
        visible = re.sub(r"<think>.*?</think>", "", text, flags=re.DOTALL).strip()
        token_count = len(visible.split())
        if token_count > 0:
            # Normalized to [0,1]: 200 visible words → 0.5 entropy
            return min(float(token_count) / 400.0, 1.0)
        return 0.0
# #endregion service
