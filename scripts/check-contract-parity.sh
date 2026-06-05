#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOCAL_VECTOR="$ROOT_DIR/contract/v3_vectors.json"
LOCAL_SDK_API="$ROOT_DIR/contract/sdk-api.v1.json"
WORKSPACE_VECTOR="$ROOT_DIR/../../contract/v3_vectors.json"
WORKSPACE_SDK_API="$ROOT_DIR/../../contract/sdk-api.v1.json"

if [[ ! -f "$LOCAL_VECTOR" ]]; then
  echo "missing SDK contract vector: $LOCAL_VECTOR" >&2
  exit 1
fi
if [[ ! -f "$LOCAL_SDK_API" ]]; then
  echo "missing SDK API contract: $LOCAL_SDK_API" >&2
  exit 1
fi

if [[ -f "$WORKSPACE_VECTOR" ]]; then
  cmp -s "$WORKSPACE_VECTOR" "$LOCAL_VECTOR" || {
    echo "SDK contract vector differs from workspace contract/v3_vectors.json" >&2
    exit 1
  }
else
  echo "workspace contract vector not present; checked repository-local vector only"
fi
if [[ -f "$WORKSPACE_SDK_API" ]]; then
  cmp -s "$WORKSPACE_SDK_API" "$LOCAL_SDK_API" || {
    echo "SDK API contract differs from workspace contract/sdk-api.v1.json" >&2
    exit 1
  }
else
  echo "workspace SDK API contract not present; checked repository-local contract only"
fi

(cd "$ROOT_DIR" && go test -run 'TestV3ContractParity|TestSDKAPIContract' .)

echo "SDK contract parity checks passed"
