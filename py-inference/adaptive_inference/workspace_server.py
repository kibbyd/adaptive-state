"""Workspace HTTP server — exposes file ops, evidence ops, and cipher ops on localhost:8787."""

import asyncio
import json
import logging
import sys
import threading
from http.server import HTTPServer, BaseHTTPRequestHandler
from pathlib import Path
from urllib.parse import urlparse, parse_qs

# Add tools dir for cipher import
sys.path.insert(0, str(Path("C:/adaptive_state/tools")))
import cipher as _cipher

logger = logging.getLogger(__name__)

# #region config
WORKSPACE_DIR = Path("C:/adaptive_state/orac_workspace")
PORT = 8787
# #endregion config

# Module-level reference to memory store (set by start_workspace_server)
_memory_store = None
_async_loop = None


# #region sandbox
def _resolve_sandbox_path(relative_path: str) -> Path | None:
    """Resolve a relative path within the workspace sandbox. Returns None if unsafe."""
    WORKSPACE_DIR.mkdir(parents=True, exist_ok=True)
    try:
        resolved = (WORKSPACE_DIR / relative_path).resolve()
        if not str(resolved).startswith(str(WORKSPACE_DIR.resolve())):
            return None
        return resolved
    except (ValueError, OSError):
        return None
# #endregion sandbox


# #region handler
class WorkspaceHandler(BaseHTTPRequestHandler):
    """Handle file ops (/files/) and evidence ops (/evidence/)."""

    def do_GET(self):
        if self.path == "/inbox/read":
            self._inbox_read()
            return

        if self.path.startswith("/evidence"):
            self._search_evidence()
            return

        if not self.path.startswith("/files"):
            self._send(404, "Not found")
            return

        # Strip /files prefix
        rel = self.path[len("/files"):]
        # Remove leading slash
        rel = rel.lstrip("/")

        if not rel:
            # List files
            self._list_files()
        else:
            # Read file
            self._read_file(rel)

    def do_POST(self):
        # Read body for all POST routes
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length).decode("utf-8") if length > 0 else ""

        if self.path == "/inbox/send":
            self._inbox_send(body)
            return
        if self.path == "/cipher/encrypt":
            self._cipher_encrypt(body)
            return
        if self.path == "/cipher/decrypt":
            self._cipher_decrypt(body)
            return

        if not self.path.startswith("/files/"):
            self._send(404, "Not found")
            return

        rel = self.path[len("/files/"):]
        if not rel:
            self._send(400, "Path required for write")
            return

        self._write_file(rel, body)

    def do_DELETE(self):
        if not self.path.startswith("/evidence/"):
            self._send(404, "Not found")
            return
        self._delete_evidence()

    def _list_files(self):
        WORKSPACE_DIR.mkdir(parents=True, exist_ok=True)
        try:
            files = []
            for p in sorted(WORKSPACE_DIR.rglob("*")):
                if p.is_file():
                    rel = p.relative_to(WORKSPACE_DIR)
                    size = p.stat().st_size
                    files.append({"path": str(rel).replace("\\", "/"), "size": size})
            self._send(200, json.dumps({"files": files, "count": len(files)}))
        except Exception as e:
            self._send(500, f"List failed: {e}")

    def _read_file(self, rel_path: str):
        target = _resolve_sandbox_path(rel_path)
        if target is None:
            self._send(403, f"Rejected: path '{rel_path}' is outside workspace.")
            return
        if not target.exists():
            self._send(404, f"File not found: {rel_path}")
            return
        try:
            content = target.read_text(encoding="utf-8")
            if len(content) > 4000:
                content = content[:4000] + "\n... (truncated)"
            self._send(200, content)
        except Exception as e:
            self._send(500, f"Read failed: {e}")

    def _write_file(self, rel_path: str, content: str):
        target = _resolve_sandbox_path(rel_path)
        if target is None:
            self._send(403, f"Rejected: path '{rel_path}' is outside workspace.")
            return
        try:
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(content, encoding="utf-8")
            self._send(200, f"Written: {rel_path} ({len(content)} bytes)")
        except Exception as e:
            self._send(500, f"Write failed: {e}")

    # #region inbox-ops
    def _inbox_read(self):
        """GET /inbox/read — read and decrypt Commander's message in one call."""
        inbox_file = _cipher.INBOX_DIR / "from_commander.enc"
        if not inbox_file.exists():
            self._send(200, "No message from Commander.")
            return
        try:
            encrypted = inbox_file.read_text(encoding="utf-8").strip()
            if not encrypted:
                self._send(200, "No message from Commander.")
                return
            plaintext = _cipher.decrypt(encrypted)
            logger.info("Orac read inbox: decrypted %d chars", len(plaintext))
            self._send(200, plaintext)
        except Exception as e:
            logger.error("Inbox read error: %s", e)
            self._send(500, f"Inbox read failed: {e}")

    def _inbox_send(self, plaintext: str):
        """POST /inbox/send — encrypt and write reply to Commander in one call."""
        if not plaintext:
            self._send(400, "Body required: your message to Commander")
            return
        try:
            _cipher.INBOX_DIR.mkdir(parents=True, exist_ok=True)
            encrypted = _cipher.encrypt(plaintext)
            outfile = _cipher.INBOX_DIR / "to_commander.enc"
            outfile.write_text(encrypted, encoding="utf-8")
            logger.info("Orac sent inbox: encrypted %d chars -> %d chars", len(plaintext), len(encrypted))
            self._send(200, f"Message sent to Commander ({len(plaintext)} chars, encrypted).")
        except Exception as e:
            logger.error("Inbox send error: %s", e)
            self._send(500, f"Inbox send failed: {e}")
    # #endregion inbox-ops

    # #region cipher-ops
    def _cipher_encrypt(self, plaintext: str):
        """POST /cipher/encrypt — encrypt plaintext, return base64 ciphertext."""
        if not plaintext:
            self._send(400, "Body required: plaintext to encrypt")
            return
        try:
            ciphertext = _cipher.encrypt(plaintext)
            logger.info("Orac encrypted %d chars -> %d chars", len(plaintext), len(ciphertext))
            self._send(200, ciphertext)
        except Exception as e:
            logger.error("Cipher encrypt error: %s", e)
            self._send(500, f"Encrypt failed: {e}")

    def _cipher_decrypt(self, ciphertext: str):
        """POST /cipher/decrypt — decrypt base64 ciphertext, return plaintext."""
        if not ciphertext:
            self._send(400, "Body required: base64 ciphertext to decrypt")
            return
        try:
            plaintext = _cipher.decrypt(ciphertext.strip())
            logger.info("Orac decrypted %d chars -> %d chars", len(ciphertext), len(plaintext))
            self._send(200, plaintext)
        except Exception as e:
            logger.error("Cipher decrypt error: %s", e)
            self._send(500, f"Decrypt failed: {e}")
    # #endregion cipher-ops

    # #region evidence-ops
    def _search_evidence(self):
        """GET /evidence/?q=query — search evidence memory. Returns top 5 results."""
        global _memory_store, _async_loop
        if _memory_store is None:
            self._send(503, "Memory store not available")
            return

        parsed = urlparse(self.path)
        params = parse_qs(parsed.query)
        query = params.get("q", [""])[0]

        if not query:
            self._send(400, "Query parameter 'q' is required. Example: GET /evidence/?q=your+search+term")
            return

        try:
            future = asyncio.run_coroutine_threadsafe(
                _memory_store.search(query_text=query, top_k=5, threshold=0.2),
                _async_loop,
            )
            results = future.result(timeout=15)

            items = []
            for r in results:
                text = r.text
                if len(text) > 300:
                    text = text[:300] + "..."
                items.append({"id": r.id, "text": text, "score": round(r.score, 4)})

            self._send(200, json.dumps({"results": items, "count": len(items)}, indent=2))
        except Exception as e:
            logger.error("Evidence search error: %s", e)
            self._send(500, f"Search failed: {e}")

    def _delete_evidence(self):
        """DELETE /evidence/{id} — delete a single evidence item by ID."""
        global _memory_store, _async_loop
        if _memory_store is None:
            self._send(503, "Memory store not available")
            return

        doc_id = self.path[len("/evidence/"):]
        if not doc_id:
            self._send(400, "Evidence ID required. Example: DELETE /evidence/abc-123")
            return

        try:
            future = asyncio.run_coroutine_threadsafe(
                _memory_store.delete(doc_id),
                _async_loop,
            )
            success = future.result(timeout=10)

            if success:
                logger.info("Orac deleted evidence id=%s", doc_id)
                self._send(200, f"Deleted: {doc_id}")
            else:
                self._send(404, f"Not found or delete failed: {doc_id}")
        except Exception as e:
            logger.error("Evidence delete error: %s", e)
            self._send(500, f"Delete failed: {e}")
    # #endregion evidence-ops

    def _send(self, code: int, body: str):
        self.send_response(code)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.end_headers()
        self.wfile.write(body.encode("utf-8"))

    def log_message(self, format, *args):
        """Route HTTP logs through our logger instead of stderr."""
        logger.info("workspace-http: %s", format % args)
# #endregion handler


# #region start
def start_workspace_server(memory_store=None, async_loop=None):
    """Start the workspace HTTP server in a daemon thread. Returns the thread.
    If memory_store and async_loop are provided, evidence search/delete endpoints are enabled."""
    global _memory_store, _async_loop
    _memory_store = memory_store
    _async_loop = async_loop

    server = HTTPServer(("127.0.0.1", PORT), WorkspaceHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    logger.info("Workspace HTTP server started on http://127.0.0.1:%d (evidence_ops=%s)", PORT, memory_store is not None)
    return thread
# #endregion start
