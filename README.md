# chomp

Local OpenAI-compatible LLM proxy. Routes to 7 free/cheap model providers, exposes a standard `/v1/chat/completions` endpoint. Built for AI agents to offload work to free models.

**[Docs](https://chomp.coey.dev)** · **[Source](https://github.com/acoyfellow/chomp)**

## Quick start

```bash
# Build
go build -o chomp-server server.go

# Configure
cp state/.env.example state/.env  # add your API keys

# Run
./chomp-server
# or: systemctl start chomp

# Use
curl http://localhost:8001/v1/chat/completions \
  -H "Authorization: Bearer $CHOMP_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model": "llama-3.3-70b", "messages": [{"role": "user", "content": "hello"}]}'
```

## API

OpenAI-compatible. Point any client at `http://localhost:8001`.

| Endpoint | Description |
| --- | --- |
| `POST /v1/chat/completions` | Chat completions (streaming supported) |
| `GET /v1/models` | List available models |
| `POST /api/dispatch` | Async task dispatch |
| `GET /api/result/:id` | Poll async result |
| `GET /` | Dashboard (HTMX) |

Auth: `Authorization: Bearer <CHOMP_API_TOKEN>`

## Routers

7 providers, configured via env vars in `state/.env`:

| Router | Env var |
| --- | --- |
| OpenCode Zen | `OPENCODE_ZEN_API_KEY` |
| Groq | `GROQ_API_KEY` |
| Cerebras | `CEREBRAS_API_KEY` |
| SambaNova | `SAMBANOVA_API_KEY` |
| Together | `TOGETHER_API_KEY` |
| Fireworks | `FIREWORKS_API_KEY` |
| OpenRouter | `OPENROUTER_API_KEY` |

Also:

| Var | Purpose |
| --- | --- |
| `CHOMP_API_TOKEN` | Bearer token for client auth |
| `PORT` | Listen port (default `8001`) |

## Structure

```
chomp/
├── server.go              Go server: /v1/ proxy + /api/ + dashboard (~2500 lines)
├── server_test.go         89 tests
├── bin/chomp              CLI (bash + jq)
├── adapters/              Agent dispatch scripts (shelley, opencode, etc)
├── templates/             Go html/template (HTMX dashboard)
├── static/                CSS + HTMX
├── worker/                Astro site → chomp.coey.dev (Cloudflare Workers)
├── .github/workflows/     CI/CD: deploy Astro to CF on push
├── Dockerfile             Multi-stage Go build
├── state/                 Runtime: state.json, .env (gitignored)
├── AGENTS.md              Agent instructions
└── llms.txt               LLM-readable project summary
```

## Run with Docker

```bash
docker build -t chomp .
docker run -d --name chomp --restart unless-stopped \
  -p 8001:8001 \
  -v $(pwd)/state:/app/state \
  chomp
```

## Run with systemd

```ini
[Unit]
Description=chomp LLM proxy
After=network.target

[Service]
ExecStart=/path/to/chomp-server
WorkingDirectory=/path/to/chomp
Restart=always
EnvironmentFile=/path/to/chomp/state/.env

[Install]
WantedBy=multi-user.target
```

## Tests

```bash
go test -v ./...
```

89 tests covering routers, auth, streaming, task dispatch, and dashboard rendering.

## License

MIT
