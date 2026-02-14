#!/usr/bin/env bash
# chomp adapter: Claude Code (claude CLI)
#
# Uses Anthropic's `claude` CLI. Piggybacks on Claude Max subscription
# or uses ANTHROPIC_API_KEY for direct API billing.

case "${1:-}" in
  available)
    command -v claude &>/dev/null
    ;;
  run)
    AGENT_TASK="CHOMP TASK #$TASK_ID: $TASK_PROMPT

DIR: $TASK_DIR

Do the work. When done, run: $CHOMP_BIN done $TASK_ID \"summary of what you did\"
If you hit context limit, run: $CHOMP_BIN handoff $TASK_ID \"where you left off\""

    cd "${TASK_DIR:-.}"
    claude --print "$AGENT_TASK"

    echo "Claude Code session for task #$TASK_ID complete."
    ;;
  *)
    echo "Usage: $0 {available|run}"
    exit 1
    ;;
esac
