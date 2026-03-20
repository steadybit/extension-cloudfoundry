#!/usr/bin/env bash
# Run all attack tests and print a combined summary.
# Usage: ./run_all.sh
#
# Prerequisites: run ./setup.sh first to create the KIND cluster + Korifi + test app.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/helpers.sh"

TOTAL_PASSED=0
TOTAL_FAILED=0
SUITE_RESULTS=()

run_suite() {
  local name="$1"
  local script="$2"

  echo ""
  echo ""
  if bash "$SCRIPT_DIR/$script"; then
    SUITE_RESULTS+=("${GREEN}PASS${NC}  $name")
  else
    SUITE_RESULTS+=("${RED}FAIL${NC}  $name")
    TOTAL_FAILED=$((TOTAL_FAILED + 1))
  fi
  TOTAL_PASSED=$((TOTAL_PASSED + 1))
}

main() {
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Cloud Foundry Extension — Full Test Suite"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

  run_suite "Stop App Action"    "test_stop.sh"
  run_suite "Restart App Action" "test_restart.sh"
  run_suite "Check App State"    "test_check.sh"

  echo ""
  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo -e "  ${BOLD}Suite Results${NC}"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  for result in "${SUITE_RESULTS[@]}"; do
    echo -e "  $result"
  done
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

  if [ "$TOTAL_FAILED" -eq 0 ]; then
    echo -e "  ${GREEN}${BOLD}All $TOTAL_PASSED suites passed${NC}"
  else
    echo -e "  ${RED}${BOLD}$TOTAL_FAILED of $TOTAL_PASSED suites failed${NC}"
  fi
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

  [ "$TOTAL_FAILED" -eq 0 ]
}

main "$@"
