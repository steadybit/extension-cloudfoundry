#!/usr/bin/env bash
# Tear down the KIND cluster.
# Usage: ./teardown.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/helpers.sh"

header "Tearing down KIND cluster '${KIND_CLUSTER_NAME}'..."
kind delete cluster --name "$KIND_CLUSTER_NAME" 2>/dev/null || true
pass "Cluster deleted"
