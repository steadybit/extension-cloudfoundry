#!/usr/bin/env bash
# Build and start the extension against the local Korifi cluster.
# The extension runs in the foreground — press Ctrl+C to stop.
# Usage: ./start_extension.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/helpers.sh"

cleanup() {
  stop_extension
}
trap cleanup EXIT INT TERM

main() {
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Cloud Foundry Extension — Start"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

  header "Building extension..."
  go build -o "$SCRIPT_DIR/extension-under-test" "$SCRIPT_DIR/.."
  pass "Build successful"

  header "Generating bearer token..."
  TOKEN=$(generate_token)
  if [ -z "$TOKEN" ]; then
    fail "Could not generate token. Is the KIND cluster running with Korifi?"
    exit 1
  fi
  pass "Token generated"

  header "Starting extension (Ctrl+C to stop)..."
  info "API URL:  https://localhost"
  info "Port:     $EXTENSION_PORT"
  info "Log level: debug"
  echo ""

  STEADYBIT_EXTENSION_API_URL=https://localhost \
  STEADYBIT_EXTENSION_BEARER_TOKEN="$TOKEN" \
  STEADYBIT_EXTENSION_SKIP_TLS_VERIFY=true \
  STEADYBIT_EXTENSION_PORT="$EXTENSION_PORT" \
  STEADYBIT_LOG_LEVEL=debug \
  "$SCRIPT_DIR/extension-under-test"
}

main "$@"
