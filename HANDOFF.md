# Chomp Handoff — 2026-02-11

## What is chomp
Task queue for AI agents. Feed tasks in, agents chew through them. Dashboard to watch. **The design spec is final** — don't redesign, just implement properly.

## Stack (just rewritten)
- **Go `html/template`** — server-rendered, embedded via `go:embed`
- **HTMX** — declarative interactivity, auto-polling (balance 60s, tasks 3s)
- **Tailwind CSS** — standalone CLI (no Node), utility classes only
- **Single binary** — templates + CSS embedded, Docker deploys to port 8000→8001

## Current state
- ✅ 38 tests passing (`go test -count=1 -run . server_test.go server.go`)
- ✅ Docker running on port 8000: `docker ps | grep chomp`
- ✅ Balance card shows **$3.00/day** (daily renewable token budget, not bank account)
- ✅ Settings drawer with API key CRUD (add/edit/delete, persisted to state/keys.json)
- ✅ Custom agent support (BYO via POST /api/config/agents)
- ✅ Task CRUD works with both JSON and form-encoded bodies (HTMX sends form-encoded)
- ✅ All layout in document flow — zero `position:fixed/absolute` for layout elements

## Key files
```
server.go              # ALL server code (~1200 lines) - API + template handlers
server_test.go         # 38 tests covering every endpoint + partial + helper
templates/layout.html  # Base HTML (Sora font, HTMX, Tailwind)
templates/page.html    # App shell (topbar, balance, tabs, content, sheets, JS)
templates/partials/    # HTMX fragments: balance, tasks, detail, settings, create
static/input.css       # Tailwind input
static/style.css       # Tailwind output (regenerate: tailwindcss -i static/input.css -o static/style.css --minify)
tailwind.config.js     # Sora font, custom colors, dark mode
Dockerfile             # Multi-stage Go build, templates embedded at compile time
state/                 # Runtime: state.json, keys.json, agents.json (Docker volume)
```

## Build & deploy cycle
```bash
cd /home/exedev/chomp
tailwindcss -i static/input.css -o static/style.css --minify  # if templates changed
go build -o chomp-server server.go                             # embeds templates+css
go test -count=1 -run . server_test.go server.go               # must pass
docker stop chomp; docker rm chomp
docker build -t chomp .
docker run -d --name chomp --restart unless-stopped -p 8000:8001 -v /home/exedev/chomp/state:/app/state chomp
```

## API endpoints
| Method | Path | Purpose |
|--------|------|--------|
| GET | / | Main page (server-rendered) |
| GET | /static/style.css | Embedded Tailwind CSS |
| GET | /partials/balance | Balance card fragment (HTMX) |
| GET | /partials/tasks?tab=active\|completed | Task list fragment (HTMX) |
| GET | /partials/detail/{id} | Task detail fragment (HTMX) |
| GET | /partials/settings | Settings fragment (HTMX) |
| GET | /partials/create | Create task form (HTMX) |
| GET | /api/state | Raw JSON state |
| GET | /api/config | Config (agents + routers) |
| GET | /api/config/agents | Merged agent list |
| POST | /api/config/agents | Add custom agent |
| DELETE | /api/config/agents | Remove custom agent |
| POST | /api/config/keys | Set/delete API key |
| GET | /api/balance | Daily token budget |
| POST | /api/tasks | Create task |
| POST | /api/tasks/run | Start task |
| POST | /api/tasks/done | Complete task |
| POST | /api/tasks/delete | Delete task |

## What to work on next
1. ~~**Delete waiting tasks doesn't refresh UI**~~ ✅ Fixed — server sends `HX-Trigger: refreshTasks`, content div listens.
2. ~~**Old `dashboard/` directory**~~ ✅ Deleted.
3. ~~**Settings: agent install/BYO UI**~~ ✅ Added — install form + remove button for custom agents.
4. ~~**Create task wizard**~~ ✅ Rebuilt as 4-step HTMX wizard (prompt→agent→model→review).
5. **Session tracking** — detail sheet says "No session history yet". Wire to real agent session data.
6. **Gateproof integration** — tasks with gate files, verification loops.

## Critical rules
- **Run tests before committing**: `go test -count=1 -run . server_test.go server.go`
- **Regenerate CSS if templates change**: `tailwindcss -i static/input.css -o static/style.css --minify`
- **Rebuild Go binary after CSS regen** (CSS is embedded via go:embed)
- **No `position:fixed/absolute` for layout** — everything in document flow
- **No `confirm()` dialogs** — they kill the headless browser
- **Balance is daily renewable**, not cumulative. Show "/day" always.
- **HTMX sends form-encoded** with `hx-vals` — `decodeBody()` helper handles both JSON and form
- Read AGENTS.md for full project context
