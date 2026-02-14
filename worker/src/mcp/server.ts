import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js"
import { Effect } from "effect"
import type { ExecutionContext } from "@cloudflare/workers-types"
import { ChompService, ChompServiceLive } from "./services.js"
import { AskParamsZod, DispatchParamsZod, ResultParamsZod } from "./schemas.js"
import * as tools from "./tools.js"

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

export function createMcpServer(deps: {
  token: string
  kv: KVNamespace
  ctx: ExecutionContext
}) {
  const server = new McpServer({ name: "chomp", version: "1.0.0" })

  /** Run an Effect program that only needs ChompService → Promise<A>. */
  const runTool = <A>(effect: Effect.Effect<A, never, ChompService>): Promise<A> =>
    Effect.runPromise(effect.pipe(Effect.provide(ChompServiceLive)))

  // -------------------------------------------------------------------------
  // ask
  // -------------------------------------------------------------------------
  server.registerTool(
    "ask",
    {
      description:
        "Send a prompt to a free AI model and get the response. " +
        "Dispatches to the best available free model, waits up to 60s for completion.",
      inputSchema: {
        prompt: AskParamsZod.shape.prompt.describe("The prompt to send"),
        model: AskParamsZod.shape.model.describe("Model ID or 'auto'"),
        system: AskParamsZod.shape.system.describe("System prompt"),
      },
    },
    async (args) => runTool(tools.ask(args, deps.token, deps.kv, deps.ctx)),
  )

  // -------------------------------------------------------------------------
  // dispatch
  // -------------------------------------------------------------------------
  server.registerTool(
    "dispatch",
    {
      description:
        "Fire-and-forget prompt dispatch. Returns a job ID immediately. " +
        "Use the 'result' tool to poll for completion.",
      inputSchema: {
        prompt: DispatchParamsZod.shape.prompt.describe("The prompt to send"),
        model: DispatchParamsZod.shape.model.describe("Model ID or 'auto'"),
        system: DispatchParamsZod.shape.system.describe("System prompt"),
      },
    },
    async (args) => runTool(tools.dispatch(args, deps.token, deps.kv, deps.ctx)),
  )

  // -------------------------------------------------------------------------
  // result
  // -------------------------------------------------------------------------
  server.registerTool(
    "result",
    {
      description: "Get the status and result of a dispatched job by ID.",
      inputSchema: {
        jobId: ResultParamsZod.shape.jobId.describe("Job ID from dispatch"),
      },
    },
    async (args) => runTool(tools.result(args, deps.token, deps.kv)),
  )

  return server
}
