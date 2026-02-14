/// <reference types="astro/client" />

type KVNamespace = import('@cloudflare/workers-types').KVNamespace

interface Env {
  JOBS: KVNamespace
  OPENROUTER_API_KEY: string
  CHOMP_API_TOKEN: string
}

type Runtime = import('@astrojs/cloudflare').Runtime<Env>

declare namespace App {
  interface Locals extends Runtime {}
}
