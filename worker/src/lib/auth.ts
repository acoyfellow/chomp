/**
 * Auth: chomp tokens are stored in KV as `user:{token}` → `{openrouter_key, created}`
 * Token is a random hex string. User creates one by posting their OpenRouter key.
 */

export interface UserRecord {
  openrouter_key: string
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
  return JSON.parse(raw) as UserRecord
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
