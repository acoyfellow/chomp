#!/usr/bin/env bash
# chomp adapter: exe.dev (Shelley worker loops)
#
# Requires: ~/bin/worker (exe.dev worker loop system)

case "${1:-}" in
  available)
    # Check if worker CLI exists
    [ -x "$HOME/bin/worker" ]
    ;;
  run)
    # Build the task prompt with chomp protocol
    AGENT_TASK="CHOMP TASK #$TASK_ID: $TASK_PROMPT

DIR: $TASK_DIR

Do the work. When done, run: $CHOMP_BIN done $TASK_ID \"summary of what you did\"
If you hit context limit, run: $CHOMP_BIN handoff $TASK_ID \"where you left off\""

    # Start a worker loop
    "$HOME/bin/worker" start "chomp-$TASK_ID" \
      --task "$AGENT_TASK" \
      --dir "$TASK_DIR" \
      --max-sessions 3

    echo "Worker loop chomp-$TASK_ID started."
    ;;
  *)
    echo "Usage: $0 {available|run}"
    exit 1
    ;;
esac
