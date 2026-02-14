import type { APIRoute } from "astro"
import type { ExecutionContext } from "@cloudflare/workers-types"
import { WebStandardStreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/webStandardStreamableHttp.js"
import { createMcpServer } from "../mcp/server.js"
import { extractToken } from "../lib/auth.js"

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

async function handleMcp(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
  const token = extractToken(request)
  if (!token) {
    return new Response(JSON.stringify({ error: "unauthorized" }), {
      status: 401,
      headers: { "Content-Type": "application/json" },
    })
  }

  const server = createMcpServer({ token, kv: env.JOBS, ctx })
  const transport = new WebStandardStreamableHTTPServerTransport({
    sessionIdGenerator: undefined, // stateless — CF Workers are request-scoped
  })

  await server.server.connect(transport)

  try {
    return await transport.handleRequest(request)
  } finally {
    await transport.close()
    await server.close()
  }
}

// ---------------------------------------------------------------------------
// Astro API route exports
// ---------------------------------------------------------------------------

export const POST: APIRoute = async ({ request, locals }) => {
  const env = locals.runtime.env as Env
  const ctx = locals.runtime.ctx
  return handleMcp(request, env, ctx)
}

export const GET: APIRoute = async ({ request, locals }) => {
  const env = locals.runtime.env as Env
  const ctx = locals.runtime.ctx
  return handleMcp(request, env, ctx)
}

export const DELETE: APIRoute = async ({ request, locals }) => {
  const env = locals.runtime.env as Env
  const ctx = locals.runtime.ctx
  return handleMcp(request, env, ctx)
}
