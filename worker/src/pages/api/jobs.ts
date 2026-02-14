import type { APIRoute } from 'astro'
import { extractToken, resolveUser, jsonResponse, unauthorized } from '../../lib/auth'

export const GET: APIRoute = async ({ locals, request }) => {
  const env = locals.runtime.env as Env

  const token = extractToken(request)
  if (!token) return unauthorized()
  const user = await resolveUser(token, env.JOBS)
  if (!user) return unauthorized()

  const indexRaw = await env.JOBS.get(`jobindex:${token}`)
  const index: string[] = indexRaw ? JSON.parse(indexRaw) : []

  const jobs = await Promise.all(
    index.slice(0, 50).map(async (id) => {
      const raw = await env.JOBS.get(`job:${token}:${id}`)
      return raw ? JSON.parse(raw) : null
    })
  )

  return jsonResponse(jobs.filter(Boolean))
}
