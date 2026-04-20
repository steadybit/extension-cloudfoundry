#!/usr/bin/env bash
# Test the Check App State action.
# Verifies: noEvents allTheTime, noEvents atLeastOnce, state checks.
# Usage: ./test_check.sh

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

assert_fail() {
  if eval "$1" >/dev/null 2>&1; then
    fail "$2 (expected failure but got success)"; FAILED=$((FAILED + 1))
  else
    pass "$2"; PASSED=$((PASSED + 1))
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
  cf start "$TEST_APP_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Run a check action and wait for completion. Returns 0 if check passed, 1 if failed.
run_check() {
  local app_guid="$1"
  local expected_states="$2"
  local check_mode="$3"
  local duration="$4"

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
    "expectedStates": $expected_states,
    "stateCheckMode": "$check_mode"
  }
}
EOF
)

  local prepare_resp
  prepare_resp=$(ext_post "/com.steadybit.extension_cloudfoundry.app.check/prepare" "$prepare_body")
  if [ -z "$prepare_resp" ]; then
    return 1
  fi

  local state
  state=$(echo "$prepare_resp" | python3 -c "import json,sys; print(json.dumps(json.load(sys.stdin)['state']))")

  # Start
  ext_post "/com.steadybit.extension_cloudfoundry.app.check/start" "{\"state\":$state}" >/dev/null 2>&1

  # Poll status until completed
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

    local completed
    completed=$(echo "$status_resp" | python3 -c "import json,sys; print(json.load(sys.stdin).get('completed', False))" 2>/dev/null)

    local has_error
    has_error=$(echo "$status_resp" | python3 -c "import json,sys; e=json.load(sys.stdin).get('error'); print('yes' if e else 'no')" 2>/dev/null)

    # Update state from response if present
    local new_state
    new_state=$(echo "$status_resp" | python3 -c "
import json, sys
d = json.load(sys.stdin)
if 'state' in d and d['state']:
    print(json.dumps(d['state']))
else:
    print('')
" 2>/dev/null)
    if [ -n "$new_state" ]; then
      state="$new_state"
    fi

    if [ "$has_error" = "yes" ]; then
      return 1
    fi

    if [ "$completed" = "True" ]; then
      return 0
    fi

    sleep 1
    poll=$((poll + 1))
  done
  return 1
}

main() {
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Test: Check App State Action"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

  cf start "$TEST_APP_NAME" >/dev/null 2>&1 || true

  start_extension

  APP_GUID=$(get_app_guid)
  info "App GUID: $APP_GUID"

  # -------------------------------------------------------
  header "Test 1: No events + All the time (app stays stable)"
  info "App is STARTED, checking for 6s — should PASS"
  assert_pass "run_check '$APP_GUID' '[\"noEvents\"]' 'allTheTime' 6000" \
    "No events + allTheTime succeeds when app is stable"

  # -------------------------------------------------------
  header "Test 2: No events + All the time (app changes mid-check)"
  info "App is STARTED, will be stopped during check — should FAIL"

  # Start check in background, stop app during it
  (sleep 4 && cf stop "$TEST_APP_NAME" >/dev/null 2>&1) &
  STOP_PID=$!

  assert_fail "run_check '$APP_GUID' '[\"noEvents\"]' 'allTheTime' 10000" \
    "No events + allTheTime fails when app state changes"

  wait $STOP_PID 2>/dev/null || true
  cf start "$TEST_APP_NAME" >/dev/null 2>&1 || true
  sleep 3

  # -------------------------------------------------------
  header "Test 3: No events + At least once (app changes later)"
  info "App is STARTED, will be stopped mid-check — should PASS (saw no events initially)"

  (sleep 4 && cf stop "$TEST_APP_NAME" >/dev/null 2>&1) &
  STOP_PID=$!

  assert_pass "run_check '$APP_GUID' '[\"noEvents\"]' 'atLeastOnce' 10000" \
    "No events + atLeastOnce succeeds when no events seen at least once"

  wait $STOP_PID 2>/dev/null || true
  cf start "$TEST_APP_NAME" >/dev/null 2>&1 || true
  sleep 3

  # -------------------------------------------------------
  header "Test 4: Expected STARTED + All the time (app is running)"
  info "App is STARTED, checking for 6s — should PASS"
  assert_pass "run_check '$APP_GUID' '[\"STARTED\"]' 'allTheTime' 6000" \
    "Expected STARTED + allTheTime succeeds when app is started"

  # -------------------------------------------------------
  header "Test 5: Expected STOPPED + All the time (app is running)"
  info "App is STARTED but expecting STOPPED — should FAIL"
  assert_fail "run_check '$APP_GUID' '[\"STOPPED\"]' 'allTheTime' 6000" \
    "Expected STOPPED + allTheTime fails when app is started"

  print_summary $PASSED $FAILED
}

main "$@"
