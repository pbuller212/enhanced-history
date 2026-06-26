#!/usr/bin/env bash
# Tests for shell/enhanced-history.bash
# Run with: bash shell/test_shell.sh

set -euo pipefail

PASS=0
FAIL=0
SCRIPT="$(cd "$(dirname "$0")" && pwd)/enhanced-history.bash"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

pass() { echo "PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "FAIL: $1"; FAIL=$((FAIL + 1)); }

# ---------------------------------------------------------------------------
# Test 1: Sourcing sets EH_SESSION
# ---------------------------------------------------------------------------
result=$(bash --norc --noprofile -c "source '$SCRIPT'; echo \"\$EH_SESSION\"" 2>/dev/null)
if [[ -n "$result" ]]; then
    pass "sourcing sets EH_SESSION (got: $result)"
else
    fail "EH_SESSION was empty after sourcing"
fi

# ---------------------------------------------------------------------------
# Test 2: EH_SESSION looks like a UUID (8-4-4-4-12 hex)
# ---------------------------------------------------------------------------
if [[ "$result" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]]; then
    pass "EH_SESSION is a valid UUID"
else
    fail "EH_SESSION does not look like a UUID: '$result'"
fi

# ---------------------------------------------------------------------------
# Test 3: preexec is defined as a function after sourcing (with hist on PATH)
# ---------------------------------------------------------------------------
fn_defined=$(bash --norc --noprofile -c "PATH='$REPO_ROOT:\$PATH' source '$SCRIPT'; declare -f preexec > /dev/null 2>&1 && echo yes || echo no" 2>/dev/null)
if [[ "$fn_defined" == "yes" ]]; then
    pass "preexec is defined as a function"
else
    fail "preexec is not defined as a function"
fi

# ---------------------------------------------------------------------------
# Test 4: Shell is not broken when hist is not on PATH
# ---------------------------------------------------------------------------
no_hist_exit=$(bash --norc --noprofile -c "PATH=/usr/bin:/bin source '$SCRIPT'; echo ok" 2>/dev/null)
if [[ "$no_hist_exit" == "ok" ]]; then
    pass "shell survives sourcing when hist is not on PATH"
else
    fail "shell broke when hist was not on PATH"
fi

# ---------------------------------------------------------------------------
# Test 5: Re-sourcing does not reset EH_SESSION
# ---------------------------------------------------------------------------
reuse=$(bash --norc --noprofile -c "
    source '$SCRIPT'
    first=\$EH_SESSION
    source '$SCRIPT'
    second=\$EH_SESSION
    [[ \"\$first\" == \"\$second\" ]] && echo same || echo different
" 2>/dev/null)
if [[ "$reuse" == "same" ]]; then
    pass "re-sourcing does not reset EH_SESSION"
else
    fail "re-sourcing changed EH_SESSION"
fi

# ---------------------------------------------------------------------------
# Test 6: preexec is NOT defined when hist is absent (guard active)
# ---------------------------------------------------------------------------
no_fn=$(bash --norc --noprofile -c "PATH=/usr/bin:/bin source '$SCRIPT'; declare -f preexec > /dev/null 2>&1 && echo yes || echo no" 2>/dev/null)
if [[ "$no_fn" == "no" ]]; then
    pass "preexec is not defined when hist is absent (guard works)"
else
    fail "preexec was defined even though hist is absent"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "Results: $PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]] && exit 0 || exit 1
