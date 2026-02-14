/// <reference types="astro/client" />

type KVNamespace = import('@cloudflare/workers-types').KVNamespace
type Fetcher = import('@cloudflare/workers-types').Fetcher

interface Env {
  JOBS: KVNamespace
  ASSETS: Fetcher
}

type Runtime = import('@astrojs/cloudflare').Runtime<Env>

declare namespace App {
  interface Locals extends Runtime {}
}
