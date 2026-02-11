# chomp-sandbox

Cloudflare Worker that dispatches AI agent tasks to Sandbox containers.

## Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/dispatch` | Spin up sandbox, start agent |
| GET | `/status/:sandboxId` | Check if sandbox is alive |
| POST | `/kill/:sandboxId` | Destroy sandbox container |
| GET | `/health` | Worker health check |

## Dispatch payload

```json
{
  "taskId": "abc123",
  "prompt": "refactor the auth module",
  "agent": "pi",
  "model": "claude-sonnet-4-20250514",
  "repoUrl": "https://github.com/user/repo",
  "dir": "/workspace/repo"
}
```

## Deploy

```bash
npx wrangler deploy
```

Live at: https://chomp-sandbox.coy.workers.dev

## Container

Based on `cloudflare/sandbox:0.7.0` with:
- pi coding agent (`@mariozechner/pi-coding-agent`)
- `chomp` CLI (calls back to Go API for done/handoff/update)
- `run-agent` dispatcher (routes to pi, opencode, shelley)
