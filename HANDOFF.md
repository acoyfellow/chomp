# chomp — Handoff: Unify UX

## Current State (2026-02-14)

**What exists:**
- Go server (`server.go`, ~2500 lines, 89 tests) serving:
  - `/v1/chat/completions` + `/v1/models` — OpenAI-compatible proxy ✅ THE PRODUCT
  - `/api/dispatch`, `/api/result/:id`, `/api/jobs` — async dispatch ✅ KEEP
  - `/api/models/:router`, `/api/models/free` — model listing ✅ KEEP
  - `/api/config`, `/api/platforms` — status ✅ KEEP
  - `/`, `/docs`, `/partials/*`, `/api/tasks/*` — HTMX dashboard ❌ REMOVE
- Astro site (`worker/`) deployed to chomp.coey.dev via GitHub Actions ✅ KEEP
- 6 live routers: Zen, Groq, Cerebras, SambaNova, Fireworks, OpenRouter (433 models)
- CI: go vet + go test + tsc --noEmit + astro build → deploy
- Pre-push hook, llms.txt, kitchen-sink test

**The problem:** Two UIs. The Go server has an HTMX dashboard nobody needs. The Astro site is the real public face. The Go server should be API-only.

## Task: Strip the Go server to API-only

### 1. Remove from server.go:
- All template/HTML handling: `pageIndex`, `pageDocs`, `partialsBalance`, `partialsTasks`, `partialsDetail`, `partialsSettings`, `partialsCreate`
- Template parsing (`templateFS`, `tmpl`, `template.New`)
- Static file serving (`serveCSS`, `serveHTMX`, `staticCSS`, `staticHTMX`)
- All `/partials/*` and `/api/tasks/*` routes (task queue is dead weight)
- The `embed` directives for templates and static files
- Template helper functions: `fmtTokens`, `timeAgo`, `isStale`, `agentName`, `agentColorStr`
- The task/session/state management: `Task`, `Session`, `State`, `readState`, `writeState`, `stateMu`, `stateFile`, etc.
- Agent/adapter management: `builtinAgents`, `mergedAgents`, `AgentConfig`, adapters
- Sandbox dispatch: `sandboxWorkerURL`, `apiSandboxOutput`, etc.
- The `apiAddTask`, `apiRunTask`, `apiDoneTask`, `apiUpdateTask`, `apiHandoffTask`, `apiDeleteTask` handlers
- Keys/agents file management: `keysFile`, `agentsFile`, `apiConfigKeys`, `apiConfigAgents`

### 2. Keep in server.go:
- `/v1/chat/completions` — the core product
- `/v1/models` — aggregated model list
- `/api/dispatch` + `/api/result/:id` + `/api/jobs` — async dispatch
- `/api/models/:router` + `/api/models/free` — model listing
- `/api/config` — but simplify to just show routers + their status
- `/api/platforms` — router status
- Router registry (`routerDefs`, `callOpenAICompat`, `callRouter`, etc.)
- Auth (`v1Auth`, `requireAuth`)
- Free model scanning for OpenRouter
- Job management (in-memory jobs map)

### 3. After stripping, the Go server should:
- Serve ONLY JSON API endpoints (no HTML at all)
- `/` should return `{"name":"chomp","version":"...","routers":6,"models":433}`
- Be ~800-1000 lines instead of ~2500
- Still pass all meaningful tests (delete dashboard tests)

### 4. Delete from repo:
- `templates/` directory (all .html files)
- `static/` directory (CSS, HTMX)
- `ui/` directory (already deleted)
- `adapters/` directory (shell scripts for agent dispatch)
- `bin/chomp` CLI (task queue CLI, not needed)

### 5. Astro site (worker/) — explore API playground:
- Consider adding a `/playground` page that calls the chomp API
- User enters a prompt, picks a router, sees the response
- Could call a hosted chomp instance or be a demo that shows curl commands
- This is optional/exploratory — the docs are the priority

## Key Files
- `server.go` — the Go server (strip this)
- `server_test.go` — tests (update to match stripped server)
- `worker/` — Astro site (keep, maybe enhance)
- `examples/kitchen-sink.sh` — integration test (keep)
- `state/.env` — API keys (gitignored)
- `.github/workflows/ci.yml` — CI pipeline
- `gates/pre-push.sh` — pre-push hook

## Router Registry (DO NOT CHANGE)
```go
routerDefs = []RouterDef{
    {ID: "zen",        BaseURL: "https://opencode.ai/zen/v1",           EnvKey: "OPENCODE_ZEN_API_KEY"},
    {ID: "groq",       BaseURL: "https://api.groq.com/openai/v1",      EnvKey: "GROQ_API_KEY"},
    {ID: "cerebras",   BaseURL: "https://api.cerebras.ai/v1",           EnvKey: "CEREBRAS_API_KEY"},
    {ID: "sambanova",  BaseURL: "https://api.sambanova.ai/v1",          EnvKey: "SAMBANOVA_API_KEY"},
    {ID: "fireworks",  BaseURL: "https://api.fireworks.ai/inference/v1", EnvKey: "FIREWORKS_API_KEY"},
    {ID: "openrouter", BaseURL: "https://openrouter.ai/api/v1",         EnvKey: "OPENROUTER_API_KEY"},
}
```

## Env Vars
```
CHOMP_API_TOKEN=xxx          # Bearer token for all endpoints
OPENCODE_ZEN_API_KEY=xxx     # OpenCode Zen
GROQ_API_KEY=xxx             # Groq
CEREBRAS_API_KEY=xxx         # Cerebras
SAMBANOVA_API_KEY=xxx        # SambaNova
FIREWORKS_API_KEY=xxx        # Fireworks
OPENROUTER_API_KEY=xxx       # OpenRouter
```

## After completion:
- `go vet ./...` passes
- `go test` passes (all tests updated)
- `kitchen-sink.sh` still works
- Astro site builds and deploys
- Pre-push hook passes
- Commit and push
