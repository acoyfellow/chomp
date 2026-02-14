import type { APIRoute } from 'astro'

export const GET: APIRoute = async ({ params, locals, request }) => {
  const env = locals.runtime.env as Env
  const token = env.CHOMP_API_TOKEN
  if (!token) return new Response(JSON.stringify({ error: 'API not configured' }), { status: 503 })
  const header = request.headers.get('Authorization') || ''
  if (!header.startsWith('Bearer ') || header.slice(7) !== token) {
    return new Response(JSON.stringify({ error: 'unauthorized' }), { status: 401 })
  }

  const id = params.id
  if (!id) return new Response(JSON.stringify({ error: 'id required' }), { status: 400 })

  const raw = await env.JOBS.get(`job:${id}`)
  if (!raw) return new Response(JSON.stringify({ error: 'not found' }), { status: 404 })

  return new Response(raw, { headers: { 'Content-Type': 'application/json' } })
}
