#!/usr/bin/env bash
# Test the Check App State action.
# Verifies: allTheTime / atLeastOnce modes and the failEarly option.
# Usage: ./test_check.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/helpers.sh"

PASSED=0
FAILED=0

assert_eq() {
  if [ "$1" = "$2" ]; then
    pass "$3"; PASSED=$((PASSED + 1))
  else
    fail "$3 (expected '$2', got '$1')"; FAILED=$((FAILED + 1))
  fi
}

assert_le() {
  if [ "$1" -le "$2" ]; then
    pass "$3"; PASSED=$((PASSED + 1))
  else
    fail "$3 (expected <= $2, got $1)"; FAILED=$((FAILED + 1))
  fi
}

assert_ge() {
  if [ "$1" -ge "$2" ]; then
    pass "$3"; PASSED=$((PASSED + 1))
  else
    fail "$3 (expected >= $2, got $1)"; FAILED=$((FAILED + 1))
  fi
}

assert_contains() {
  if echo "$1" | grep -q "$2"; then
    pass "$3"; PASSED=$((PASSED + 1))
  else
    fail "$3 (expected to contain '$2', got '$1')"; FAILED=$((FAILED + 1))
  fi
}

cleanup() {
  stop_extension
  cf start "$TEST_APP_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Run a check action and poll status until it errors or completes.
# Args: app_guid expected_state check_mode duration_ms [fail_early]
# Omit fail_early to test the default behavior.
# Sets: CHECK_RESULT (pass|fail), CHECK_ELAPSED (seconds), CHECK_ERROR_TITLE
run_check() {
  local app_guid="$1"
  local expected_state="$2"
  local check_mode="$3"
  local duration="$4"
  local fail_early="${5:-}"

  CHECK_RESULT="fail"
  CHECK_ELAPSED=0
  CHECK_ERROR_TITLE=""

  local fail_early_json=""
  if [ -n "$fail_early" ]; then
    fail_early_json=", \"failEarly\": $fail_early"
  fi

  local prepare_body
  prepare_body=$(cat <<EOF
{
  "target": {
    "name": "$TEST_APP_NAME",
    "attributes": {
      "cloudfoundry.app.guid": ["$app_guid"],
      "cloudfoundry.app.name": ["$TEST_APP_NAME"]
    }
  },
  "config": {
    "duration": $duration,
    "expectedState": "$expected_state",
    "stateCheckMode": "$check_mode"$fail_early_json
  }
}
EOF
)

  local prepare_resp
  prepare_resp=$(ext_post "/com.steadybit.extension_cloudfoundry.app.check/prepare" "$prepare_body")
  if [ -z "$prepare_resp" ]; then
    CHECK_ERROR_TITLE="prepare failed"
    return 0
  fi

  local state
  state=$(echo "$prepare_resp" | python3 -c "import json,sys; print(json.dumps(json.load(sys.stdin)['state']))")

  local start_ts
  start_ts=$(date +%s)

  ext_post "/com.steadybit.extension_cloudfoundry.app.check/start" "{\"state\":$state}" >/dev/null 2>&1

  local max_polls=$(( (duration / 1000) + 10 ))
  local poll=0
  while [ $poll -lt $max_polls ]; do
    local status_resp
    status_resp=$(ext_post "/com.steadybit.extension_cloudfoundry.app.check/status" "{\"state\":$state}" 2>/dev/null || echo "")

    if [ -z "$status_resp" ]; then
      sleep 1
      poll=$((poll + 1))
      continue
    fi

    local parsed
    parsed=$(echo "$status_resp" | python3 -c "
import json, sys
d = json.load(sys.stdin)
err = d.get('error') or {}
print(d.get('completed', False))
print(err.get('title', ''))
print(json.dumps(d['state']) if d.get('state') else '')
" 2>/dev/null)

    local completed error_title new_state
    completed=$(echo "$parsed" | sed -n 1p)
    error_title=$(echo "$parsed" | sed -n 2p)
    new_state=$(echo "$parsed" | sed -n 3p)

    if [ -n "$new_state" ]; then
      state="$new_state"
    fi

    if [ -n "$error_title" ]; then
      CHECK_RESULT="fail"
      CHECK_ERROR_TITLE="$error_title"
      CHECK_ELAPSED=$(( $(date +%s) - start_ts ))
      return 0
    fi

    if [ "$completed" = "True" ]; then
      CHECK_RESULT="pass"
      CHECK_ELAPSED=$(( $(date +%s) - start_ts ))
      return 0
    fi

    sleep 1
    poll=$((poll + 1))
  done

  CHECK_ERROR_TITLE="timed out polling status"
  CHECK_ELAPSED=$(( $(date +%s) - start_ts ))
  return 0
}

# Ensure the app is in the STARTED desired state before a test.
ensure_app_started() {
  cf start "$TEST_APP_NAME" >/dev/null 2>&1 || true
  sleep 2
}

main() {
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Test: Check App State Action"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

  ensure_app_started

  start_extension

  APP_GUID=$(get_app_guid)
  info "App GUID: $APP_GUID"

  # -------------------------------------------------------
  header "Test 1: allTheTime, app stable (failEarly omitted)"
  info "App is STARTED, checking for 6s — should PASS"
  run_check "$APP_GUID" "STARTED" "allTheTime" 6000
  assert_eq "$CHECK_RESULT" "pass" "allTheTime succeeds when app stays started"

  # -------------------------------------------------------
  header "Test 2: allTheTime, deviation, failEarly omitted (defaults to true)"
  info "App will be stopped 3s into a 15s check — should FAIL before the step ends"

  (sleep 3 && cf stop "$TEST_APP_NAME" >/dev/null 2>&1) &
  BG_PID=$!
  run_check "$APP_GUID" "STARTED" "allTheTime" 15000
  wait $BG_PID 2>/dev/null || true

  assert_eq "$CHECK_RESULT" "fail" "check fails when app state deviates"
  assert_le "$CHECK_ELAPSED" 9 "check failed early (after ${CHECK_ELAPSED}s, well before the 15s duration)"
  assert_contains "$CHECK_ERROR_TITLE" "is in state" "error reports current deviation (present tense)"

  ensure_app_started

  # -------------------------------------------------------
  header "Test 3: allTheTime, deviation, failEarly=true (explicit)"
  info "App will be stopped 3s into a 15s check — should FAIL before the step ends"

  (sleep 3 && cf stop "$TEST_APP_NAME" >/dev/null 2>&1) &
  BG_PID=$!
  run_check "$APP_GUID" "STARTED" "allTheTime" 15000 true
  wait $BG_PID 2>/dev/null || true

  assert_eq "$CHECK_RESULT" "fail" "check fails when app state deviates"
  assert_le "$CHECK_ELAPSED" 9 "check failed early (after ${CHECK_ELAPSED}s, well before the 15s duration)"

  ensure_app_started

  # -------------------------------------------------------
  header "Test 4: allTheTime, deviation with recovery, failEarly=false"
  info "App stopped at 3s, restarted at 7s during a 15s check — should FAIL, but only at the end"

  (sleep 3 && cf stop "$TEST_APP_NAME" >/dev/null 2>&1 && sleep 4 && cf start "$TEST_APP_NAME" >/dev/null 2>&1) &
  BG_PID=$!
  run_check "$APP_GUID" "STARTED" "allTheTime" 15000 false
  wait $BG_PID 2>/dev/null || true

  assert_eq "$CHECK_RESULT" "fail" "check still fails even though the app recovered"
  assert_ge "$CHECK_ELAPSED" 12 "check ran for the whole duration (${CHECK_ELAPSED}s of 15s)"
  assert_contains "$CHECK_ERROR_TITLE" "was in state" "error reports past deviation (past tense)"

  ensure_app_started

  # -------------------------------------------------------
  header "Test 5: allTheTime, app stable, failEarly=false"
  info "App is STARTED, checking for 6s — should PASS"
  run_check "$APP_GUID" "STARTED" "allTheTime" 6000 false
  assert_eq "$CHECK_RESULT" "pass" "failEarly=false passes when no deviation is seen"

  # -------------------------------------------------------
  header "Test 6: atLeastOnce reaches expected state mid-check"
  info "Expecting STOPPED, app stopped 3s into a 10s check — should PASS"

  (sleep 3 && cf stop "$TEST_APP_NAME" >/dev/null 2>&1) &
  BG_PID=$!
  run_check "$APP_GUID" "STOPPED" "atLeastOnce" 10000
  wait $BG_PID 2>/dev/null || true

  assert_eq "$CHECK_RESULT" "pass" "atLeastOnce succeeds when state is reached during the check"

  ensure_app_started

  # -------------------------------------------------------
  header "Test 6b: atLeastOnce is unaffected by failEarly=true"
  info "Expecting STOPPED with failEarly=true, app STARTED (deviating) at start, stopped at 3s of a 10s check"
  info "Must NOT fail early on the initial deviation — should PASS at the end"

  (sleep 3 && cf stop "$TEST_APP_NAME" >/dev/null 2>&1) &
  BG_PID=$!
  run_check "$APP_GUID" "STOPPED" "atLeastOnce" 10000 true
  wait $BG_PID 2>/dev/null || true

  assert_eq "$CHECK_RESULT" "pass" "atLeastOnce passes despite failEarly=true and an initial deviation"
  assert_ge "$CHECK_ELAPSED" 8 "check ran the full duration (${CHECK_ELAPSED}s of 10s), no early exit"

  ensure_app_started

  # -------------------------------------------------------
  header "Test 7: atLeastOnce never reaches expected state"
  info "Expecting STOPPED, app stays STARTED for 6s — should FAIL at the end"
  run_check "$APP_GUID" "STOPPED" "atLeastOnce" 6000
  assert_eq "$CHECK_RESULT" "fail" "atLeastOnce fails when state is never reached"
  assert_contains "$CHECK_ERROR_TITLE" "never reached" "error reports state never reached"

  print_summary $PASSED $FAILED
}

main "$@"
