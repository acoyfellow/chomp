# chomp

OpenAI-compatible LLM proxy on Cloudflare Workers. Routes to 6 free/cheap model providers via a standard `/v1/chat/completions` endpoint.

**[chomp.coey.dev](https://chomp.coey.dev)** · **[Docs](https://chomp.coey.dev/docs)** · **[Source](https://github.com/acoyfellow/chomp)**

## Quick start

Register your provider API keys:

```bash
curl -X POST https://chomp.coey.dev/api/keys \
  -H "Content-Type: application/json" \
  -d '{
    "groq": "gsk_...",
    "cerebras": "csk_...",
    "sambanova": "sk_..."
  }'
# → { "token": "chomp_abc123..." }
```

Use the returned token with any OpenAI-compatible client:

```bash
curl https://chomp.coey.dev/v1/chat/completions \
  -H "Authorization: Bearer chomp_abc123..." \
  -H "Content-Type: application/json" \
  -d '{
    "model": "groq/llama-3.3-70b-versatile",
    "messages": [{"role": "user", "content": "hello"}]
  }'
```

Or with the OpenAI SDK:

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://chomp.coey.dev/v1",
    api_key="chomp_abc123..."
)

response = client.chat.completions.create(
    model="groq/llama-3.3-70b-versatile",
    messages=[{"role": "user", "content": "hello"}]
)
```

## Model prefix convention

Models are addressed as `router/model-name`:

```
groq/llama-3.3-70b-versatile   →  router: groq,     model: llama-3.3-70b-versatile
cerebras/llama-3.3-70b         →  router: cerebras,  model: llama-3.3-70b
zen/minimax-m2.5-free          →  router: zen,       model: minimax-m2.5-free
```

If no prefix is given, chomp picks the first router that has a matching model.

## API

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/v1/chat/completions` | Chat completions proxy (streaming supported) |
| `GET` | `/v1/models` | Aggregated model list across all providers |
| `POST` | `/api/dispatch` | Async task dispatch |
| `GET` | `/api/result/[id]` | Poll async result |
| `GET` | `/api/jobs` | List jobs |
| `POST` | `/api/keys` | Register API keys → receive a chomp token |
| `GET` | `/api/keys` | Check key status |
| `DELETE` | `/api/keys` | Revoke token |
| `GET` | `/api/models/free` | Free model list |

Auth: `Authorization: Bearer <chomp_token>`

## Routers

6 providers, each configured with its own API key:

| Router | ID | Default model |
| --- | --- | --- |
| OpenCode Zen | `zen` | `minimax-m2.5-free` |
| Groq | `groq` | `llama-3.3-70b-versatile` |
| Cerebras | `cerebras` | `llama-3.3-70b` |
| SambaNova | `sambanova` | `Meta-Llama-3.3-70B-Instruct` |
| Fireworks | `fireworks` | `accounts/fireworks/models/llama-v3p3-70b-instruct` |
| OpenRouter | `openrouter` | `auto` |

Users bring their own keys — register them via `POST /api/keys` to get a chomp token.

## MCP server

Built-in MCP server for AI agent integration, available at `/mcp`. Built with Effect-ts.

## Structure

```
chomp/
├── worker/                Astro site → chomp.coey.dev (Cloudflare Workers)
│   ├── src/
│   │   ├── pages/         index, docs/*, api routes, v1/ proxy
│   │   ├── components/    Nav, Code, SEO
│   │   ├── layouts/       Layout.astro
│   │   ├── lib/           auth.ts, routers.ts
│   │   ├── mcp/           MCP server (Effect-ts)
│   │   └── styles/        global.css (Tailwind v4)
│   ├── wrangler.jsonc     CF Workers config + KV bindings
│   └── astro.config.mjs   SSR + Cloudflare adapter
├── .github/workflows/     CI/CD
├── AGENTS.md              Agent instructions
└── README.md
```

## Development

```bash
cd worker
bun install
bun run dev
```

## Tests

```bash
cd worker
bunx tsc --noEmit
bun run build
```

## Deployment

Push to `main` → CI builds and deploys to Cloudflare Workers.

## License

MIT
