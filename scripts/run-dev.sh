#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "==> Starting Python inference service..."
cd "$ROOT/py-inference"
python -m adaptive_inference.server &
PY_PID=$!

# Wait for Python gRPC server to be ready
sleep 2

echo "==> Starting Go controller..."
cd "$ROOT/go-controller"
go run ./cmd/controller/

# Cleanup
kill $PY_PID 2>/dev/null || true
