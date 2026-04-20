#!/usr/bin/env bash
# Common helpers for test scripts

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

EXTENSION_PORT=8085
EXTENSION_PID=""
KIND_CLUSTER_NAME="korifi"
KIND_CONTEXT="kind-${KIND_CLUSTER_NAME}"
CF_ORG="test-org"
CF_SPACE="test-space"
TEST_APP_NAME="petclinic"
TEST_APP_IMAGE=""  # pushed via buildpack, not docker image

pass() { echo -e "  ${GREEN}PASS${NC} $1"; }
fail() { echo -e "  ${RED}FAIL${NC} $1"; }
info() { echo -e "  ${BLUE}INFO${NC} $1"; }
warn() { echo -e "  ${YELLOW}WARN${NC} $1"; }
header() { echo -e "\n${BOLD}$1${NC}"; }

# Run a curl against the extension and return the body
ext_get() {
  curl -sf "http://localhost:${EXTENSION_PORT}$1" 2>/dev/null
}

ext_post() {
  curl -sf -X POST "http://localhost:${EXTENSION_PORT}$1" \
    -H "Content-Type: application/json" \
    -d "$2" 2>/dev/null
}

# Get the app GUID from discovered targets
get_app_guid() {
  local app_name="${1:-$TEST_APP_NAME}"
  ext_get "/com.steadybit.extension_cloudfoundry.app/discovery/discovered-targets" \
    | python3 -c "
import json, sys
d = json.load(sys.stdin)
for t in d.get('targets', d if isinstance(d, list) else []):
  for v in t.get('attributes', {}).get('cloudfoundry.app.name', []):
    if v == '${app_name}':
      print(t['attributes']['cloudfoundry.app.guid'][0])
      sys.exit(0)
sys.exit(1)
"
}

# Get app state from CF API
get_cf_app_state() {
  cf app "$1" 2>/dev/null | grep "requested state" | awk '{print $NF}'
}

# Wait for the extension to be healthy
wait_for_extension() {
  local timeout=${1:-30}
  local elapsed=0
  while [ $elapsed -lt $timeout ]; do
    if curl -sf "http://localhost:${EXTENSION_PORT}/health/readiness" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  return 1
}

# Wait for discovery to find a target
wait_for_target() {
  local app_name="${1:-$TEST_APP_NAME}"
  local timeout=${2:-120}
  local elapsed=0
  while [ $elapsed -lt $timeout ]; do
    if get_app_guid "$app_name" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
    elapsed=$((elapsed + 2))
  done
  return 1
}

# Generate a bearer token for the extension
generate_token() {
  kubectl --context "$KIND_CONTEXT" create token cf-extension-sa -n cf --duration=24h 2>/dev/null
}

# Start the extension process
start_extension() {
  header "Starting extension..."
  go build -o ./extension-under-test .. 2>/dev/null
  local token
  token=$(generate_token)

  STEADYBIT_EXTENSION_API_URL=https://localhost \
  STEADYBIT_EXTENSION_BEARER_TOKEN="$token" \
  STEADYBIT_EXTENSION_SKIP_TLS_VERIFY=true \
  STEADYBIT_EXTENSION_PORT="$EXTENSION_PORT" \
  STEADYBIT_LOG_LEVEL=info \
  ./extension-under-test >/dev/null 2>&1 &
  EXTENSION_PID=$!

  if ! wait_for_extension 30; then
    fail "Extension did not start within 30s"
    exit 1
  fi
  info "Extension running (PID $EXTENSION_PID)"

  info "Waiting for target discovery..."
  if ! wait_for_target "$TEST_APP_NAME" 120; then
    fail "Target '$TEST_APP_NAME' not discovered within 120s"
    exit 1
  fi
  info "Target '$TEST_APP_NAME' discovered"
}

# Stop the extension process
stop_extension() {
  if [ -n "$EXTENSION_PID" ] && kill -0 "$EXTENSION_PID" 2>/dev/null; then
    kill "$EXTENSION_PID" 2>/dev/null || true
    wait "$EXTENSION_PID" 2>/dev/null || true
  fi
  rm -f ./extension-under-test
}

# Print test summary
print_summary() {
  local passed=$1
  local failed=$2
  local total=$((passed + failed))

  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  if [ "$failed" -eq 0 ]; then
    echo -e "  ${GREEN}${BOLD}All $total tests passed${NC}"
  else
    echo -e "  ${RED}${BOLD}$failed of $total tests failed${NC}"
  fi
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

  [ "$failed" -eq 0 ]
}
