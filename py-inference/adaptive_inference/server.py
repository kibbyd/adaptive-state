"""gRPC server implementing CodecService."""

import asyncio
import logging
import os
import sys
import threading
from concurrent import futures

import grpc

# Add proto path for generated stubs
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "proto"))

import adaptive_pb2 as pb2
import adaptive_pb2_grpc as pb2_grpc

from .memory import MemoryStore
from .service import InferenceService

logger = logging.getLogger(__name__)

# #region grpc-servicer
class CodecServiceServicer(pb2_grpc.CodecServiceServicer):
    """gRPC servicer that delegates to InferenceService."""

    def __init__(self, inference_service: InferenceService, memory_store: MemoryStore, embed_model: str = "qwen3-embedding:0.6b"):
        self._service = inference_service
        self._memory = memory_store
        self._embed_model = embed_model
        self._loop = asyncio.new_event_loop()
        self._loop_thread = threading.Thread(target=self._loop.run_forever, daemon=True)
        self._loop_thread.start()

    def _run(self, coro):
        """Schedule a coroutine on the dedicated loop and block for the result."""
        return asyncio.run_coroutine_threadsafe(coro, self._loop).result()

    def Generate(self, request, context):
        """Handle Generate RPC."""
        logger.info("Generate called: prompt=%s...", request.prompt[:50] if request.prompt else "")

        try:
            result = self._run(
                self._service.generate(
                    prompt=request.prompt,
                    state_vector=list(request.state_vector),
                    evidence=list(request.evidence),
                    context=list(request.context) if request.context else None,
                )
            )
            return pb2.GenerateResponse(
                text=result.text,
                entropy=result.entropy,
                logits=result.logits,
                context=result.context,
            )
        except Exception as e:
            logger.error("Generate error: %s", e)
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return pb2.GenerateResponse()

    def Embed(self, request, context):
        """Handle Embed RPC."""
        logger.info("Embed called: text=%s...", request.text[:50] if request.text else "")

        try:
            result = self._run(
                self._service.embed(text=request.text)
            )
            return pb2.EmbedResponse(embedding=result.embedding)
        except Exception as e:
            logger.error("Embed error: %s", e)
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return pb2.EmbedResponse()

    def Search(self, request, context):
        """Handle Search RPC — query the evidence memory store."""
        logger.info("Search called: query=%s..., top_k=%d, threshold=%.2f",
                     request.query_text[:50] if request.query_text else "",
                     request.top_k, request.similarity_threshold)

        try:
            results = self._run(
                self._memory.search(
                    query_text=request.query_text,
                    top_k=request.top_k if request.top_k > 0 else 5,
                    threshold=request.similarity_threshold,
                )
            )
            return pb2.SearchResponse(
                results=[
                    pb2.SearchResult(
                        id=r.id,
                        text=r.text,
                        score=r.score,
                        metadata_json=r.metadata_json,
                    )
                    for r in results
                ]
            )
        except Exception as e:
            logger.error("Search error: %s", e)
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return pb2.SearchResponse()

    def StoreEvidence(self, request, context):
        """Handle StoreEvidence RPC — store text in the evidence memory."""
        logger.info("StoreEvidence called: text=%s...", request.text[:50] if request.text else "")

        try:
            import json
            metadata = json.loads(request.metadata_json) if request.metadata_json else {}
            doc_id = self._run(
                self._memory.store(text=request.text, metadata=metadata)
            )
            return pb2.StoreEvidenceResponse(id=doc_id)
        except Exception as e:
            logger.error("StoreEvidence error: %s", e)
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return pb2.StoreEvidenceResponse()

    def WebSearch(self, request, context):
        """Handle WebSearch RPC — search the web using DDGS."""
        logger.info("WebSearch called: query=%s..., max_results=%d",
                     request.query[:50] if request.query else "", request.max_results)

        try:
            from ddgs import DDGS

            max_results = request.max_results if request.max_results > 0 else 3
            with DDGS() as ddgs:
                raw = list(ddgs.text(request.query, max_results=max_results))

            results = []
            for r in raw:
                results.append(pb2.WebSearchResult(
                    title=r.get("title", ""),
                    snippet=r.get("body", ""),
                    url=r.get("href", ""),
                ))
            return pb2.WebSearchResponse(results=results)
        except Exception as e:
            logger.error("WebSearch error: %s", e)
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return pb2.WebSearchResponse()
# #endregion grpc-servicer


# #region serve
def serve():
    """Start the gRPC server."""
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")

    port = os.environ.get("GRPC_PORT", "50051")
    model = os.environ.get("OLLAMA_MODEL", "qwen3-4b")
    embed_model = os.environ.get("EMBED_MODEL", "qwen3-embedding:0.6b")
    ollama_url = os.environ.get("OLLAMA_URL", "http://localhost:11434")

    persist_dir = os.environ.get("MEMORY_PERSIST_DIR", "./chroma_data")
    inference = InferenceService(model=model, base_url=ollama_url, embed_model=embed_model)
    memory = MemoryStore(persist_dir=persist_dir, embed_model=embed_model)
    servicer = CodecServiceServicer(inference, memory, embed_model=embed_model)

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4))
    pb2_grpc.add_CodecServiceServicer_to_server(servicer, server)
    server.add_insecure_port(f"0.0.0.0:{port}")

    logger.info("Starting gRPC server on port %s (model=%s, embed_model=%s, ollama=%s)", port, model, embed_model, ollama_url)
    server.start()
    server.wait_for_termination()
# #endregion serve


if __name__ == "__main__":
    serve()
