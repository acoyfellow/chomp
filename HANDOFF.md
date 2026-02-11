# Chomp Handoff — 2026-02-11

## What is chomp

Task queue for AI agents. Feed tasks in, agents chew through them. Dashboard to watch. **The design spec is final** — don't redesign, just implement properly.

## Stack

- **Go `html/template`** — server-rendered, embedded via `go:embed`
- **HTMX** — declarative interactivity, auto-polling, `HX-Trigger: refreshTasks` for instant UI updates
- **Tailwind CSS** — standalone CLI (no Node), utility classes only
- **Single binary** — templates + CSS embedded, Docker deploys to port 8000→8001

## Current state

- ✅ 59 tests passing (`go test -count=1 -run . server_test.go server.go`)
- ✅ Docker running on port 8000
- ✅ Platform status board (real — no fake dollars, no theater)
- ✅ Settings drawer with API key CRUD + agent install/remove UI
- ✅ 4-step create wizard (prompt → agent → model → review)
- ✅ Session tracking with handoff chaining (activity timeline in detail sheet)
- ✅ PS5-style boot screen (wordmark fade, gold bar fill, app reveal)
- ✅ Agent/model git commit trailers via `chomp done`
- ✅ Per-task budget flag (300k token soft cap)
- ✅ All metrics real: LIVE (active tasks), TASKS (total), BURNED (sum of tokens)
- ✅ All task mutations send `HX-Trigger: refreshTasks` — instant UI refresh

## Key files

```
server.go              # ALL server code (~1500 lines) - API + template handlers
server_test.go         # 59 tests
templates/layout.html  # Base HTML (Sora font, HTMX, Tailwind)
templates/page.html    # App shell (boot screen, topbar, tabs, sheets, JS)
templates/partials/    # HTMX fragments: balance, tasks, detail, settings, create
static/input.css       # Tailwind input (includes boot keyframes)
static/style.css       # Tailwind output
bin/chomp              # CLI (bash + jq)
adapters/              # Shell scripts: exedev.sh, opencode.sh
Dockerfile             # Multi-stage Go build
state/                 # Runtime: state.json, keys.json, agents.json
```

## Data model

```go
type Session struct {
    ID, Agent, Model, StartedAt, EndedAt, Result, Summary string
    Tokens int
}
type Task struct {
    ID, Prompt, Dir, Status, Created, Result, Platform, Model string
    Tokens int; BudgetExceeded bool; Sessions []Session
}
```

Statuses: `queued` → `active` → `done`/`failed`. Handoff: `active` → `queued` (closes session).

## API endpoints

| Method | Path | Purpose |
|--------|------|--------|
| GET | /partials/balance | Platform status card |
| GET | /partials/tasks?tab=active\|completed | Task list |
| GET | /partials/detail/{id} | Task detail + session timeline |
| GET | /partials/settings | Settings + agent install |
| GET | /partials/create?step=1-4 | Wizard steps |
| GET | /api/platforms | Real platform statuses |
| POST | /api/tasks | Create task |
| POST | /api/tasks/run | Start task (creates session) |
| POST | /api/tasks/update | Update tokens |
| POST | /api/tasks/done | Complete (closes session) |
| POST | /api/tasks/handoff | Close session, re-queue |
| POST | /api/tasks/delete | Delete task |
| POST | /api/config/agents | Add/delete custom agent |
| POST | /api/config/keys | Set/delete API key |

## Build & deploy

```bash
cd /home/exedev/chomp
tailwindcss -i static/input.css -o static/style.css --minify
go build -o chomp-server server.go
go test -count=1 -run . server_test.go server.go
docker stop chomp; docker rm chomp
docker build -t chomp .
docker run -d --name chomp --restart unless-stopped -p 8000:8001 -v /home/exedev/chomp/state:/app/state chomp
docker image prune -f
```

---

## NEXT: Cloudflare Sandbox Dispatch

**This is the priority. Full Cloudflare account access confirmed.**

### What we're building

Tap ▶ on a task → Cloudflare Sandbox spins up → AI agent runs inside → live terminal in dashboard → agent calls chomp API when done.

### Architecture

```
Dashboard (Go, exe.dev :8000)       Cloudflare Edge
┌──────────────────────┐    ┌──────────────────────────────────┐
│ Go server            │    │ Worker (TypeScript)              │
│  POST /api/tasks/run │───>│  /dispatch → getSandbox()        │
│                      │    │  /ws/terminal → sandbox.terminal()│
│ templates/page.html  │    │  /status → sandbox.exec() ping   │
│  └─ xterm.js widget  │<──>│  WebSocket proxy                 │
│                      │    │                                  │
│ state.json           │    │ Sandbox (Ubuntu container)        │
│                      │    │  ├─ agent (shelley/pi/opencode)  │
│                      │    │  ├─ git repo (cloned)            │
│                      │    │  ├─ chomp CLI (calls back)       │
│                      │    │  └─ keepAlive: true              │
└──────────────────────┘    └──────────────────────────────────┘
```

### Sandbox SDK key facts

- **Container:** Isolated Ubuntu, full Linux env (Node, Bun, git, curl built-in)
- **Base images:** `cloudflare/sandbox:0.7.0` (default), `-python`, `-opencode`
- **Custom Dockerfile:** Extend base, install pi/shelley/anything
- **`exec(cmd)`:** Run commands, stream output, set env/cwd/timeout
- **`terminal(request)`:** WebSocket → xterm.js live terminal in browser
- **`gitCheckout(url)`:** Clone repos, supports private (token in URL), branches, shallow
- **`keepAlive: true`:** Auto-heartbeat every 30s, container stays up
- **`startProcess(cmd)`:** Background processes
- **`destroy()`:** Kill container immediately
- **Sleeps after 10min idle** — all state lost. Use R2 for persistence.
- **Workers Paid required.** WebSocket transport avoids subrequest limits.

### Implementation phases

**Phase 1: Worker scaffold (`chomp-sandbox/`) ✅ DONE**
- New Worker project with Sandbox binding
- Custom Dockerfile: base image + `pi` + `chomp` CLI
- Endpoints: `/dispatch`, `/status/:id`, `/kill/:id` (terminal deferred to Phase 3)
- Deployed to https://chomp-sandbox.coy.workers.dev
- All endpoints tested and working

**Phase 2: Go server integration**
- `apiRunTask` → POST to Worker `/dispatch` with task details
- Store `sandbox_id` on Task/Session
- Agent in sandbox calls back: `curl $CHOMP_API/api/tasks/update`

**Phase 3: Live terminal**
- xterm.js in detail sheet (CDN: xterm.js + `@cloudflare/sandbox/addon`)
- WebSocket to Worker `/ws/terminal/:sandboxId`
- Show when task is active, hide when done

**Phase 4: Gate verification loop**
- After agent says "done", sandbox runs `gates/health.sh`
- Pass → `chomp done`. Fail → inject output, agent loops (new session)
- Max N loops before marking failed

**Phase 5: Pi adapter**
- `pi` installed in Dockerfile (`npm i -g @mariozechner/pi-coding-agent`)
- CLI: `pi --message "prompt" --dir /workspace/repo`
- Same dispatch protocol as other agents

### Sandbox dispatch pseudocode

```typescript
import { getSandbox, type Sandbox } from '@cloudflare/sandbox';
export { Sandbox } from '@cloudflare/sandbox';

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    if (url.pathname === '/dispatch' && request.method === 'POST') {
      const { taskId, prompt, agent, model, repoUrl, dir } = await request.json();
      const sandbox = getSandbox(env.Sandbox, `task-${taskId}`, { keepAlive: true });

      if (repoUrl) {
        await sandbox.gitCheckout(repoUrl, { depth: 1, targetDir: '/workspace/repo' });
      }

      // Start agent in background
      await sandbox.startProcess(
        `CHOMP_API=${env.CHOMP_API} TASK_ID=${taskId} run-agent ${agent} ${model} "${prompt}"`,
      );

      return Response.json({ sandboxId: `task-${taskId}`, status: 'started' });
    }

    if (url.pathname.startsWith('/ws/terminal/')) {
      const sandboxId = url.pathname.split('/').pop();
      const sandbox = getSandbox(env.Sandbox, sandboxId!);
      return sandbox.terminal(request, { cols: 120, rows: 30 });
    }

    if (url.pathname.startsWith('/kill/')) {
      const sandboxId = url.pathname.split('/').pop();
      const sandbox = getSandbox(env.Sandbox, sandboxId!);
      await sandbox.destroy();
      return Response.json({ status: 'destroyed' });
    }

    return new Response('not found', { status: 404 });
  }
};
```

### Wrangler config

```jsonc
{
  "name": "chomp-sandbox",
  "main": "src/index.ts",
  "compatibility_date": "2025-01-01",
  "containers": [{ "class_name": "Sandbox", "image": "./Dockerfile", "max_instances": 5 }],
  "durable_objects": { "bindings": [{ "name": "Sandbox", "class_name": "Sandbox" }] },
  "vars": { "SANDBOX_TRANSPORT": "websocket", "CHOMP_API": "https://jordan.exe.xyz:8000" },
  "migrations": [{ "tag": "v1", "new_classes": ["Sandbox"] }]
}
```

### Dockerfile

```dockerfile
FROM docker.io/cloudflare/sandbox:0.7.0
RUN npm install -g @mariozechner/pi-coding-agent
COPY bin/chomp /usr/local/bin/chomp
COPY bin/run-agent /usr/local/bin/run-agent
RUN chmod +x /usr/local/bin/chomp /usr/local/bin/run-agent
```

### Critical rules

- **No theater.** Every number shown must come from real data.
- **Run tests before committing.** Small commits after each meaningful change.
- **Disk is tight (~19GB).** Clean Docker images, go cache, node_modules regularly.
- **No `confirm()` dialogs.** No `position:fixed/absolute` for layout.
- Read AGENTS.md for full project context.
