import type { APIRoute } from 'astro'
import { extractToken, resolveUser, jsonResponse, unauthorized } from '../../lib/auth'

/**
 * POST /api/keys — register your OpenRouter key, get a chomp token
 * Body: { "openrouter_key": "sk-or-..." }
 * Returns: { "token": "..." }
 *
 * GET /api/keys — check your key status (requires auth)
 * Returns: { "key_preview": "sk-or-...xxxx", "created": "..." }
 *
 * DELETE /api/keys — revoke your token
 */

function generateToken(): string {
  const bytes = new Uint8Array(32)
  crypto.getRandomValues(bytes)
  return Array.from(bytes).map(b => b.toString(16).padStart(2, '0')).join('')
}

export const POST: APIRoute = async ({ request, locals }) => {
  const env = locals.runtime.env as Env

  let body: { openrouter_key?: string }
  try {
    body = await request.json()
  } catch {
    return jsonResponse({ error: 'invalid JSON' }, 400)
  }

  const key = body.openrouter_key?.trim()
  if (!key || !key.startsWith('sk-or-')) {
    return jsonResponse({ error: 'openrouter_key required (must start with sk-or-)' }, 400)
  }

  // Validate the key against OpenRouter
  const check = await fetch('https://openrouter.ai/api/v1/auth/key', {
    headers: { 'Authorization': `Bearer ${key}` },
  })
  if (!check.ok) {
    return jsonResponse({ error: 'invalid OpenRouter key' }, 400)
  }

  const token = generateToken()
  const record = { openrouter_key: key, created: new Date().toISOString() }

  // Store user record (no expiry — persists until deleted)
  await env.JOBS.put(`user:${token}`, JSON.stringify(record))

  return jsonResponse({ token, created: record.created })
}

export const GET: APIRoute = async ({ request, locals }) => {
  const env = locals.runtime.env as Env
  const token = extractToken(request)
  if (!token) return unauthorized()

  const user = await resolveUser(token, env.JOBS)
  if (!user) return unauthorized()

  // Show only last 4 chars of key
  const preview = user.openrouter_key.slice(0, 8) + '...' + user.openrouter_key.slice(-4)
  return jsonResponse({ key_preview: preview, created: user.created })
}

export const DELETE: APIRoute = async ({ request, locals }) => {
  const env = locals.runtime.env as Env
  const token = extractToken(request)
  if (!token) return unauthorized()

  const user = await resolveUser(token, env.JOBS)
  if (!user) return unauthorized()

  await env.JOBS.delete(`user:${token}`)
  return jsonResponse({ deleted: true })
}
