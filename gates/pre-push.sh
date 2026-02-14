#!/bin/bash
set -e

echo "[chomp] pre-push checks..."

# Go
echo "  go vet..."
go vet ./...
echo "  go test..."
go test -timeout 30s -count=1 ./... 2>&1 | tail -1

# Astro
if command -v bun &>/dev/null || [ -x "$HOME/.bun/bin/bun" ]; then
  export PATH="$HOME/.bun/bin:$PATH"
  echo "  tsc --noEmit..."
  cd worker && bunx tsc --noEmit && cd ..
  echo "  astro build..."
  cd worker && bun run build 2>&1 | tail -1 && cd ..
else
  echo "  tsc --noEmit..."
  cd worker && npx tsc --noEmit && cd ..
  echo "  astro build..."
  cd worker && npm run build 2>&1 | tail -1 && cd ..
fi

# MCP smoke test — start dev server, verify tools, kill
echo "  mcp smoke test..."
MCP_PORT=4399
cd worker
npx astro dev --port $MCP_PORT > /dev/null 2>&1 &
MCP_PID=$!
cd ..

# Wait for server to be ready (max 15s)
for i in $(seq 1 30); do
  if curl -sf http://localhost:$MCP_PORT/ > /dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

# Run MCP client test
if timeout 15 node worker/test-mcp.mjs http://localhost:$MCP_PORT/mcp; then
  MCP_OK=1
else
  MCP_OK=0
fi

# Cleanup
kill $MCP_PID 2>/dev/null || true
wait $MCP_PID 2>/dev/null || true

if [ "$MCP_OK" != "1" ]; then
  echo "[chomp] \u2717 MCP smoke test failed"
  exit 1
fi

echo "[chomp] \u2705 all checks passed"
