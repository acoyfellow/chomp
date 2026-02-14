/// <reference types="astro/client" />

type KVNamespace = import('@cloudflare/workers-types').KVNamespace

interface Env {
  JOBS: KVNamespace
}

type Runtime = import('@astrojs/cloudflare').Runtime<Env>

declare namespace App {
  interface Locals extends Runtime {}
}
