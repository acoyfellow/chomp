#!/usr/bin/env bash
# chomp adapter: Cursor (agent CLI)
#
# Uses the Cursor "agent" CLI which piggybacks on your Cursor Pro/Business
# subscription for model access. Flat-rate sub = effectively free tokens.
#
# Install: Cursor > Settings > Install CLI (adds `agent` to PATH)
# Auth: `agent login` or set CURSOR_API_KEY

case "${1:-}" in
  available)
    command -v agent &>/dev/null
    ;;
  run)
    AGENT_TASK="CHOMP TASK #$TASK_ID: $TASK_PROMPT

DIR: $TASK_DIR

Do the work. When done, run: $CHOMP_BIN done $TASK_ID \"summary of what you did\"
If you hit context limit, run: $CHOMP_BIN handoff $TASK_ID \"where you left off\""

    cd "${TASK_DIR:-.}"
    agent --print --mode ask "$AGENT_TASK"

    echo "Cursor session for task #$TASK_ID complete."
    ;;
  *)
    echo "Usage: $0 {available|run}"
    exit 1
    ;;
esac
