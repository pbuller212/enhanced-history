#!/usr/bin/env bash
# Tests for shell/check-setup.sh
# Run with: bash shell/test_check_setup.sh

set -uo pipefail

PASS=0
FAIL=0
SCRIPT="$(cd "$(dirname "$0")" && pwd)/check-setup.sh"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

pass() { echo "PASS: $1"; PASS=$((PASS + 1)); }
fail() { echo "FAIL: $1"; FAIL=$((FAIL + 1)); }

# Build a throwaway hist binary once, reused by tests that need one on PATH.
FAKE_HOME_BASE="$(mktemp -d)"
trap 'rm -rf "$FAKE_HOME_BASE"' EXIT

BIN_DIR="$FAKE_HOME_BASE/bin"
mkdir -p "$BIN_DIR"
cat > "$BIN_DIR/hist" <<'EOF'
#!/usr/bin/env bash
if [[ "$1" == "--version" ]]; then
    echo "test"
    exit 0
fi
exit 0
EOF
chmod +x "$BIN_DIR/hist"

new_fake_home() {
    local dir
    dir="$(mktemp -d)"
    echo "$dir"
}

# ---------------------------------------------------------------------------
# Test 1: fully-configured environment reports success and exits 0
# ---------------------------------------------------------------------------
home="$(new_fake_home)"
touch "$home/.bash-preexec.sh"
cp "$REPO_ROOT/shell/enhanced-history.bash" "$home/.enhanced-history.sh"
{
    echo "[[ -f $home/.bash-preexec.sh ]] && source $home/.bash-preexec.sh"
    echo "[[ -f $home/.enhanced-history.sh ]] && source $home/.enhanced-history.sh"
} > "$home/.bashrc"
mkdir -p "$home/.local/share/enhanced-history"

out="$(HOME="$home" PATH="$BIN_DIR:/usr/bin:/bin" "$SCRIPT" < /dev/null 2>&1)"
code=$?
if [[ "$code" -eq 0 ]] && echo "$out" | grep -q "All checks passed."; then
    pass "fully-configured environment exits 0"
else
    fail "fully-configured environment did not pass (exit=$code)"
    echo "$out"
fi
rm -rf "$home"

# ---------------------------------------------------------------------------
# Test 2: missing hist binary is reported and script exits non-zero
# ---------------------------------------------------------------------------
home="$(new_fake_home)"
out="$(HOME="$home" PATH="/usr/bin:/bin" "$SCRIPT" < /dev/null 2>&1)"
code=$?
if [[ "$code" -ne 0 ]] && echo "$out" | grep -q "hist binary not found on PATH"; then
    pass "missing hist binary is detected and script exits non-zero"
else
    fail "missing hist binary was not detected correctly (exit=$code)"
    echo "$out"
fi
rm -rf "$home"

# ---------------------------------------------------------------------------
# Test 3: non-interactive run never prompts and never mutates ~/.bashrc
# ---------------------------------------------------------------------------
home="$(new_fake_home)"
touch "$home/.bashrc"
before="$(cat "$home/.bashrc")"
HOME="$home" PATH="$BIN_DIR:/usr/bin:/bin" "$SCRIPT" < /dev/null > /dev/null 2>&1
after="$(cat "$home/.bashrc")"
if [[ "$before" == "$after" ]]; then
    pass "non-interactive run does not modify ~/.bashrc without confirmation"
else
    fail "non-interactive run modified ~/.bashrc without confirmation"
fi
rm -rf "$home"

# ---------------------------------------------------------------------------
# Test 4: missing integration file is fixed when the user confirms
# ---------------------------------------------------------------------------
if command -v script > /dev/null 2>&1; then
    home="$(new_fake_home)"
    touch "$home/.bash-preexec.sh" "$home/.bashrc"
    printf 'y\ny\n' | HOME="$home" PATH="$BIN_DIR:/usr/bin:/bin" \
        script -qec "$SCRIPT" /dev/null > /dev/null 2>&1
    if [[ -f "$home/.enhanced-history.sh" ]]; then
        pass "confirming the fix copies enhanced-history.bash into place"
    else
        fail "confirming the fix did not create ~/.enhanced-history.sh"
    fi
    rm -rf "$home"
else
    echo "SKIP: 'script' command not available, skipping interactive fix test"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "Results: $PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]] && exit 0 || exit 1
