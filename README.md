# chomp

*Burn every free token you're given.*

chomp is a task queue for AI agents. You toss in tasks — any tasks — and agents on free-token platforms chew through them until the budget is gone.

Platforms give you free tokens. They reset if you don't use them. That's waste. chomp fixes that.

## How it works

```
You:     chomp add "rewrite the auth module"
         chomp add "whats the capital of france"
         chomp add "read these docs and write a PRD in chinese"

chomp:   picks next task → dispatches to available platform → tracks result
         picks next task → dispatches to available platform → tracks result
         ...
         (until backlog empty or tokens gone)
```

## Platforms

| Platform | Dispatch method | Notes |
|----------|----------------|-------|
| **exe.dev** | Shelley worker loops | Long-running, great for code tasks |
| **OpenCode** | CLI (`opencode`) | Fast, good free tiers from providers |

More adapters = more mouths.

## Usage

```bash
# Feed it
chomp add "refactor the billing module"
chomp add "what are the best practices for durable objects"
chomp add "write tests for /api/session" --dir /home/exedev/myfilepath-new

# Run it
chomp run              # dispatch next task to best available platform
chomp run --all        # dispatch to ALL platforms in parallel

# Watch it
chomp status           # what's running, what's queued, tokens spent
chomp log              # history of completed tasks

# Manage it
chomp list             # see the backlog
chomp done <id>        # mark complete (agents do this automatically)
chomp drop <id>        # remove a task
```

## Task format

A task is a string. That's it.

Optionally, a task can have a working directory (`--dir`) for filesystem tasks. Everything else is just the prompt.

```typescript
interface Task {
  id: string;
  prompt: string;
  dir?: string;           // working directory, if relevant
  status: "queued" | "active" | "done" | "failed";
  created: string;
  result?: string;        // what the agent produced
  platform?: string;      // which platform ran it
  tokens?: number;        // how many tokens it ate
}
```

## Agent protocol

When chomp dispatches a task, the agent receives:

```
CHOMP TASK #7: rewrite the auth module
DIR: /home/exedev/myfilepath-new

Do the work. When done: chomp done 7 "summary of what you did"
If you hit context limit: chomp handoff 7 "where you left off"
```

That's it. The agent works normally. Reports back. chomp advances.

## Platform adapters

An adapter is a shell script that knows how to start a session on a platform:

```bash
# adapters/exedev.sh
# Starts a Shelley worker loop with the given task
worker start "chomp-$TASK_ID" --task "$TASK_PROMPT" --dir "$TASK_DIR"
```

```bash
# adapters/opencode.sh  
# Starts an OpenCode session with the given task
opencode --message "$TASK_PROMPT" --dir "$TASK_DIR"
```

Adding a platform = writing a 5-line shell script.

## Structure

```
chomp/
├── bin/chomp             # CLI (bash)
├── state.json            # tasks + history
├── adapters/
│   ├── exedev.sh         # exe.dev dispatch
│   └── opencode.sh       # opencode dispatch
└── README.md
```

No database. No server. One JSON file and some shell scripts.

## Design

1. **Tasks are strings** — no schema, no metadata hierarchy
2. **Agents are the user** — built for agents to consume, humans just feed
3. **Greedy** — if tokens exist, spend them
4. **Dumb** — a queue and some shell scripts. That's the whole thing
5. **Observable** — always know what's queued, running, spent

---

*If they give you tokens, spend them.*
