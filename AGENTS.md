# chomp — Agent Instructions

## What this is

An OpenAI-compatible LLM proxy running on Cloudflare Workers. One codebase, one deployment: an Astro SSR site at **chomp.coey.dev** that serves the docs site and the full API.

## Repo structure

```
chomp/
├── worker/                    # Astro site → Cloudflare Workers (chomp.coey.dev)
│   ├── src/
│   │   ├── pages/             # index, docs/*, api routes, v1/ proxy, mcp
│   │   ├── components/        # Nav, Code, SEO
│   │   ├── layouts/           # Layout.astro (theme toggle, fonts)
│   │   ├── lib/               # auth.ts (multi-key), routers.ts (6 providers)
│   │   ├── mcp/               # MCP server (Effect-ts): server, services, tools, schemas, errors
│   │   └── styles/            # global.css (Tailwind v4)
│   ├── wrangler.jsonc         # CF Workers config + KV bindings (JOBS)
│   └── astro.config.mjs       # SSR + Cloudflare adapter + Tailwind v4 vite plugin
├── .github/workflows/ci.yml   # CI: tsc + build → wrangler deploy
├── gates/pre-push.sh          # Pre-push hook (tsc, build, MCP smoke test)
└── AGENTS.md
```

## API endpoints

| Endpoint | Method | Purpose |
|---|---|---|
| `/v1/chat/completions` | POST | OpenAI-compatible proxy (the product) |
| `/v1/models` | GET | Aggregated model list from all routers |
| `/api/dispatch` | POST | Async prompt dispatch, returns job ID |
| `/api/result/[id]` | GET | Poll for job completion |
| `/api/jobs` | GET | List recent jobs |
| `/api/keys` | POST | Register provider keys, get chomp token |
| `/api/keys` | GET | Check key status |
| `/api/keys` | DELETE | Revoke token |
| `/api/models/free` | GET | OpenRouter free models |
| `/api/og` | GET | OG image generation |
| `/mcp` | POST | MCP server (Effect-ts) |

**Pages:** `/` (landing), `/docs` (tutorial), `/docs/reference`, `/docs/guides`, `/docs/concepts`, `/docs/guides/exe-dev`, `/docs/guides/mcp`, `/docs/guides/tool`

## Routers

Six backends defined in `worker/src/lib/routers.ts`:

| ID | Name | Base URL | Default Model |
|---|---|---|---|
| `zen` | OpenCode Zen | `opencode.ai/zen/v1` | `minimax-m2.5-free` |
| `groq` | Groq | `api.groq.com/openai/v1` | `llama-3.3-70b-versatile` |
| `cerebras` | Cerebras | `api.cerebras.ai/v1` | `llama-3.3-70b` |
| `sambanova` | SambaNova | `api.sambanova.ai/v1` | `Meta-Llama-3.3-70B-Instruct` |
| `fireworks` | Fireworks | `api.fireworks.ai/inference/v1` | `accounts/fireworks/models/llama-v3p3-70b-instruct` |
| `openrouter` | OpenRouter | `openrouter.ai/api/v1` | `auto` |

**Adding a router = one `RouterDef` object** in the `routers` array. Proxy, model listing, and resolution all pick it up automatically.

## Auth

- **Multi-key:** users register API keys for multiple providers in a single request
- `POST /api/keys` accepts `{keys: {groq: "gsk_...", openrouter: "sk-or-..."}}` or legacy `{openrouter_key: "..."}`
- A random hex token is returned; stored in KV as `user:{token}` → `{keys: {...}, created}`
- Legacy records (`{openrouter_key, created}`) are normalised on read
- Bearer token auth on all API calls: `Authorization: Bearer <token>`
- `getUserKey(user, routerId)` gets a user's key for a specific router
- `getFirstAvailableRouter(user, routerIds)` finds the first router a user has a key for

## Model prefix convention

Models are addressed as `router/model`:
- `groq/llama-3.3-70b` → router `groq`, model `llama-3.3-70b`
- `fireworks/accounts/fireworks/models/llama-v3p3-70b-instruct` → router `fireworks`, model as-is

Resolution order: explicit router prefix → first available router the user has a key for. If the prefix doesn't match a known router ID, the entire string is treated as the model name (handles models with slashes like fireworks paths).

## MCP

Effect-ts based MCP server at `/mcp`. Files in `worker/src/mcp/`:

| File | Purpose |
|---|---|
| `server.ts` | MCP server setup and request handling |
| `services.ts` | Effect service layer |
| `tools.ts` | Tool definitions: `ask`, `dispatch`, `result` |
| `schemas.ts` | Schemas (Effect Schema + Zod) |
| `errors.ts` | Typed error definitions |

Smoke test: `node worker/test-mcp.mjs`

## CI pipeline

```
push to main → tsc --noEmit → astro build → wrangler deploy
```

Defined in `.github/workflows/ci.yml`. Uses bun for everything. Deploy only runs on main branch pushes.

**Pre-push hook** (`gates/pre-push.sh`): tsc, astro build, MCP smoke test.

## Build & run

```bash
cd worker && bun install && bun run dev      # local dev server
cd worker && bunx tsc --noEmit                # type check
cd worker && bun run build                    # full production build
```

## Design language

- Font: **Sora** (400, 600, 700)
- Tailwind CSS v4 with `@tailwindcss/vite` plugin
- Gold accent: `#c8a630`
- Mobile-first
- Class-based dark mode via `@custom-variant dark (&:where(.dark, .dark *))` in `global.css`
- Theme toggle in Nav.astro saves preference to a cookie

## Key decisions

- **One codebase, one deployment** — Astro SSR on Cloudflare Workers, no separate server
- **No database** — Cloudflare KV for jobs and user records
- **RouterDef is pure data** — adding a router = one object in the array
- **Effect-ts for MCP service layer** — typed errors, retry, timeout
- **User-scoped keys** — each user brings their own provider API keys
- **Multi-key auth** — a single chomp token maps to keys for multiple providers

## Rules

- `tsc --noEmit` must pass
- `astro build` must pass
- MCP smoke test must pass (`node worker/test-mcp.mjs`)
- Keep the site mobile-first
- Adding a router = one `RouterDef` entry, nothing else
- Commit messages should be descriptive
