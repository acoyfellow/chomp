// Shared router infrastructure for OpenAI-compatible API providers

export interface RouterDef {
  id: string
  name: string
  baseUrl: string
  defaultModel: string
  headers?: Record<string, string>
}

export const routers: readonly RouterDef[] = [
  {
    id: "zen",
    name: "OpenCode Zen",
    baseUrl: "https://opencode.ai/zen/v1",
    defaultModel: "minimax-m2.5-free",
  },
  {
    id: "groq",
    name: "Groq",
    baseUrl: "https://api.groq.com/openai/v1",
    defaultModel: "llama-3.3-70b-versatile",
  },
  {
    id: "cerebras",
    name: "Cerebras",
    baseUrl: "https://api.cerebras.ai/v1",
    defaultModel: "llama-3.3-70b",
  },
  {
    id: "sambanova",
    name: "SambaNova",
    baseUrl: "https://api.sambanova.ai/v1",
    defaultModel: "Meta-Llama-3.3-70B-Instruct",
  },
  {
    id: "fireworks",
    name: "Fireworks",
    baseUrl: "https://api.fireworks.ai/inference/v1",
    defaultModel: "accounts/fireworks/models/llama-v3p3-70b-instruct",
  },
  {
    id: "openrouter",
    name: "OpenRouter",
    baseUrl: "https://openrouter.ai/api/v1",
    defaultModel: "auto",
    headers: {
      "HTTP-Referer": "https://chomp.coey.dev",
      "X-Title": "chomp",
    },
  },
] as const

export function getRouter(id: string): RouterDef | undefined {
  return routers.find((r) => r.id === id)
}

export function resolveRouterAndModel(input: string): {
  router: string | undefined
  model: string
} {
  const slashIndex = input.indexOf("/")
  if (slashIndex === -1) {
    return { router: undefined, model: input }
  }
  const maybeRouter = input.slice(0, slashIndex)
  const maybeModel = input.slice(slashIndex + 1)
  // Only treat as router/model if the prefix matches a known router
  if (routers.some((r) => r.id === maybeRouter)) {
    return { router: maybeRouter, model: maybeModel }
  }
  // Not a known router prefix — treat entire input as the model
  // (handles models like "accounts/fireworks/models/...")
  return { router: undefined, model: input }
}

export interface OpenAIResponse {
  id: string
  object: string
  created: number
  model: string
  choices: Array<{
    index: number
    message: { role: string; content: string | null }
    finish_reason: string | null
  }>
  usage?: {
    prompt_tokens: number
    completion_tokens: number
    total_tokens: number
  }
  error?: {
    message: string
    type?: string
    code?: string | number | null
  }
}

export async function callRouter(params: {
  router: RouterDef
  apiKey: string
  model: string
  messages: Array<{ role: string; content: string }>
  signal?: AbortSignal
}): Promise<OpenAIResponse> {
  const { router, apiKey, model, messages, signal } = params

  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    Authorization: `Bearer ${apiKey}`,
    ...router.headers,
  }

  const response = await fetch(`${router.baseUrl}/chat/completions`, {
    method: "POST",
    headers,
    body: JSON.stringify({ model, messages }),
    signal,
  })

  if (!response.ok) {
    const text = await response.text().catch(() => "")
    let parsed: OpenAIResponse | undefined
    try {
      parsed = JSON.parse(text) as OpenAIResponse
    } catch {
      // not JSON
    }
    if (parsed?.error) {
      return parsed
    }
    return {
      id: "",
      object: "error",
      created: 0,
      model,
      choices: [],
      error: {
        message: text || `HTTP ${response.status} ${response.statusText}`,
        type: "api_error",
        code: response.status,
      },
    }
  }

  return (await response.json()) as OpenAIResponse
}
