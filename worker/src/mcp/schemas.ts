import { Schema } from "effect"
import { z } from "zod"

// ---------------------------------------------------------------------------
// Effect Schemas
// ---------------------------------------------------------------------------

export class DispatchParams extends Schema.Class<DispatchParams>("DispatchParams")({
  prompt: Schema.String,
  model: Schema.optional(Schema.String),
  system: Schema.optional(Schema.String),
  router: Schema.optional(Schema.String),
}) {}

export class ResultParams extends Schema.Class<ResultParams>("ResultParams")({
  jobId: Schema.String,
}) {}

export class AskParams extends Schema.Class<AskParams>("AskParams")({
  prompt: Schema.String,
  model: Schema.optional(Schema.String),
  system: Schema.optional(Schema.String),
  router: Schema.optional(Schema.String),
}) {}

export class Job extends Schema.Class<Job>("Job")({
  id: Schema.String,
  prompt: Schema.String,
  system: Schema.String,
  model: Schema.String,
  status: Schema.String,
  result: Schema.String,
  error: Schema.String,
  tokens_in: Schema.Number,
  tokens_out: Schema.Number,
  created: Schema.String,
  finished: Schema.String,
  latency_ms: Schema.Number,
}) {}

export class DispatchResult extends Schema.Class<DispatchResult>("DispatchResult")({
  id: Schema.String,
  model: Schema.String,
  status: Schema.String,
}) {}

export class FreeModel extends Schema.Class<FreeModel>("FreeModel")({
  id: Schema.String,
  name: Schema.String,
  context_length: Schema.Number,
  max_output: Schema.Number,
}) {}

// ---------------------------------------------------------------------------
// Zod Schemas (for MCP SDK registerTool boundary)
// ---------------------------------------------------------------------------

export const DispatchParamsZod = z.object({
  prompt: z.string(),
  model: z.string().optional(),
  system: z.string().optional(),
  router: z.string().optional(),
})

export const ResultParamsZod = z.object({
  jobId: z.string(),
})

export const AskParamsZod = z.object({
  prompt: z.string(),
  model: z.string().optional(),
  system: z.string().optional(),
  router: z.string().optional(),
})
