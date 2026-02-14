import type { APIRoute } from 'astro'

interface OpenRouterModel {
  id: string
  name: string
  context_length: number
  top_provider?: { max_completion_tokens?: number }
  created?: number
}

export const GET: APIRoute = async ({ locals }) => {
  const resp = await fetch('https://openrouter.ai/api/v1/models')
  if (!resp.ok) {
    return new Response(JSON.stringify({ error: 'Failed to fetch models' }), { status: 502 })
  }

  const { data } = await resp.json() as { data: OpenRouterModel[] }

  const free = data
    .filter(m => m.id.endsWith(':free'))
    .filter(m => {
      const name = m.name.toLowerCase()
      const tiny = ['1b', '3b', '7b', '8b'].some(s => name.includes(s))
      const big = ['70b', '80b', '180b'].some(s => name.includes(s))
      return !tiny || big
    })
    .map(m => ({
      id: m.id,
      name: m.name,
      context_length: m.context_length,
      max_output: m.top_provider?.max_completion_tokens || 0,
    }))
    .sort((a, b) => b.context_length - a.context_length)

  return new Response(JSON.stringify({ count: free.length, models: free }), {
    headers: { 'Content-Type': 'application/json', 'Cache-Control': 'public, max-age=900' },
  })
}
