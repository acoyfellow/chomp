# chomp — Agent Instructions

## What this is

A task queue for AI agents. Feed tasks in, agents chew through them. Dashboard to watch.

## Repo structure

```
chomp/
├─ bin/chomp             # CLI (bash + jq)
├─ adapters/             # Platform dispatch scripts
│  ├─ exedev.sh          # exe.dev / Shelley worker loops
│  └─ opencode.sh        # OpenCode CLI
├─ dashboard/            # Web dashboard (static HTML/CSS/JS)
│  ├─ index.html
│  ├─ style.css
│  ├─ data.js            # Agent/router config + API fetch
│  └─ app.js             # Render logic, sheets, pickers
├─ server.go             # Go server: static files + /api/state
├─ chomp-dashboard.service  # systemd unit
├─ state.json            # Task state (gitignored, runtime only)
└─ README.md
```

## Key decisions

- **state.json** is the single source of truth. CLI writes it, server reads it, dashboard displays it.
- **No database.** JSON file + jq. Intentionally simple.
- **Adapters are shell scripts.** Two functions: `available` and `run`. Adding a platform = one new .sh file.
- **Dashboard is static.** Go server only serves files and one API endpoint. All logic is client-side JS.
- **Mobile-first design.** Single column, bottom sheets, tap targets. Light mode default, dark mode toggle.

## Agents

Three agents in the catalog:
- **Shelley** (exe.dev worker loops)
- **OpenCode** (CLI)
- **Pi** (planned)

## Routers

Three AI routers:
- **Cloudflare AI Gateway**
- **OpenCode Zen**
- **OpenRouter**

## Design language

- Font: **Sora** (Bold 700, Regular 400)
- Light mode default, dark mode via toggle
- Progress = leveling up (XP bars, progress rings)
- Completion = unlocking (check animation, shimmer on done cards)
- Peak moments = title card flash (full-screen unlock animation when all gates pass)
- Mobile-first: single column, bottom sheets for detail/picker
- Stripe's precision + PS5 boot sequence + luxury car door weight

## Build & run

```bash
# Build server
go build -o chomp-server server.go

# Run locally
./chomp-server  # serves on :8001

# Install as service
sudo cp chomp-dashboard.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable chomp-dashboard
sudo systemctl start chomp-dashboard
```

## Adding tasks

```bash
# Ensure bin/chomp is on PATH
ln -sf /home/exedev/chomp/bin/chomp ~/bin/chomp

chomp add "your task here"
chomp add "another task" --dir /path/to/project
chomp list
chomp run
```

## What to work on next

1. **Gateproof integration** — tasks with gate files, verification loops
2. **Session tracking** — multiple agent sessions per task with handoff context
3. **Agent/model stamping** — git commit trailers (`Agent: shelley/claude-sonnet-4`)
4. **Pi adapter** — new adapter script for Pi agent
5. **Dashboard dispatch** — wire picker buttons to actually call `chomp run --agent X --router Y`
6. **Token budget** — per-task and global token limits with auto-kill on exceed

## Rules

- Don't commit state.json or the compiled binary
- Don't add mock data to data.js — it fetches from /api/state
- Keep the dashboard mobile-first — test at 390px before merging
- Keep adapters as simple shell scripts
- Commit messages should be descriptive (see git log for style)
