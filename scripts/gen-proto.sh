#!/usr/bin/env bash
set -euo pipefail

PROTO_DIR="$(cd "$(dirname "$0")/.." && pwd)/proto"
GO_OUT="$(cd "$(dirname "$0")/.." && pwd)/go-controller/gen"
PY_OUT="$(cd "$(dirname "$0")/.." && pwd)/py-inference/adaptive_inference/proto"

echo "==> Generating Go protobuf stubs..."
protoc \
  --proto_path="$PROTO_DIR" \
  --go_out="$GO_OUT/adaptive" \
  --go_opt=paths=source_relative \
  --go-grpc_out="$GO_OUT/adaptive" \
  --go-grpc_opt=paths=source_relative \
  "$PROTO_DIR/adaptive.proto"

echo "==> Generating Python protobuf stubs..."
python -m grpc_tools.protoc \
  --proto_path="$PROTO_DIR" \
  --python_out="$PY_OUT" \
  --grpc_python_out="$PY_OUT" \
  --pyi_out="$PY_OUT" \
  "$PROTO_DIR/adaptive.proto"

echo "==> Done."
