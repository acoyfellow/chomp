#!/usr/bin/env node
/**
 * MCP smoke test — connects to the local MCP endpoint, verifies tools are listed.
 * Usage: node test-mcp.mjs [url]
 * Default URL: http://localhost:4321/mcp
 *
 * Exit 0 = pass, exit 1 = fail
 */
import { Client } from "@modelcontextprotocol/sdk/client/index.js"
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js"

const MCP_URL = process.argv[2] || "http://localhost:4321/mcp"
const EXPECTED_TOOLS = ["ask", "dispatch", "result"]

async function main() {
  console.log(`  mcp smoke test → ${MCP_URL}`)

  const transport = new StreamableHTTPClientTransport(new URL(MCP_URL), {
    requestInit: {
      headers: {
        "Authorization": "Bearer test-token",
      },
    },
  })

  const client = new Client({ name: "chomp-test", version: "1.0.0" })

  try {
    await client.connect(transport)
  } catch (e) {
    // MCP connect sends initialize — if server responds, we're good
    // Some stateless servers may error on session, but tool listing works
    console.error(`  ⚠ connect issue: ${e.message}`)
  }

  let tools
  try {
    const result = await client.listTools()
    tools = result.tools.map(t => t.name)
  } catch (e) {
    console.error(`  ✗ listTools failed: ${e.message}`)
    process.exit(1)
  }

  const missing = EXPECTED_TOOLS.filter(t => !tools.includes(t))
  if (missing.length > 0) {
    console.error(`  ✗ missing tools: ${missing.join(", ")}`)
    console.error(`  found: ${tools.join(", ")}`)
    process.exit(1)
  }

  console.log(`  ✓ ${tools.length} tools: ${tools.join(", ")}`)

  // Verify each tool has a description and input schema
  const fullResult = await client.listTools()
  for (const tool of fullResult.tools) {
    if (!tool.description) {
      console.error(`  ✗ tool "${tool.name}" has no description`)
      process.exit(1)
    }
    if (!tool.inputSchema) {
      console.error(`  ✗ tool "${tool.name}" has no inputSchema`)
      process.exit(1)
    }
  }
  console.log("  ✓ all tools have descriptions and schemas")

  try { await client.close() } catch {}
  process.exit(0)
}

main().catch((e) => {
  console.error(`  ✗ ${e.message}`)
  process.exit(1)
})
