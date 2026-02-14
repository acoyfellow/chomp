import type { ExecutionContext } from "@cloudflare/workers-types"
import { Context, Effect, Layer, Schedule } from "effect"
import {
  AuthError,
  DispatchError,
  JobNotFoundError,
  JobPendingError,
  PollError,
  ModelError,
} from "./errors.js"
import type { Job } from "./schemas.js"

// ---------------------------------------------------------------------------
// OpenRouter types
// ---------------------------------------------------------------------------

interface OpenRouterModel {
  id: string
  name: string
  context_length: number
  top_provider?: { max_completion_tokens?: number }
}

interface ChatCompletionResponse {
  choices?: { message: { content: string } }[]
  usage?: { prompt_tokens: number; completion_tokens: number }
  error?: { message: string }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const resolveUser = (token: string, kv: KVNamespace) =>
  Effect.tryPromise({
    try: () => kv.get(`user:${token}`),
    catch: () => new AuthError({ message: "KV lookup failed" }),
  }).pipe(
    Effect.flatMap((raw) =>
      raw
        ? Effect.succeed(JSON.parse(raw) as { openrouter_key: string; created: string })
        : Effect.fail(new AuthError({ message: "Invalid token" }))
    )
  )

const fetchFreeModels = () =>
  Effect.tryPromise({
    try: async () => {
      const resp = await fetch("https://openrouter.ai/api/v1/models")
      if (!resp.ok) throw new Error(`OpenRouter returned ${resp.status}`)
      const { data } = (await resp.json()) as { data: OpenRouterModel[] }
      return data
    },
    catch: (e) => new ModelError({ message: String(e) }),
  }).pipe(
    Effect.map((data) =>
      data
        .filter((m) => m.id.endsWith(":free"))
        .filter((m) => {
          const name = m.name.toLowerCase()
          const tiny = ["1b", "3b", "7b", "8b"].some((s) => name.includes(s))
          const big = ["70b", "80b", "180b"].some((s) => name.includes(s))
          return !tiny || big
        })
        .sort((a, b) => b.context_length - a.context_length)
    )
  )

const pickBestFreeModel = () =>
  fetchFreeModels().pipe(
    Effect.flatMap((models) =>
      models.length > 0
        ? Effect.succeed(models[0].id)
        : Effect.fail(new ModelError({ message: "No free models available" }))
    ),
    Effect.mapError(
      (e) =>
        new DispatchError({
          message: e instanceof ModelError ? e.message : String(e),
          statusCode: 502,
        })
    )
  )

// ---------------------------------------------------------------------------
// Service definition
// ---------------------------------------------------------------------------

export class ChompService extends Context.Tag("ChompService")<
  ChompService,
  {
    readonly dispatch: (params: {
      prompt: string
      model?: string
      system?: string
      token: string
      kv: KVNamespace
      ctx: ExecutionContext
    }) => Effect.Effect<
      { id: string; model: string; status: string },
      AuthError | DispatchError
    >

    readonly getResult: (params: {
      jobId: string
      token: string
      kv: KVNamespace
    }) => Effect.Effect<Job, AuthError | JobNotFoundError | PollError>

    readonly pollUntilDone: (params: {
      jobId: string
      token: string
      kv: KVNamespace
    }) => Effect.Effect<Job, AuthError | JobNotFoundError | PollError>

    readonly listFreeModels: () => Effect.Effect<
      Array<{ id: string; name: string; context_length: number; max_output: number }>,
      ModelError
    >
  }
>() {}

// ---------------------------------------------------------------------------
// Implementation
// ---------------------------------------------------------------------------

const dispatch: ChompService["Type"]["dispatch"] = (params) =>
  Effect.gen(function* () {
    const { prompt, system, token, kv, ctx } = params

    // 1. Authenticate
    const user = yield* resolveUser(token, kv)

    // 2. Resolve model
    let model = params.model || "auto"
    if (model === "auto") {
      model = yield* pickBestFreeModel()
    }

    // 3. Generate job ID
    const id =
      Date.now().toString(36) + Math.random().toString(36).slice(2, 6)

    // 4. Build job record
    const job = {
      id,
      prompt,
      system: system || "",
      model,
      status: "running" as string,
      result: "",
      error: "",
      tokens_in: 0,
      tokens_out: 0,
      created: new Date().toISOString(),
      finished: "",
      latency_ms: 0,
    }

    // 5. Persist job to KV
    yield* Effect.tryPromise({
      try: () =>
        kv.put(`job:${token}:${id}`, JSON.stringify(job), {
          expirationTtl: 86400,
        }),
      catch: (e) =>
        new DispatchError({ message: `KV put failed: ${e}`, statusCode: 500 }),
    })

    // 6. Update job index
    yield* Effect.tryPromise({
      try: async () => {
        const indexKey = `jobindex:${token}`
        const raw = await kv.get(indexKey)
        const index: string[] = raw ? JSON.parse(raw) : []
        index.unshift(id)
        if (index.length > 100) index.length = 100
        await kv.put(indexKey, JSON.stringify(index))
      },
      catch: (e) =>
        new DispatchError({
          message: `Job index update failed: ${e}`,
          statusCode: 500,
        }),
    })

    // 7. Fire LLM call via waitUntil (non-blocking for dispatch)
    const userKey = user.openrouter_key
    ctx.waitUntil(
      (async () => {
        const start = Date.now()
        try {
          const messages: { role: string; content: string }[] = []
          if (system) messages.push({ role: "system", content: system })
          messages.push({ role: "user", content: prompt })

          const resp = await fetch(
            "https://openrouter.ai/api/v1/chat/completions",
            {
              method: "POST",
              headers: {
                Authorization: `Bearer ${userKey}`,
                "Content-Type": "application/json",
                "HTTP-Referer": "https://chomp.coey.dev",
                "X-Title": "chomp",
              },
              body: JSON.stringify({ model, messages }),
            }
          )

          const data = (await resp.json()) as ChatCompletionResponse

          job.latency_ms = Date.now() - start
          job.finished = new Date().toISOString()

          if (!resp.ok || data.error) {
            job.status = "error"
            job.error = data.error?.message || `OpenRouter ${resp.status}`
          } else {
            job.status = "done"
            job.result = data.choices?.[0]?.message?.content || ""
            job.tokens_in = data.usage?.prompt_tokens || 0
            job.tokens_out = data.usage?.completion_tokens || 0
          }
        } catch (e) {
          job.latency_ms = Date.now() - start
          job.finished = new Date().toISOString()
          job.status = "error"
          job.error = (e as Error).message
        }
        await kv.put(`job:${token}:${id}`, JSON.stringify(job), {
          expirationTtl: 86400,
        })
      })()
    )

    return { id, model, status: "running" }
  })

const getResult: ChompService["Type"]["getResult"] = (params) =>
  Effect.gen(function* () {
    const { jobId, token, kv } = params

    // 1. Authenticate
    yield* resolveUser(token, kv)

    // 2. Read job from KV
    const raw = yield* Effect.tryPromise({
      try: () => kv.get(`job:${token}:${jobId}`),
      catch: (e) =>
        new PollError({ message: `KV read failed: ${e}`, jobId }),
    })

    if (!raw) {
      return yield* new JobNotFoundError({ jobId })
    }

    return JSON.parse(raw) as Job
  })

const pollUntilDone: ChompService["Type"]["pollUntilDone"] = (params) => {
  const pollOnce = getResult(params).pipe(
    Effect.flatMap((job) => {
      if (job.status === "running") {
        return Effect.fail(
          new JobPendingError({ jobId: params.jobId, status: job.status })
        )
      }
      return Effect.succeed(job)
    })
  )

  return pollOnce.pipe(
    Effect.retry({
      while: (e): e is JobPendingError => e._tag === "JobPendingError",
      schedule: Schedule.exponential("1 second").pipe(
        Schedule.intersect(Schedule.recurs(10))
      ),
    }),
    Effect.timeout("60 seconds"),
    Effect.flatMap((option) =>
      option !== undefined
        ? Effect.succeed(option)
        : Effect.fail(
            new PollError({
              message: "Poll timed out after 60 seconds",
              jobId: params.jobId,
            })
          )
    )
  ) as Effect.Effect<Job, AuthError | JobNotFoundError | PollError>
}

const listFreeModels: ChompService["Type"]["listFreeModels"] = () =>
  fetchFreeModels().pipe(
    Effect.map((models) =>
      models.map((m) => ({
        id: m.id,
        name: m.name,
        context_length: m.context_length,
        max_output: m.top_provider?.max_completion_tokens || 0,
      }))
    )
  )

// ---------------------------------------------------------------------------
// Live layer
// ---------------------------------------------------------------------------

export const ChompServiceLive = Layer.succeed(ChompService, {
  dispatch,
  getResult,
  pollUntilDone,
  listFreeModels,
})
