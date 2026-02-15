import type { APIRoute } from 'astro'
import {
  extractToken,
  resolveUser,
  getUserKey,
  getFirstAvailableRouter,
  unauthorized,
  jsonResponse,
} from '../../../lib/auth'
import {
  routers,
  getRouter,
  resolveRouterAndModel,
  callRouter,
} from '../../../lib/routers'

const CORS_HEADERS: Record<string, string> = {
  'Access-Control-Allow-Origin': '*',
  'Access-Control-Allow-Methods': 'POST, OPTIONS',
  'Access-Control-Allow-Headers': 'Content-Type, Authorization',
}

function corsJson(data: unknown, status = 200): Response {
  const res = jsonResponse(data, status)
  for (const [k, v] of Object.entries(CORS_HEADERS)) {
    res.headers.set(k, v)
  }
  return res
}

export const OPTIONS: APIRoute = async () => {
  return new Response(null, { status: 204, headers: CORS_HEADERS })
}

export const POST: APIRoute = async ({ request, locals }) => {
  try {
    // 1. Auth
    const token = extractToken(request)
    if (!token) return unauthorized()

    const kv = locals.runtime.env.JOBS
    const user = await resolveUser(token, kv)
    if (!user) return unauthorized()

    // 2. Parse body
    interface ChatCompletionRequest {
      model: string
      messages: Array<{ role: string; content: string }>
      temperature?: number
      max_tokens?: number
      router?: string
    }

    let body: ChatCompletionRequest
    try {
      body = await request.json()
    } catch {
      return corsJson({ error: { message: 'invalid JSON body', type: 'invalid_request_error' } }, 400)
    }

    if (!body.messages || !Array.isArray(body.messages) || body.messages.length === 0) {
      return corsJson(
        { error: { message: 'messages array is required and must not be empty', type: 'invalid_request_error' } },
        400,
      )
    }

    // 3. Resolve router
    let routerId: string | undefined = body.router
    let model: string = body.model ?? ''

    if (!routerId) {
      const resolved = resolveRouterAndModel(model)
      routerId = resolved.router
      model = resolved.model
    }

    if (!routerId) {
      routerId = getFirstAvailableRouter(user, routers.map((r) => r.id)) ?? undefined
    }

    if (!routerId) {
      return corsJson(
        { error: { message: 'unable to resolve a router — no keys configured', type: 'invalid_request_error' } },
        400,
      )
    }

    const routerDef = getRouter(routerId)
    if (!routerDef) {
      return corsJson(
        { error: { message: `unknown router: ${routerId}`, type: 'invalid_request_error' } },
        400,
      )
    }

    // 4. Resolve model
    if (!model || model === 'auto') {
      model = routerDef.defaultModel
    }

    // 5. Get API key
    const apiKey = getUserKey(user, routerId)
    if (!apiKey) {
      return corsJson(
        { error: { message: `no key for router ${routerId}`, type: 'authentication_error' } },
        401,
      )
    }

    // 6. Call upstream with 120s timeout
    const controller = new AbortController()
    const timeout = setTimeout(() => controller.abort(), 120_000)
    const start = Date.now()

    let result
    try {
      result = await callRouter({
        router: routerDef,
        apiKey,
        model,
        messages: body.messages,
        signal: controller.signal,
      })
    } catch (err: unknown) {
      clearTimeout(timeout)
      if (err instanceof DOMException && err.name === 'AbortError') {
        return corsJson({ error: { message: 'upstream timeout', type: 'timeout' } }, 504)
      }
      throw err
    }
    clearTimeout(timeout)

    const latencyMs = Date.now() - start

    // 7–8. Return response (pass through upstream errors as-is)
    return corsJson({
      ...result,
      chomp: { router: routerId, latency_ms: latencyMs },
    }, result.error ? 502 : 200)
  } catch (err: unknown) {
    // 10. Unexpected errors
    const message = err instanceof Error ? err.message : 'internal server error'
    return corsJson({ error: { message, type: 'internal_error' } }, 500)
  }
}
