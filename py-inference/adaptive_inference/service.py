"""InferenceService — orchestrates Ollama calls with state conditioning."""

import json
import logging
import re
from dataclasses import dataclass

from . import ollama_client

logger = logging.getLogger(__name__)

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


# #region tools
TOOLS = [
    {
        "type": "function",
        "function": {
            "name": "web_search",
            "description": "Search the web for current information. Use this when you don't know the answer or need to verify facts. NEVER guess — search instead.",
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {
                        "type": "string",
                        "description": "Search query — be specific",
                    }
                },
                "required": ["query"],
            },
        },
    }
]


_FACTUAL_QUESTION_WORDS = re.compile(
    r"\b(who|what|where|when|how much|how many|how long|how far|how old)\b", re.IGNORECASE
)
_FACTUAL_KEYWORDS = re.compile(
    r"\b(phone|address|number|hours|time|price|cost|population|capital|zip code|weather|score|rate|salary|distance)\b",
    re.IGNORECASE,
)


def _is_factual_question(text: str) -> bool:
    """Heuristic: does this prompt look like a factual question?"""
    has_question_word = _FACTUAL_QUESTION_WORDS.search(text) is not None
    has_factual_keyword = _FACTUAL_KEYWORDS.search(text) is not None
    has_question_mark = "?" in text
    # Require question structure AND a factual keyword
    return (has_question_mark or has_question_word) and has_factual_keyword


def _execute_tool(name: str, args: dict) -> str:
    """Execute a tool call and return the result string."""
    if name == "web_search":
        query = args.get("query", "")
        logger.info("tool call: web_search(%r)", query)
        try:
            from ddgs import DDGS
            with DDGS() as ddgs:
                results = list(ddgs.text(query, max_results=3))
            if not results:
                return "No search results found."
            output = "Search results:\n"
            for r in results:
                title = r.get("title", "No title")
                body = r.get("body", "")[:300]
                url = r.get("href", "")
                output += f"  [{title}]\n  {body}\n  {url}\n\n"
            return output
        except Exception as e:
            return f"Search failed: {e}"
    return f"Unknown tool: {name}"
# #endregion tools


# #region service
class InferenceService:
    """Orchestrates inference calls, injecting state context into prompts."""

    MAX_TOOL_DEPTH = 5

    def __init__(self, model: str = ollama_client.DEFAULT_MODEL, base_url: str = ollama_client.DEFAULT_BASE_URL, embed_model: str = "qwen3-embedding:0.6b"):
        self.model = model
        self.base_url = base_url
        self.embed_model = embed_model

    async def generate(
        self, prompt: str, state_vector: list[float], evidence: list[str],
        context: list[int] | None = None,
    ) -> GenerateResult:
        """Generate a response with native tool calling (chat API)."""
        system_prompt = self._build_system_prompt(state_vector, evidence)
        messages = [{"role": "user", "content": prompt}]

        has_evidence = bool(evidence)
        text = await self._chat_with_tools(messages, system_prompt, depth=0, has_evidence=has_evidence)
        visible = self._strip_think(text)

        # Qwen think-only failure: model emitted <think> but no answer.
        # Send continuation prompt to force answer generation.
        if not visible and "<think>" in text:
            logger.info("think-only response detected, sending continuation")
            messages.append({"role": "assistant", "content": text})
            messages.append({"role": "user", "content": "Provide the final answer only."})
            cont = await ollama_client.chat(
                messages=messages, system=system_prompt,
                model=self.model, base_url=self.base_url,
            )
            visible = self._strip_think(cont.get("message", {}).get("content", ""))

        # Last resort: if still empty, retry via generate endpoint
        if not visible:
            logger.info("still empty after continuation, fallback to generate")
            raw_prompt = messages[0].get("content", "")
            result = await ollama_client.generate(
                prompt=raw_prompt, system=system_prompt,
                model=self.model, base_url=self.base_url,
            )
            visible = self._strip_think(result.get("response", ""))

        # Entropy proxy from visible token count
        token_count = len(visible.split())
        entropy = min(float(token_count) / 400.0, 1.0) if token_count > 0 else 0.0

        return GenerateResult(text=visible, entropy=entropy, logits=[], context=[])

    @staticmethod
    def _strip_think(text: str) -> str:
        """Remove <think> blocks (including unclosed) and return visible text."""
        out = re.sub(r"<think>.*?</think>", "", text, flags=re.DOTALL)
        out = re.sub(r"<think>.*", "", out, flags=re.DOTALL)
        return out.strip()

    async def _chat_with_tools(
        self, messages: list[dict], system_prompt: str, depth: int,
        has_evidence: bool = False,
    ) -> str:
        """Recursive chat loop — executes tool calls until the model returns text."""
        if depth >= self.MAX_TOOL_DEPTH:
            logger.warning("tool depth limit reached (%d)", depth)
            return "I was unable to find the information after multiple searches."

        result = await ollama_client.chat(
            messages=messages,
            system=system_prompt,
            tools=TOOLS,
            model=self.model,
            base_url=self.base_url,
        )

        message = result.get("message", {})
        tool_calls = message.get("tool_calls")

        if tool_calls:
            # Append assistant message with tool calls
            messages.append(message)

            for tc in tool_calls:
                func = tc.get("function", {})
                tool_name = func.get("name", "")
                tool_args = func.get("arguments", {})
                logger.info("tool call: %s(%s)", tool_name, json.dumps(tool_args, ensure_ascii=False))

                tool_result = _execute_tool(tool_name, tool_args)
                messages.append({"role": "tool", "content": tool_result})

            return await self._chat_with_tools(messages, system_prompt, depth + 1, has_evidence)

        # Forced fallback: model skipped tool call on a factual question (skip if evidence already present)
        if not has_evidence:
            raw_prompt = messages[0].get("content", "") if messages else ""
            # Extract raw user text from wrapped prompt (after [USER PROMPT] marker)
            if "[USER PROMPT]" in raw_prompt:
                raw_prompt = raw_prompt.split("[USER PROMPT]")[-1].strip()
        if depth == 0 and not has_evidence and _is_factual_question(raw_prompt):
            logger.info("forced search fallback for factual question")
            search_result = _execute_tool("web_search", {"query": raw_prompt})
            messages.append(message)
            messages.append({"role": "tool", "content": search_result})
            return await self._chat_with_tools(messages, system_prompt, depth + 1)

        return message.get("content", "")

    async def embed(self, text: str) -> EmbedResult:
        """Get embedding for text."""
        embedding = await ollama_client.embed(
            text=text, model=self.embed_model, base_url=self.base_url
        )
        return EmbedResult(embedding=embedding)

    def _build_system_prompt(
        self, state_vector: list[float], evidence: list[str]
    ) -> str:
        """Build system prompt: behavioral rules + tool instruction + evidence preamble."""
        # Separate behavioral rules from regular evidence
        rules = []
        regular_evidence = []
        for item in (evidence or []):
            if item.strip().startswith("[BEHAVIORAL RULES]"):
                rules.append(item.strip())
            else:
                regular_evidence.append(item)

        lines = []

        # Behavioral rules go FIRST in system prompt (highest authority)
        if rules:
            for rule_block in rules:
                lines.append(rule_block)
            lines.append("")

        lines.append(
            "You have access to a web_search tool. You MUST use the web_search tool for any factual question (phone numbers, addresses, URLs, statistics, dates, current events). NEVER answer factual questions from memory — always search first. For casual conversation, respond normally without searching."
        )
        lines.append("Always provide a final answer after reasoning. Never output only reasoning.")

        if regular_evidence:
            lines.append("---")
            has_web = any("[Web Search Results]" in e for e in regular_evidence)
            if has_web:
                lines.append("Some context below comes from a live web search. Prefer web search results for factual queries.")
            lines.append("Use the following prior context to inform your answer. Do not repeat it verbatim.")
            for i, item in enumerate(regular_evidence, 1):
                text = item.strip()
                if len(text) > 500:
                    text = text[:500] + "..."
                lines.append(f"[{i}] {text}")

        return "\n".join(lines)
# #endregion service
