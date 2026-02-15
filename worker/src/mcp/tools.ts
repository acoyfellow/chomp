import { Effect } from "effect"
import type { ExecutionContext } from "@cloudflare/workers-types"
import { ChompService } from "./services.js"
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js"
import type {
  AuthError,
  DispatchError,
  PollError,
  JobNotFoundError,
  ModelError,
} from "./errors.js"

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const toMcpError = (tag: string, message: string): CallToolResult => ({
  content: [{ type: "text", text: `[${tag}] ${message}` }],
  isError: true,
})

type ToolError = AuthError | DispatchError | PollError | JobNotFoundError | ModelError

const catchAll = <R>(effect: Effect.Effect<CallToolResult, ToolError, R>) =>
  effect.pipe(
    Effect.catchTags({
      AuthError: (e) => Effect.succeed(toMcpError("AuthError", e.message)),
      DispatchError: (e) => Effect.succeed(toMcpError("DispatchError", e.message)),
      PollError: (e) => Effect.succeed(toMcpError("PollError", e.message)),
      JobNotFoundError: (e) => Effect.succeed(toMcpError("JobNotFoundError", `Job ${e.jobId} not found`)),
      ModelError: (e) => Effect.succeed(toMcpError("ModelError", e.message)),
    })
  )

// ---------------------------------------------------------------------------
// Tools
// ---------------------------------------------------------------------------

/**
 * ask — dispatch a prompt, poll until done, return the text result.
 */
export const ask = (
  params: { prompt: string; model?: string; system?: string; router?: string },
  token: string,
  kv: KVNamespace,
  ctx: ExecutionContext,
) =>
  catchAll(
    Effect.gen(function* () {
      const svc = yield* ChompService
      const dispatched = yield* svc.dispatch({
        prompt: params.prompt,
        model: params.model,
        system: params.system,
        router: params.router,
        token,
        kv,
        ctx,
      })
      const job = yield* svc.pollUntilDone({
        jobId: dispatched.id,
        token,
        kv,
      })
      if (job.status === "error") {
        return {
          content: [{ type: "text" as const, text: `Error: ${job.error}` }],
          isError: true,
        } satisfies CallToolResult
      }
      return {
        content: [{ type: "text" as const, text: job.result }],
        _meta: {
          model: job.model,
          tokens_in: job.tokens_in,
          tokens_out: job.tokens_out,
          latency_ms: job.latency_ms,
        },
      } satisfies CallToolResult
    })
  )

/**
 * dispatch — fire-and-forget, return job ID immediately.
 */
export const dispatch = (
  params: { prompt: string; model?: string; system?: string; router?: string },
  token: string,
  kv: KVNamespace,
  ctx: ExecutionContext,
) =>
  catchAll(
    Effect.gen(function* () {
      const svc = yield* ChompService
      const result = yield* svc.dispatch({
        prompt: params.prompt,
        model: params.model,
        system: params.system,
        router: params.router,
        token,
        kv,
        ctx,
      })
      return {
        content: [{ type: "text" as const, text: JSON.stringify(result) }],
      } satisfies CallToolResult
    })
  )

/**
 * result — poll a single job by ID.
 */
export const result = (
  params: { jobId: string },
  token: string,
  kv: KVNamespace,
) =>
  catchAll(
    Effect.gen(function* () {
      const svc = yield* ChompService
      const job = yield* svc.getResult({ jobId: params.jobId, token, kv })
      return {
        content: [{ type: "text" as const, text: JSON.stringify(job) }],
      } satisfies CallToolResult
    })
  )
