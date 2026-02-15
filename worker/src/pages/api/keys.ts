import type { APIRoute } from 'astro'
import { extractToken, resolveUser, jsonResponse, unauthorized } from '../../lib/auth'

function generateToken(): string {
  const bytes = new Uint8Array(32)
  crypto.getRandomValues(bytes)
  return Array.from(bytes).map(b => b.toString(16).padStart(2, '0')).join('')
}

function previewKey(key: string): string {
  if (key.length <= 8) return key.slice(0, 2) + '...' + key.slice(-2)
  return key.slice(0, 4) + '...' + key.slice(-4)
}

export const POST: APIRoute = async ({ request, locals }) => {
  const env = locals.runtime.env as Env

  let body: { openrouter_key?: string; keys?: Record<string, string> }
  try {
    body = await request.json()
  } catch {
    return jsonResponse({ error: 'invalid JSON' }, 400)
  }

  let keys: Record<string, string>

  if (body.keys && typeof body.keys === 'object') {
    // New format: { keys: { groq: "gsk_...", openrouter: "sk-or-..." } }
    keys = {}
    for (const [routerId, apiKey] of Object.entries(body.keys)) {
      if (typeof apiKey !== 'string' || !apiKey.trim()) continue
      keys[routerId] = apiKey.trim()
    }
    if (Object.keys(keys).length === 0) {
      return jsonResponse({ error: 'at least one key required' }, 400)
    }
  } else if (body.openrouter_key) {
    // Old format: { openrouter_key: "sk-or-..." }
    const key = body.openrouter_key.trim()
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

    keys = { openrouter: key }
  } else {
    return jsonResponse({ error: 'at least one key required' }, 400)
  }

  const token = generateToken()
  const record = { keys, created: new Date().toISOString() }

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

  const previews: Record<string, string> = {}
  for (const [routerId, apiKey] of Object.entries(user.keys)) {
    previews[routerId] = previewKey(apiKey)
  }

  return jsonResponse({ keys: previews, created: user.created })
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
