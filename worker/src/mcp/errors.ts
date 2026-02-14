import { Schema } from "effect"

export class AuthError extends Schema.TaggedError<AuthError>()("AuthError", {
  message: Schema.String,
}) {}

export class DispatchError extends Schema.TaggedError<DispatchError>()("DispatchError", {
  message: Schema.String,
  statusCode: Schema.Number,
}) {}

export class PollError extends Schema.TaggedError<PollError>()("PollError", {
  message: Schema.String,
  jobId: Schema.String,
}) {}

export class JobNotFoundError extends Schema.TaggedError<JobNotFoundError>()("JobNotFoundError", {
  jobId: Schema.String,
}) {}

export class JobPendingError extends Schema.TaggedError<JobPendingError>()("JobPendingError", {
  jobId: Schema.String,
  status: Schema.String,
}) {}

export class ModelError extends Schema.TaggedError<ModelError>()("ModelError", {
  message: Schema.String,
}) {}
