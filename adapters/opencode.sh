#!/usr/bin/env bash
# chomp adapter: OpenCode
#
# Requires: opencode CLI (https://opencode.ai)

case "${1:-}" in
  available)
    command -v opencode &>/dev/null
    ;;
  run)
    AGENT_TASK="CHOMP TASK #$TASK_ID: $TASK_PROMPT

Do the work. When done, run: $CHOMP_BIN done $TASK_ID \"summary of what you did\"
If you hit context limit, run: $CHOMP_BIN handoff $TASK_ID \"where you left off\""

    cd "${TASK_DIR:-.}"
    opencode --message "$AGENT_TASK"

    echo "OpenCode session for task #$TASK_ID complete."
    ;;
  *)
    echo "Usage: $0 {available|run}"
    exit 1
    ;;
esac
