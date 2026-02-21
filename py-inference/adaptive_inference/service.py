"""InferenceService — orchestrates Ollama calls with state conditioning."""

import re
from dataclasses import dataclass

from . import ollama_client

# #region types
@dataclass
class GenerateResult:
    text: str
    entropy: float
    logits: list[float]
    context: list[int]


@dataclass
class EmbedResult:
    embedding: list[float]
# #endregion types


# #region service
class InferenceService:
    """Orchestrates inference calls, injecting state context into prompts."""

    def __init__(self, model: str = ollama_client.DEFAULT_MODEL, base_url: str = ollama_client.DEFAULT_BASE_URL, embed_model: str = "phi4-mini"):
        self.model = model
        self.base_url = base_url
        self.embed_model = embed_model

    async def generate(
        self, prompt: str, state_vector: list[float], evidence: list[str],
        context: list[int] | None = None,
    ) -> GenerateResult:
        """Generate a response conditioned on the state vector."""
        system_preamble = self._format_state_preamble(state_vector, evidence)

        result = await ollama_client.generate(
            prompt=prompt,
            system=system_preamble,
            model=self.model,
            base_url=self.base_url,
            context=context,
        )

        text = result.get("response", "")
        # Phase 1: entropy estimate from response length as proxy
        entropy = self._estimate_entropy(result)
        new_context = result.get("context", [])

        return GenerateResult(text=text, entropy=entropy, logits=[], context=new_context)

    async def embed(self, text: str) -> EmbedResult:
        """Get embedding for text."""
        embedding = await ollama_client.embed(
            text=text, model=self.embed_model, base_url=self.base_url
        )
        return EmbedResult(embedding=embedding)

    def _format_state_preamble(
        self, state_vector: list[float], evidence: list[str]
    ) -> str:
        """Format evidence into a system prompt preamble.

        State vector norms are not injected — small models interpret any
        numbers in the system prompt as content.  State conditioning happens
        through the Go-side pipeline (gate, retrieval, update, decay).
        """
        lines = ["You are a helpful assistant. Respond directly to the user."]

        if evidence:
            lines.append("---")
            lines.append("Use the following prior context to inform your answer. Do not repeat it verbatim.")
            for i, item in enumerate(evidence, 1):
                # Truncate long evidence and strip Q&A formatting
                text = item.strip()
                if len(text) > 500:
                    text = text[:500] + "..."
                lines.append(f"[{i}] {text}")

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
