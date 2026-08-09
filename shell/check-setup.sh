#!/usr/bin/env bash
# check-setup.sh — verify enhanced-history is fully installed and wired up.
#
# Run after `make install` to check for the manual steps make install does
# not do for you (copying the shell integration file, sourcing it and
# bash-preexec from ~/.bashrc, and having uuidgen available). Prompts before
# making any change and exits non-zero if a problem is found and not fixed.
#
# Usage: shell/check-setup.sh

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BASHRC="$HOME/.bashrc"
PREEXEC_FILE="$HOME/.bash-preexec.sh"
INTEGRATION_FILE="$HOME/.enhanced-history.sh"
INTEGRATION_SRC="$REPO_ROOT/shell/enhanced-history.bash"

PROBLEMS=0
FIXED=0

ok()   { echo "  [OK]   $1"; }
fail() { echo "  [FAIL] $1"; PROBLEMS=$((PROBLEMS + 1)); }
info() { echo "         $1"; }

# Prompt the user before making a change. Defaults to No on empty/non-y input
# or when stdin is not a terminal (non-interactive runs never auto-fix).
confirm() {
    local prompt="$1"
    if [[ ! -t 0 ]]; then
        return 1
    fi
    local reply
    read -r -p "         Fix: $prompt [y/N] " reply
    [[ "$reply" =~ ^[Yy]$ ]]
}

echo "Checking enhanced-history setup..."

# ---------------------------------------------------------------------------
# 1. Binary + PATH
# ---------------------------------------------------------------------------
if command -v hist > /dev/null 2>&1; then
    version="$(hist --version 2>/dev/null)"
    if [[ -n "$version" ]]; then
        ok "hist binary found on PATH ($version)"
    else
        fail "hist found on PATH but 'hist --version' failed"
    fi
else
    fail "hist binary not found on PATH"
    info "Build and install it: make build && make install"
    info "Then ensure \$HOME/bin is on PATH."
fi

# ---------------------------------------------------------------------------
# 2. Required tools: uuidgen
# ---------------------------------------------------------------------------
if command -v uuidgen > /dev/null 2>&1; then
    ok "uuidgen found on PATH"
else
    fail "uuidgen not found on PATH"

    pkg=""
    install_cmd=""
    case "$(uname -s)" in
        Darwin)
            info "uuidgen ships with macOS by default; check your PATH."
            ;;
        Linux)
            if command -v apt-get > /dev/null 2>&1; then
                pkg="uuid-runtime"
                install_cmd="sudo apt-get install -y uuid-runtime"
            elif command -v dnf > /dev/null 2>&1; then
                pkg="util-linux"
                install_cmd="sudo dnf install -y util-linux"
            elif command -v yum > /dev/null 2>&1; then
                pkg="util-linux"
                install_cmd="sudo yum install -y util-linux"
            elif command -v pacman > /dev/null 2>&1; then
                pkg="util-linux"
                install_cmd="sudo pacman -S --noconfirm util-linux"
            elif command -v zypper > /dev/null 2>&1; then
                pkg="util-linux"
                install_cmd="sudo zypper install -y util-linux"
            fi
            ;;
    esac

    if [[ -n "$install_cmd" ]]; then
        if confirm "install $pkg via '$install_cmd'?"; then
            if eval "$install_cmd"; then
                if command -v uuidgen > /dev/null 2>&1; then
                    echo "         uuidgen installed."
                    FIXED=$((FIXED + 1))
                    PROBLEMS=$((PROBLEMS - 1))
                else
                    info "Install command succeeded but uuidgen is still not on PATH."
                fi
            else
                info "Install command failed; install $pkg manually."
            fi
        fi
    elif [[ -n "$pkg" ]]; then
        info "Install the '$pkg' package for your distribution."
    fi
fi

# ---------------------------------------------------------------------------
# 3. Shell integration files present
# ---------------------------------------------------------------------------
if [[ -f "$PREEXEC_FILE" ]]; then
    ok "$PREEXEC_FILE found"
else
    fail "$PREEXEC_FILE not found"
    info "Install bash-preexec: https://github.com/rcaloras/bash-preexec"
fi

if [[ -f "$INTEGRATION_FILE" ]]; then
    ok "$INTEGRATION_FILE found"
else
    fail "$INTEGRATION_FILE not found"
    if [[ -f "$INTEGRATION_SRC" ]]; then
        if confirm "copy shell/enhanced-history.bash to $INTEGRATION_FILE?"; then
            if cp "$INTEGRATION_SRC" "$INTEGRATION_FILE"; then
                echo "         Copied."
                FIXED=$((FIXED + 1))
                PROBLEMS=$((PROBLEMS - 1))
            else
                info "Copy failed."
            fi
        fi
    else
        info "Expected source file not found: $INTEGRATION_SRC"
    fi
fi

# ---------------------------------------------------------------------------
# 4. ~/.bashrc sources both files
# ---------------------------------------------------------------------------
sources_preexec=0
sources_integration=0
if [[ -f "$BASHRC" ]]; then
    # Accept either the $HOME/... form or the ~/... shorthand some users write by hand.
    grep -Fq "$PREEXEC_FILE" "$BASHRC" 2>/dev/null && sources_preexec=1
    grep -Fq "${PREEXEC_FILE/#$HOME/\~}" "$BASHRC" 2>/dev/null && sources_preexec=1
    grep -Fq "$INTEGRATION_FILE" "$BASHRC" 2>/dev/null && sources_integration=1
    grep -Fq "${INTEGRATION_FILE/#$HOME/\~}" "$BASHRC" 2>/dev/null && sources_integration=1
fi

if [[ "$sources_preexec" -eq 1 && "$sources_integration" -eq 1 ]]; then
    ok "$BASHRC sources $PREEXEC_FILE and $INTEGRATION_FILE"
else
    fail "$BASHRC does not source both integration files"
    lines=""
    [[ "$sources_preexec" -eq 0 ]] && lines+=$'\n'"[[ -f $PREEXEC_FILE ]] && source $PREEXEC_FILE"
    [[ "$sources_integration" -eq 0 ]] && lines+=$'\n'"[[ -f $INTEGRATION_FILE ]] && source $INTEGRATION_FILE"

    if confirm "append the missing source line(s) to $BASHRC?"; then
        {
            echo ""
            echo "# Added by enhanced-history/shell/check-setup.sh"
            echo "$lines"
        } >> "$BASHRC"
        echo "         Appended to $BASHRC."
        FIXED=$((FIXED + 1))
        PROBLEMS=$((PROBLEMS - 1))
    else
        info "Add the following to $BASHRC:$lines"
    fi
fi

# ---------------------------------------------------------------------------
# 5. Live hook: preexec defined and EH_SESSION set in an interactive shell
# ---------------------------------------------------------------------------
live_check_output="$(bash -i -c 'declare -f preexec > /dev/null 2>&1 && echo "preexec=yes" || echo "preexec=no"; echo "session=${EH_SESSION:-}"' 2>/dev/null)"
live_preexec="$(echo "$live_check_output" | /bin/grep '^preexec=' | cut -d= -f2)"
live_session="$(echo "$live_check_output" | /bin/grep '^session=' | cut -d= -f2-)"

if [[ "$live_preexec" == "yes" && -n "$live_session" ]]; then
    ok "interactive shell defines preexec and sets EH_SESSION"
else
    fail "interactive shell does not have a working preexec hook / EH_SESSION"
    info "This usually means \$BASHRC is missing the source lines above, or hist is not on PATH."
fi

# ---------------------------------------------------------------------------
# 6. Database directory writable
# ---------------------------------------------------------------------------
db_path="${ENHANCED_HISTORY_DB:-$HOME/.local/share/enhanced-history/hist.db}"
db_dir="$(dirname "$db_path")"

if [[ -d "$db_dir" && -w "$db_dir" ]]; then
    ok "database writable at $db_path"
elif [[ ! -e "$db_dir" ]]; then
    fail "database directory does not exist: $db_dir"
    info "It is created automatically the first time a command is recorded."
else
    fail "database directory is not writable: $db_dir"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
if [[ "$PROBLEMS" -le 0 && "$FIXED" -eq 0 ]]; then
    echo "All checks passed."
    exit 0
fi

if [[ "$FIXED" -gt 0 ]]; then
    echo "$FIXED problem(s) found and fixed. Restart your shell (or 'source ~/.bashrc') to pick up the change."
fi

if [[ "$PROBLEMS" -gt 0 ]]; then
    echo "$PROBLEMS problem(s) found."
    exit 1
fi

exit 0
