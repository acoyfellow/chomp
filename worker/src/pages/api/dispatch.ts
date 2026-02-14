import type { APIRoute } from 'astro'

function auth(request: Request, env: Env): Response | null {
  const token = env.CHOMP_API_TOKEN
  if (!token) return new Response(JSON.stringify({ error: 'API not configured' }), { status: 503 })
  const header = request.headers.get('Authorization') || ''
  if (!header.startsWith('Bearer ') || header.slice(7) !== token) {
    return new Response(JSON.stringify({ error: 'unauthorized' }), {
      status: 401,
      headers: { 'WWW-Authenticate': 'Bearer realm="chomp"' },
    })
  }
  return null
}

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
  const denied = auth(request, env)
  if (denied) return denied

  const body = await request.json() as { prompt?: string; model?: string; system?: string }
  if (!body.prompt) {
    return new Response(JSON.stringify({ error: 'prompt required' }), { status: 400 })
  }

  let model = body.model || 'auto'
  if (model === 'auto') {
    try { model = await pickBestFreeModel() }
    catch (e) {
      return new Response(JSON.stringify({ error: (e as Error).message }), { status: 502 })
    }
  }

  // Generate job ID from timestamp
  const id = Date.now().toString(36) + Math.random().toString(36).slice(2, 6)
  const job = {
    id,
    prompt: body.prompt,
    system: body.system || '',
    model,
    status: 'running',
    result: '',
    error: '',
    tokens_in: 0,
    tokens_out: 0,
    created: new Date().toISOString(),
    finished: '',
    latency_ms: 0,
  }

  // Save job to KV
  await env.JOBS.put(`job:${id}`, JSON.stringify(job), { expirationTtl: 86400 })

  // Add to job index
  const indexRaw = await env.JOBS.get('job:index')
  const index: string[] = indexRaw ? JSON.parse(indexRaw) : []
  index.unshift(id)
  if (index.length > 100) index.length = 100
  await env.JOBS.put('job:index', JSON.stringify(index))

  // Fire the LLM call via waitUntil so we return immediately
  const ctx = locals.runtime.ctx
  ctx.waitUntil((async () => {
    const start = Date.now()
    try {
      const messages: { role: string; content: string }[] = []
      if (body.system) messages.push({ role: 'system', content: body.system })
      messages.push({ role: 'user', content: body.prompt })

      const resp = await fetch('https://openrouter.ai/api/v1/chat/completions', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${env.OPENROUTER_API_KEY}`,
          'Content-Type': 'application/json',
          'HTTP-Referer': 'https://chomp.coey.dev',
          'X-Title': 'chomp',
        },
        body: JSON.stringify({ model, messages }),
      })

      const data = await resp.json() as {
        choices?: { message: { content: string } }[]
        usage?: { prompt_tokens: number; completion_tokens: number }
        error?: { message: string }
      }

      job.latency_ms = Date.now() - start
      job.finished = new Date().toISOString()

      if (!resp.ok || data.error) {
        job.status = 'error'
        job.error = data.error?.message || `OpenRouter ${resp.status}`
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
    await env.JOBS.put(`job:${id}`, JSON.stringify(job), { expirationTtl: 86400 })
  })())

  return new Response(JSON.stringify({ id, model, status: 'running' }), {
    headers: { 'Content-Type': 'application/json' },
  })
}
