import type { APIRoute } from 'astro'
import { extractToken, resolveUser, getUserKey, getFirstAvailableRouter, jsonResponse, unauthorized } from '../../lib/auth'
import { routers, getRouter, resolveRouterAndModel, callRouter } from '../../lib/routers'

async function pickBestFreeModel(): Promise<string> {
  const resp = await fetch('https://openrouter.ai/api/v1/models')
  const { data } = await resp.json() as { data: { id: string; context_length: number; name: string }[] }
  const free = data
    .filter(m => m.id.endsWith(':free'))
    .filter(m => {
      const name = m.name.toLowerCase()
      const tiny = ['1b', '3b', '7b', '8b'].some(s => name.includes(s))
      const big = ['70b', '80b', '180b'].some(s => name.includes(s))
      return !tiny || big
    })
    .sort((a, b) => b.context_length - a.context_length)
  if (!free.length) throw new Error('No free models available')
  return free[0].id
}

export const POST: APIRoute = async ({ request, locals }) => {
  const env = locals.runtime.env as Env

  const token = extractToken(request)
  if (!token) return unauthorized()
  const user = await resolveUser(token, env.JOBS)
  if (!user) return unauthorized()

  let body: { prompt?: string; model?: string; system?: string; router?: string }
  try {
    body = await request.json()
  } catch {
    return jsonResponse({ error: 'invalid JSON' }, 400)
  }
  if (!body.prompt) {
    return jsonResponse({ error: 'prompt required' }, 400)
  }

  // --- Router resolution chain ---
  let routerId: string | undefined = body.router
  let model = body.model || 'auto'

  // If no explicit router, try extracting from model prefix (e.g. "groq/llama-3.3-70b")
  if (!routerId && model !== 'auto') {
    const resolved = resolveRouterAndModel(model)
    if (resolved.router) {
      routerId = resolved.router
      model = resolved.model
    }
  }

  // If still no router, pick the first one the user has a key for
  if (!routerId) {
    routerId = getFirstAvailableRouter(user, routers.map(r => r.id)) ?? undefined
  }

  if (!routerId) {
    return jsonResponse({ error: 'No router available — configure at least one API key' }, 400)
  }

  const routerDef = getRouter(routerId)
  if (!routerDef) {
    return jsonResponse({ error: `Unknown router: ${routerId}` }, 400)
  }

  // --- Model resolution ---
  if (model === 'auto') {
    if (routerId === 'openrouter') {
      try { model = await pickBestFreeModel() }
      catch (e) {
        return jsonResponse({ error: (e as Error).message }, 502)
      }
    } else {
      model = routerDef.defaultModel
    }
  }

  const id = Date.now().toString(36) + Math.random().toString(36).slice(2, 6)
  const job = {
    id,
    prompt: body.prompt,
    system: body.system || '',
    model,
    router: routerId,
    status: 'running',
    result: '',
    error: '',
    tokens_in: 0,
    tokens_out: 0,
    created: new Date().toISOString(),
    finished: '',
    latency_ms: 0,
  }

  // Scope jobs to user token
  await env.JOBS.put(`job:${token}:${id}`, JSON.stringify(job), { expirationTtl: 86400 })

  // User-scoped job index
  const indexKey = `jobindex:${token}`
  const indexRaw = await env.JOBS.get(indexKey)
  const index: string[] = indexRaw ? JSON.parse(indexRaw) : []
  index.unshift(id)
  if (index.length > 100) index.length = 100
  await env.JOBS.put(indexKey, JSON.stringify(index))

  // Fire LLM call with USER's key for the resolved router
  const ctx = locals.runtime.ctx
  const apiKey = getUserKey(user, routerId)

  ctx.waitUntil((async () => {
    const start = Date.now()
    try {
      if (!apiKey) {
        job.status = 'error'
        job.error = `No ${routerDef.name} key configured`
        job.finished = new Date().toISOString()
        await env.JOBS.put(`job:${token}:${id}`, JSON.stringify(job), { expirationTtl: 86400 })
        return
      }

      const messages: { role: string; content: string }[] = []
      if (body.system) messages.push({ role: 'system', content: body.system })
      messages.push({ role: 'user', content: body.prompt! })

      const data = await callRouter({
        router: routerDef,
        apiKey,
        model,
        messages,
      })

      job.latency_ms = Date.now() - start
      job.finished = new Date().toISOString()

      if (data.error) {
        job.status = 'error'
        job.error = data.error.message || `${routerDef.name} error`
      } else {
        job.status = 'done'
        job.result = data.choices?.[0]?.message?.content || ''
        job.tokens_in = data.usage?.prompt_tokens || 0
        job.tokens_out = data.usage?.completion_tokens || 0
      }
    } catch (e) {
      job.latency_ms = Date.now() - start
      job.finished = new Date().toISOString()
      job.status = 'error'
      job.error = (e as Error).message
    }
    await env.JOBS.put(`job:${token}:${id}`, JSON.stringify(job), { expirationTtl: 86400 })
  })())

  return jsonResponse({ id, model, router: routerId, status: 'running' })
}
