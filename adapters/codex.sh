#!/usr/bin/env bash
# chomp adapter: OpenAI Codex CLI
#
# Uses OpenAI's `codex` CLI. Piggybacks on ChatGPT Pro subscription
# or uses OPENAI_API_KEY for direct billing.

case "${1:-}" in
  available)
    command -v codex &>/dev/null
    ;;
  run)
    AGENT_TASK="CHOMP TASK #$TASK_ID: $TASK_PROMPT

DIR: $TASK_DIR

Do the work. When done, run: $CHOMP_BIN done $TASK_ID \"summary of what you did\"
If you hit context limit, run: $CHOMP_BIN handoff $TASK_ID \"where you left off\""

    cd "${TASK_DIR:-.}"
    codex --quiet "$AGENT_TASK"

    echo "Codex session for task #$TASK_ID complete."
    ;;
  *)
    echo "Usage: $0 {available|run}"
    exit 1
    ;;
esac
