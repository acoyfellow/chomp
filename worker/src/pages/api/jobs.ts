import type { APIRoute } from 'astro'

export const GET: APIRoute = async ({ locals, request }) => {
  const env = locals.runtime.env as Env
  const token = env.CHOMP_API_TOKEN
  if (!token) return new Response(JSON.stringify({ error: 'API not configured' }), { status: 503 })
  const header = request.headers.get('Authorization') || ''
  if (!header.startsWith('Bearer ') || header.slice(7) !== token) {
    return new Response(JSON.stringify({ error: 'unauthorized' }), { status: 401 })
  }

  const indexRaw = await env.JOBS.get('job:index')
  const index: string[] = indexRaw ? JSON.parse(indexRaw) : []

  const jobs = await Promise.all(
    index.slice(0, 50).map(async (id) => {
      const raw = await env.JOBS.get(`job:${id}`)
      return raw ? JSON.parse(raw) : null
    })
  )

  return new Response(JSON.stringify(jobs.filter(Boolean)), {
    headers: { 'Content-Type': 'application/json' },
  })
}
