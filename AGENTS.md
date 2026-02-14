# chomp — Agent Instructions

## What this is

An OpenAI-compatible LLM proxy + docs site. Two deployments:

1. **Go API server** (`server.go`, 953 lines) — runs on this machine as a systemd service on `:8001`. Pure JSON API, no HTML. Proxies requests across 6 backends via `/v1/chat/completions`.
2. **Astro site** (`worker/`) — deployed to Cloudflare Workers at `chomp.coey.dev`. Public docs, landing page, and its own API routes that use Cloudflare KV for job storage.

## Repo structure

```
chomp/
├── server.go              # Go API server (runs on this box, :8001)
├── server_test.go         # 28 tests
├── worker/                # Astro site → Cloudflare Workers (chomp.coey.dev)
│   ├── src/
│   │   ├── pages/         # index, docs/*, api routes
│   │   ├── components/    # Nav, Code, SEO
│   │   ├── layouts/       # Layout.astro (theme toggle, fonts)
│   │   ├── styles/        # global.css (Tailwind v4)
│   │   └── lib/           # auth.ts
│   ├── wrangler.jsonc     # CF Workers config + KV bindings (JOBS)
│   └── astro.config.mjs   # SSR + Cloudflare adapter + Tailwind v4 vite plugin
├── examples/kitchen-sink.sh  # Integration test script
├── gates/pre-push.sh         # Pre-push hook (go vet, go test, tsc, astro build)
├── state/.env                # API keys (gitignored)
├── .github/workflows/ci.yml  # CI: go vet+test → tsc+build → wrangler deploy
├── docs/                     # Misc docs
├── Dockerfile                # Multi-stage Go build
├── go.mod                    # Go 1.22.2
└── AGENTS.md
```

## Go API server (server.go)

Runs as `chomp.service` on this machine. Pure JSON, no HTML.

| Endpoint | Purpose |
|---|---|
| `GET /` | `{"name":"chomp","version":"2.0.0","routers":N}` |
| `POST /v1/chat/completions` | OpenAI-compatible proxy (the product) |
| `GET /v1/models` | Aggregated model list from all routers |
| `POST /api/dispatch` | Async prompt dispatch, returns job ID |
| `GET /api/result/:id` | Poll for job completion |
| `GET /api/jobs` | List recent jobs |
| `GET /api/models/:router` | Per-router model listing |
| `GET /api/models/free` | OpenRouter free models |
| `GET /api/config` | Router status |
| `GET /api/platforms` | Router health check |

### Routers

Six backends defined in `routerDefs`:

| ID | Base URL | Env Key | Default Model |
|---|---|---|---|
| `groq` | `api.groq.com/openai/v1` | `GROQ_API_KEY` | `llama-3.3-70b-versatile` |
| `cerebras` | `api.cerebras.ai/v1` | `CEREBRAS_API_KEY` | `llama-3.3-70b` |
| `sambanova` | `api.sambanova.ai/v1` | `SAMBANOVA_API_KEY` | `Meta-Llama-3.3-70B-Instruct` |
| `together` | `api.together.xyz/v1` | `TOGETHER_API_KEY` | `meta-llama/Llama-3.3-70B-Instruct-Turbo` |
| `fireworks` | `api.fireworks.ai/inference/v1` | `FIREWORKS_API_KEY` | `accounts/fireworks/models/llama-v3p3-70b-instruct` |
| `openrouter` | `openrouter.ai/api/v1` | `OPENROUTER_API_KEY` | `auto` |

Adding a router = one `RouterDef{}` entry in `routerDefs`. Proxy, model listing, and health all pick it up.

### Auth

- `CHOMP_API_TOKEN` env var → clients pass `Authorization: Bearer <token>`
- `CHOMP_V1_NO_AUTH=1` → skip auth (dev only)

## Astro site (worker/)

SSR on Cloudflare Workers. Tailwind CSS v4 + `@tailwindcss/vite` plugin.

**Dark mode:** Uses class-based dark mode via `@custom-variant dark (&:where(.dark, .dark *))` in `global.css`. Toggle button in Nav.astro saves preference to a cookie.

**API routes** (CF Workers, use KV for storage):
- `POST /api/keys` — Register OpenRouter key, get chomp token
- `GET /api/keys` — Check key status
- `DELETE /api/keys` — Revoke token
- `POST /api/dispatch` — Dispatch prompt to free model
- `GET /api/result/[id]` — Poll job result
- `GET /api/jobs` — List jobs
- `GET /api/models/free` — Free model list
- `GET /api/og` — OG image generation

**Pages:** `/` (landing), `/docs` (tutorial), `/docs/reference`, `/docs/guides`, `/docs/concepts`

## CI pipeline

```
push to main → go vet + go test → tsc + astro build → wrangler deploy
```

Defined in `.github/workflows/ci.yml`. Uses bun for the Astro build. Deploy only runs on main branch pushes.

## Build & run

```bash
# Go server
go build -o chomp-server server.go
CHOMP_V1_NO_AUTH=1 ./chomp-server   # :8001

# Or use systemd
sudo systemctl restart chomp

# Tests
go test -timeout 30s -count=1 ./...

# Astro site (local dev)
cd worker && npm install && npm run dev

# Astro build
cd worker && npm run build
```

## Design language

- Font: **Sora** (400, 600, 700)
- Tailwind CSS v4, class-based dark mode
- Gold accent: `#c8a630`
- Mobile-first

## Key decisions

- **server.go is one file** — intentional, not an accident
- **No database** — Go server uses in-memory state; CF Workers use KV
- **RouterDef is the abstraction** — struct with ID, name, base URL, env key, default model
- **Adapters are shell scripts** — `available` and `run` functions
- **Two separate API surfaces** — Go server (private, this box) and CF Workers (public, chomp.coey.dev)

## Rules

- Don't commit `state/` contents or the compiled binary
- 28 tests must pass before merging (`go test ./...`)
- `tsc --noEmit` must pass for the Astro site
- Keep the site mobile-first
- Adding a router = one `RouterDef{}` entry, nothing else
- Commit messages should be descriptive
