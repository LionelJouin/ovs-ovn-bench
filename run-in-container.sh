#!/usr/bin/env bash
set -euo pipefail

# Auto-detect container runtime (docker or podman)
if command -v docker >/dev/null 2>&1; then
    RUNTIME="docker"
elif command -v podman >/dev/null 2>&1; then
    RUNTIME="podman"
else
    echo "Error: Neither docker nor podman was found in PATH." >&2
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p "$SCRIPT_DIR/results"

echo "==> Building benchmark container (Ubuntu 26.04 with OVS, OVN, and jemalloc)..."
$RUNTIME build -t ovs-ovn-bench "$SCRIPT_DIR"

echo "==> Running OVS/OVN scaling benchmark inside container..."
$RUNTIME run --rm -v "$SCRIPT_DIR/results:/workspace/results:Z" ovs-ovn-bench "$@"

# Ensure generated result files on the host match the host user's UID/GID
if [ "$(id -u)" != "0" ] && [ -d "$SCRIPT_DIR/results" ]; then
    $RUNTIME run --rm -v "$SCRIPT_DIR/results:/workspace/results:Z" alpine chown -R "$(id -u):$(id -g)" /workspace/results 2>/dev/null || true
fi

echo "==> Benchmark complete! Results saved to $SCRIPT_DIR/results/BENCHMARK_REPORT.md"
