## Status: ✅ COMPLETE (all 4 phases done)

All Go server code has been consolidated into CF Workers. One codebase, one deploy.

**Completed:**
- Phase 1: Shared router infrastructure (routers.ts) + multi-key auth
- Phase 2: /v1/chat/completions proxy + /v1/models aggregated listing
- Phase 3: Dispatch + MCP tools use shared routers
- Phase 4: Deleted Go files, updated CI/docs/pre-push
- All checks pass: tsc, build, MCP smoke test, pre-push gate

---

# HANDOFF: Kill Go server, consolidate into CF Workers

## Goal

One codebase. One deploy. Everything in `worker/` on Cloudflare Workers.
Delete `server.go`, `server_test.go`, `go.mod`, `go.sum`, `Dockerfile`.

## What the Go server has that Workers doesn't

### 1. `/v1/chat/completions` — OpenAI-compatible proxy (THE feature)

Synchronous. Client sends messages, gets a response. Any OpenAI SDK works as a drop-in.

- Resolves router from `model` field or `router` extension field
- Resolves model (auto = pick default for router)
- Calls upstream via `callOpenAICompat()` — standard OpenAI request format
- Returns standard OpenAI response format with chomp extensions (`router`, `latency_ms`)
- 120s timeout

**Port to:** `worker/src/pages/v1/chat/completions.ts`

### 2. `/v1/models` — aggregated model listing

- Iterates all routers with configured keys
- Fetches each router's `/models` endpoint (15-min cache)
- Returns unified OpenAI-format model list with `router/model-id` prefixed IDs

**Port to:** `worker/src/pages/v1/models.ts`

### 3. Multi-router support (6 routers)

Router definitions — pure data:

```typescript
const routers = [
  { id: "zen",        name: "OpenCode Zen", baseUrl: "https://opencode.ai/zen/v1",             defaultModel: "minimax-m2.5-free" },
  { id: "groq",       name: "Groq",         baseUrl: "https://api.groq.com/openai/v1",          defaultModel: "llama-3.3-70b-versatile" },
  { id: "cerebras",   name: "Cerebras",     baseUrl: "https://api.cerebras.ai/v1",              defaultModel: "llama-3.3-70b" },
  { id: "sambanova",  name: "SambaNova",    baseUrl: "https://api.sambanova.ai/v1",             defaultModel: "Meta-Llama-3.3-70B-Instruct" },
  { id: "fireworks",  name: "Fireworks",    baseUrl: "https://api.fireworks.ai/inference/v1",    defaultModel: "accounts/fireworks/models/llama-v3p3-70b-instruct" },
  { id: "openrouter", name: "OpenRouter",   baseUrl: "https://openrouter.ai/api/v1",            defaultModel: "auto", headers: { "HTTP-Referer": "https://chomp.coey.dev", "X-Title": "chomp" } },
]
```

**Port to:** `worker/src/mcp/routers.ts` (shared by MCP tools + /v1/ proxy)

### 4. API key model change

Currently: users register ONE OpenRouter key → get a token.

New: users register MULTIPLE router keys → stored in KV.

```
KV key: user:{token}
Old value: { "openrouter_key": "sk-or-...", "created": "..." }
New value: {
  "keys": {
    "openrouter": "sk-or-...",
    "groq": "gsk_...",
    "zen": "..."
  },
  "created": "..."
}
```

`POST /api/keys` body changes:
```json
// Old
{"openrouter_key": "sk-or-..."}
// New (backward-compatible)
{"openrouter_key": "sk-or-..."}           // still works
{"keys": {"groq": "gsk_...", "openrouter": "sk-or-..."}}  // new multi-key
```

**Port to:** update `worker/src/pages/api/keys.ts` + `worker/src/lib/auth.ts`

### 5. Dispatch gets router support

Currently Workers dispatch only goes through OpenRouter.
New: dispatch accepts `router` field, routes to the right backend using user's key for that router.

**Port to:** update `worker/src/pages/api/dispatch.ts` + `worker/src/mcp/services.ts`

### 6. Per-router model listing

`GET /api/models/:router` — fetches models from a specific router's API.

Already partially exists. Needs caching (KV or in-memory with `caches` API).

**Port to:** `worker/src/pages/api/models/[router].ts`

## Execution order

### Phase 1: Shared router infrastructure
1. Create `worker/src/lib/routers.ts` — router definitions, `callRouter()`, `getRouter()`
2. Update `worker/src/lib/auth.ts` — multi-key user record
3. Update `worker/src/pages/api/keys.ts` — accept multi-key registration (backward-compatible)
4. Tests: register with multiple keys, verify KV storage

### Phase 2: `/v1/` endpoints
5. Create `worker/src/pages/v1/chat/completions.ts` — OpenAI-compatible proxy
   - Parse model field ("groq/llama-3.3-70b" → router=groq, model=llama-3.3-70b)
   - Or use `router` extension field
   - Call upstream, return OpenAI-format response
   - 120s timeout via AbortController
6. Create `worker/src/pages/v1/models.ts` — aggregated model list
7. Tests: curl with OpenAI SDK format, verify responses match spec

### Phase 3: Update dispatch + MCP to use routers
8. Update `worker/src/pages/api/dispatch.ts` — accept `router` field, use user's key for that router
9. Update `worker/src/mcp/services.ts` — ChompService uses routers
10. Update `worker/src/mcp/tools.ts` — tools accept `router` param
11. Update Zod schemas in `worker/src/mcp/schemas.ts`

### Phase 4: Cleanup
12. Delete: `server.go`, `server_test.go`, `go.mod`, `go.sum`, `Dockerfile`, `state/`
13. Remove Go from CI (`go vet`, `go test` steps in `.github/workflows/ci.yml`)
14. Update pre-push hook (remove Go checks)
15. Update `AGENTS.md`, `README.md` — one architecture, one deploy
16. Update all docs pages that reference the Go server or localhost:8001

## Key decisions

- **Router keys are user-scoped.** Each user brings their own keys. No shared keys on the server.
- **Model prefix convention:** `groq/llama-3.3-70b` = router `groq`, model `llama-3.3-70b`. Same as Go server.
- **`auto` router:** picks first router the user has a key for.
- **`auto` model:** picks default model for the resolved router.
- **Backward-compatible auth:** Old `{"openrouter_key": "..."}` still works, gets stored as `{"keys": {"openrouter": "..."}}`.
- **Effect stays:** The MCP service layer uses Effect. The new `/v1/` proxy can use Effect too (typed errors, retry, timeout) or be plain — dealer's choice.
- **Caching:** Model lists cached via CF Workers `caches` API (15 min TTL).

## What we keep from Go (the ideas, not the code)

- RouterDef as pure data
- `callOpenAICompat()` pattern — generic function that works with any OpenAI-compatible backend
- Model prefix convention (`router/model`)
- Auto-resolution chain (router → key → model)

## Risk

- **CF Workers execution time:** Synchronous `/v1/chat/completions` must wait for upstream. Paid plan allows long wall time. Should be fine.
- **Cold starts:** First request to a fresh Worker instance may be slow. Not worse than current.
- **No streaming yet:** Go server didn't have it either. Can add later with SSE.
