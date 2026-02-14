# chomp — Agent Instructions

## What this is

An OpenAI-compatible LLM proxy. Point any tool that speaks the OpenAI API at chomp's `POST /v1/chat/completions` endpoint and it routes requests across 7 backends. Also has a task-queue dashboard for dispatching AI agent work. The Go server is the product.

## Repo structure

```
chomp/
├─ server.go             # Go server (~2500 lines): proxy, API, dashboard, all logic
├─ server_test.go        # 89 tests (unit + integration)
├─ templates/            # Go html/template files
│  ├─ layout.html        # Base HTML (Sora font, HTMX, Tailwind)
│  ├─ page.html          # App shell (topbar, balance, tabs, sheets, JS)
│  └─ partials/          # HTMX fragments (balance, tasks, detail, settings, create)
├─ static/               # Tailwind input/output CSS
├─ bin/chomp             # CLI (bash + jq)
├─ adapters/             # Platform dispatch scripts (exedev.sh, opencode.sh)
├─ worker/               # Astro site → docs/marketing at chomp.coey.dev
│  ├─ src/pages/         # index, docs, api pages
│  └─ wrangler.jsonc     # Cloudflare Workers config
├─ Dockerfile            # Multi-stage Go build
├─ state/                # Runtime: state.json, keys.json, agents.json (gitignored)
├─ go.mod                # Go 1.22.2
└─ README.md
```

## Key decisions

- **OpenAI-compatible proxy** is the core value. `POST /v1/chat/completions` and `GET /v1/models` work with any OpenAI SDK or tool.
- **RouterDef is the abstraction.** Each backend is a struct: ID, name, base URL, env key for the API key, default model, optional extra headers. Adding a router = one new entry in `routerDefs`.
- **state.json** is the single source of truth for tasks. CLI writes it, server reads it, dashboard displays it.
- **No database.** JSON file + jq. Intentionally simple.
- **Adapters are shell scripts.** Two functions: `available` and `run`. Adding a platform = one new .sh file.
- **The Go server is the product.** Everything in one binary — proxy, dashboard, API.
- **worker/ is marketing.** The Astro site at chomp.coey.dev is docs and landing page, deployed to Cloudflare Workers.

## Routers

Seven OpenAI-compatible backends, defined in `routerDefs` in server.go:

| ID | Name | Base URL | Env Key | Default Model |
|---|---|---|---|---|
| `zen` | OpenCode Zen | `opencode.ai/zen/v1` | `OPENCODE_ZEN_API_KEY` | `minimax-m2.5-free` |
| `groq` | Groq | `api.groq.com/openai/v1` | `GROQ_API_KEY` | `llama-3.3-70b-versatile` |
| `cerebras` | Cerebras | `api.cerebras.ai/v1` | `CEREBRAS_API_KEY` | `llama-3.3-70b` |
| `sambanova` | SambaNova | `api.sambanova.ai/v1` | `SAMBANOVA_API_KEY` | `Meta-Llama-3.3-70B-Instruct` |
| `together` | Together | `api.together.xyz/v1` | `TOGETHER_API_KEY` | `meta-llama/Llama-3.3-70B-Instruct-Turbo` |
| `fireworks` | Fireworks | `api.fireworks.ai/inference/v1` | `FIREWORKS_API_KEY` | `accounts/fireworks/models/llama-v3p3-70b-instruct` |
| `openrouter` | OpenRouter | `openrouter.ai/api/v1` | `OPENROUTER_API_KEY` | `auto` |

**Adding a new router:** Add a `RouterDef{}` entry to `routerDefs` with ID, Name, BaseURL, EnvKey, Color, and DefaultModel. That's it — the proxy, dashboard, and model listing all pick it up automatically.

## Auth

- **`CHOMP_API_TOKEN`** — Set this env var. Clients pass `Authorization: Bearer <token>` on every `/v1/` request.
- **`CHOMP_V1_NO_AUTH=1`** — Skip auth entirely. For local-only / development use.
- The dashboard API endpoints (`/api/*`) use the same `CHOMP_API_TOKEN` for protected operations.

## API surface

### OpenAI-compatible (the proxy)
- `POST /v1/chat/completions` — Proxies to a router. Supports streaming.
- `GET /v1/models` — Lists available models across all configured routers.

### Dashboard API
- `GET /api/state` — Full task state
- `GET|POST /api/config` — Server config
- `POST /api/tasks` — Add task
- `POST /api/tasks/run` — Run task
- `POST /api/tasks/done` — Mark done
- `POST /api/tasks/delete` — Delete task
- `POST /api/dispatch` — Dispatch to agent
- `GET /api/models/free` — Free model list
- `GET /api/models/{router}` — Models for a specific router
- `GET /api/jobs` — Job listing

### Dashboard pages (HTMX)
- `GET /` — Main dashboard
- `GET /docs` — Docs page
- `GET /partials/*` — HTMX partial fragments

## Design language

- Font: **Sora** (Bold 700, Regular 400)
- Light mode default, dark mode via toggle
- Mobile-first: single column, bottom sheets for detail/picker
- Stripe's precision + PS5 boot sequence + luxury car door weight

## Build & run

```bash
# Build server
go build -o chomp-server server.go

# Run locally (no auth, good for dev)
CHOMP_V1_NO_AUTH=1 ./chomp-server   # serves on :8001

# Run with auth
CHOMP_API_TOKEN=your-secret-token ./chomp-server

# Set router API keys (only the ones you need)
export GROQ_API_KEY=gsk_...
export OPENROUTER_API_KEY=sk-or-...
# etc.

# Test
go test ./...

# Quick smoke test the proxy
curl http://localhost:8001/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"groq/llama-3.3-70b-versatile","messages":[{"role":"user","content":"hi"}]}'
```

### Cloudflare deployment (worker/)

GitHub Actions auto-deploys `worker/` to Cloudflare on push to `main`. The workflow builds the Astro site with Bun and runs `wrangler deploy`. Site lives at chomp.coey.dev.

```bash
# Local dev for the Astro site
cd worker && bun install && bun run dev
```

## Tests

89 tests in `server_test.go`. Covers:
- All `/v1/` endpoints (auth, method checks, payload validation, router resolution)
- Task CRUD operations
- Config and settings APIs
- Dashboard partial rendering
- Router definitions and model listing

Run with `go test ./...` or `go test -v -run TestName` for a specific test.

## What to work on next

1. **Streaming reliability** — Ensure SSE streaming works cleanly across all 7 routers
2. **Router health / fallback** — Auto-failover when a router is down or rate-limited
3. **Usage tracking** — Token counts per router, per key, with dashboard display
4. **More routers** — DeepInfra, Lepton, etc. (just add a RouterDef)
5. **Worker site content** — Flesh out docs and API reference at chomp.coey.dev
6. **Agent dispatch polish** — Wire dashboard picker to dispatch tasks with router selection

## Rules

- Don't commit `state/` contents or the compiled binary
- Keep `server.go` as one file — it's intentional, not an accident
- 89 tests must pass before merging (`go test ./...`)
- Keep the dashboard mobile-first — test at 390px before merging
- Keep adapters as simple shell scripts
- Adding a router = one `RouterDef{}` entry, nothing else
- Commit messages should be descriptive (see git log for style)
