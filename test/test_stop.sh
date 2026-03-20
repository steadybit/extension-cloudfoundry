#!/usr/bin/env bash
# Test the Stop App action.
# Verifies: prepare, start (stops app), rollback (restarts app).
# Usage: ./test_stop.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/helpers.sh"

PASSED=0
FAILED=0

assert_pass() {
  if eval "$1" >/dev/null 2>&1; then
    pass "$2"; PASSED=$((PASSED + 1))
  else
    fail "$2"; FAILED=$((FAILED + 1))
  fi
}

assert_eq() {
  if [ "$1" = "$2" ]; then
    pass "$3"; PASSED=$((PASSED + 1))
  else
    fail "$3 (expected '$2', got '$1')"; FAILED=$((FAILED + 1))
  fi
}

cleanup() {
  stop_extension
  # Ensure app is started for other tests
  cf start "$TEST_APP_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

main() {
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Test: Stop App Action"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

  # Ensure app is started
  cf start "$TEST_APP_NAME" >/dev/null 2>&1 || true

  start_extension

  APP_GUID=$(get_app_guid)
  info "App GUID: $APP_GUID"

  # --- Prepare ---
  header "Step 1: Prepare"
  PREPARE_RESP=$(ext_post "/com.steadybit.extension_cloudfoundry.app.stop/prepare" \
    "{\"target\":{\"name\":\"$TEST_APP_NAME\",\"attributes\":{\"cf.app.guid\":[\"$APP_GUID\"],\"cf.app.name\":[\"$TEST_APP_NAME\"]}},\"config\":{\"duration\":30000}}")

  assert_pass "echo '$PREPARE_RESP' | python3 -c \"import json,sys; d=json.load(sys.stdin); assert d['state']['AppGUID'] == '$APP_GUID'\"" \
    "Prepare returns correct AppGUID"

  INITIAL_STATE=$(echo "$PREPARE_RESP" | python3 -c "import json,sys; print(json.load(sys.stdin)['state']['InitialState'])")
  assert_eq "$INITIAL_STATE" "STARTED" "Prepare captures initial state as STARTED"

  STATE=$(echo "$PREPARE_RESP" | python3 -c "import json,sys; print(json.dumps(json.load(sys.stdin)['state']))")

  # --- Start (stop the app) ---
  header "Step 2: Start (stops the app)"
  START_RESP=$(ext_post "/com.steadybit.extension_cloudfoundry.app.stop/start" \
    "{\"state\":$STATE}")

  assert_pass "echo '$START_RESP' | python3 -c \"import json,sys; d=json.load(sys.stdin); assert any('Stopped' in m['message'] for m in d['messages'])\"" \
    "Start returns stop confirmation message"

  sleep 2
  CF_STATE=$(get_cf_app_state "$TEST_APP_NAME")
  assert_eq "$CF_STATE" "stopped" "App is stopped in Cloud Foundry"

  # --- Stop (rollback) ---
  header "Step 3: Stop (rollback — restarts the app)"
  STOP_RESP=$(ext_post "/com.steadybit.extension_cloudfoundry.app.stop/stop" \
    "{\"state\":$STATE}")

  assert_pass "echo '$STOP_RESP' | python3 -c \"import json,sys; d=json.load(sys.stdin); assert any('Restarted' in m['message'] for m in d['messages'])\"" \
    "Rollback returns restart confirmation message"

  sleep 3
  CF_STATE=$(get_cf_app_state "$TEST_APP_NAME")
  assert_eq "$CF_STATE" "started" "App is started again after rollback"

  print_summary $PASSED $FAILED
}

main "$@"
