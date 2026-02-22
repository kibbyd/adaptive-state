"""Ollama API client for generate and embed operations."""

import httpx

# #region config
DEFAULT_BASE_URL = "http://localhost:11434"
DEFAULT_MODEL = "qwen3-4b"
# #endregion config


# #region generate
async def generate(
    prompt: str,
    system: str = "",
    model: str = DEFAULT_MODEL,
    base_url: str = DEFAULT_BASE_URL,
    context: list[int] | None = None,
) -> dict:
    """Call Ollama /api/generate and return the response dict."""
    payload = {
        "model": model,
        "prompt": prompt,
        "stream": False,
    }
    if system:
        payload["system"] = system
    if context:
        payload["context"] = context

    async with httpx.AsyncClient(timeout=60.0) as client:
        resp = await client.post(f"{base_url}/api/generate", json=payload)
        resp.raise_for_status()
        return resp.json()
# #endregion generate


# #region embed
async def embed(
    text: str,
    model: str = DEFAULT_MODEL,
    base_url: str = DEFAULT_BASE_URL,
) -> list[float]:
    """Call Ollama /api/embed and return the embedding vector."""
    payload = {
        "model": model,
        "input": text,
    }

    async with httpx.AsyncClient(timeout=30.0) as client:
        resp = await client.post(f"{base_url}/api/embed", json=payload)
        resp.raise_for_status()
        data = resp.json()
        # Ollama returns {"embeddings": [[...]]}
        return data["embeddings"][0]
# #endregion embed


# #region chat
async def chat(
    messages: list[dict],
    system: str = "",
    tools: list[dict] | None = None,
    model: str = DEFAULT_MODEL,
    base_url: str = DEFAULT_BASE_URL,
) -> dict:
    """Call Ollama /api/chat with optional tools and return the response dict."""
    payload = {
        "model": model,
        "messages": messages,
        "stream": False,
    }
    if system:
        payload["messages"] = [{"role": "system", "content": system}] + payload["messages"]
    if tools:
        payload["tools"] = tools
    payload["options"] = {"num_predict": 512}

    async with httpx.AsyncClient(timeout=120.0) as client:
        resp = await client.post(f"{base_url}/api/chat", json=payload)
        resp.raise_for_status()
        return resp.json()
# #endregion chat
