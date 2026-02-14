import type { APIRoute } from 'astro'
import { extractToken, resolveUser, jsonResponse, unauthorized } from '../../../lib/auth'

export const GET: APIRoute = async ({ params, locals, request }) => {
  const env = locals.runtime.env as Env

  const token = extractToken(request)
  if (!token) return unauthorized()
  const user = await resolveUser(token, env.JOBS)
  if (!user) return unauthorized()

  const id = params.id
  if (!id) return jsonResponse({ error: 'id required' }, 400)

  const raw = await env.JOBS.get(`job:${token}:${id}`)
  if (!raw) return jsonResponse({ error: 'not found' }, 404)

  return new Response(raw, { headers: { 'Content-Type': 'application/json' } })
}
