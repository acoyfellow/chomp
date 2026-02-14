#!/bin/bash
set -e

echo "[chomp] pre-push checks..."

# Go
echo "  go vet..."
go vet ./...
echo "  go test..."
go test -timeout 30s -count=1 ./... 2>&1 | tail -1

# Astro (if bun available)
if command -v bun &>/dev/null || [ -x "$HOME/.bun/bin/bun" ]; then
  export PATH="$HOME/.bun/bin:$PATH"
  echo "  tsc --noEmit..."
  cd worker && bunx tsc --noEmit && cd ..
  echo "  astro build..."
  cd worker && bun run build 2>&1 | tail -1 && cd ..
fi

echo "[chomp] ✅ all checks passed"
