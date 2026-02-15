/**
 * Auth: chomp tokens are stored in KV as `user:{token}` → `{keys, created}`
 * Token is a random hex string. User creates one by posting their API key(s).
 *
 * Legacy records stored `{openrouter_key, created}` — resolveUser normalises
 * those to the new multi-key shape automatically.
 */

export interface UserRecord {
  keys: Record<string, string> // routerId → apiKey, e.g. { "openrouter": "sk-or-...", "groq": "gsk_..." }
  created: string
}

export function extractToken(request: Request): string | null {
  const header = request.headers.get('Authorization') || ''
  if (!header.startsWith('Bearer ')) return null
  return header.slice(7) || null
}

export async function resolveUser(token: string, kv: KVNamespace): Promise<UserRecord | null> {
  const raw = await kv.get(`user:${token}`)
  if (!raw) return null
  const parsed = JSON.parse(raw)

  // Normalise legacy format { openrouter_key, created } → { keys, created }
  if (parsed.openrouter_key && !parsed.keys) {
    return { keys: { openrouter: parsed.openrouter_key }, created: parsed.created }
  }

  return parsed as UserRecord
}

/** Return the user's API key for the given router, or null if they don't have one. */
export function getUserKey(user: UserRecord, routerId: string): string | null {
  return user.keys[routerId] ?? null
}

/** Return the first router ID (from the ordered list) that the user has a key for, or null. */
export function getFirstAvailableRouter(user: UserRecord, routerIds: string[]): string | null {
  for (const id of routerIds) {
    if (user.keys[id]) return id
  }
  return null
}

export function unauthorized(): Response {
  return new Response(JSON.stringify({ error: 'unauthorized' }), {
    status: 401,
    headers: { 'Content-Type': 'application/json', 'WWW-Authenticate': 'Bearer realm="chomp"' },
  })
}

export function jsonResponse(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}
