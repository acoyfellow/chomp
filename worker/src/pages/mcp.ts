import type { APIRoute } from "astro"
import { WebStandardStreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/webStandardStreamableHttp.js"
import { createMcpServer } from "../mcp/server.js"
import { extractToken } from "../lib/auth.js"

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

async function handleMcp(request: Request, locals: App.Locals): Promise<Response> {
  const token = extractToken(request)
  if (!token) {
    return new Response(JSON.stringify({ error: "unauthorized" }), {
      status: 401,
      headers: { "Content-Type": "application/json" },
    })
  }

  const env = locals.runtime.env as Env
  const ctx = locals.runtime.ctx
  const server = createMcpServer({ token, kv: env.JOBS, ctx })

  const transport = new WebStandardStreamableHTTPServerTransport({
    sessionIdGenerator: undefined, // stateless — CF Workers are request-scoped
  })

  await server.connect(transport)

  const response = await transport.handleRequest(request)

  // If SSE stream, clean up when the stream finishes (not before)
  if (response.headers.get("content-type")?.includes("text/event-stream") && response.body) {
    const originalBody = response.body
    const cleanup = new TransformStream({
      flush: async () => {
        await transport.close()
        await server.close()
      },
    })
    const newBody = originalBody.pipeThrough(cleanup)
    return new Response(newBody, {
      status: response.status,
      headers: response.headers,
    })
  }

  // JSON response — clean up immediately
  await transport.close()
  await server.close()
  return response
}

// ---------------------------------------------------------------------------
// Astro API route exports
// ---------------------------------------------------------------------------

export const POST: APIRoute = async ({ request, locals }) => {
  return handleMcp(request, locals)
}

export const GET: APIRoute = async ({ request, locals }) => {
  return handleMcp(request, locals)
}

export const DELETE: APIRoute = async ({ request, locals }) => {
  return handleMcp(request, locals)
}
