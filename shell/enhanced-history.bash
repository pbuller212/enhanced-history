# enhanced-history.bash — bash-preexec integration for enhanced-history
#
# Add to ~/.bashrc:
#   source /path/to/bash-preexec.sh
#   source /path/to/enhanced-history.bash

# Generate a session UUID once per shell; skip if already set so re-sourcing
# ~/.bashrc mid-session does not start a new logical session.
if [[ -z "${EH_SESSION:-}" ]]; then
    EH_SESSION="$(uuidgen)"
    export EH_SESSION
fi

# Only wire up preexec when the hist binary is available.  This guard ensures
# the shell is never broken by a missing or not-yet-installed binary.
if command -v hist > /dev/null 2>&1; then
    preexec() {
        hist record --cmd "$1" --dir "$PWD" --session "$EH_SESSION"
    }
fi
