import type { APIRoute } from "astro";
import {
  extractToken,
  resolveUser,
  unauthorized,
  jsonResponse,
} from "../../lib/auth";
import { routers } from "../../lib/routers";
import type { RouterDef } from "../../lib/routers";

interface UpstreamModel {
  id: string;
  object: string;
  created?: number;
  owned_by?: string;
}

const CACHE_TTL = 15 * 60; // 15 minutes

const CORS_HEADERS: Record<string, string> = {
  "Access-Control-Allow-Origin": "*",
  "Access-Control-Allow-Methods": "GET, OPTIONS",
  "Access-Control-Allow-Headers": "Authorization, Content-Type",
};

function corsJson(data: unknown, status = 200): Response {
  const res = jsonResponse(data, status);
  for (const [k, v] of Object.entries(CORS_HEADERS)) {
    res.headers.set(k, v);
  }
  return res;
}

async function fetchRouterModels(
  router: RouterDef,
  apiKey: string,
): Promise<UpstreamModel[]> {
  const headers: Record<string, string> = {
    Authorization: `Bearer ${apiKey}`,
    ...router.headers,
  };

  const res = await fetch(`${router.baseUrl}/models`, { headers });
  if (!res.ok) {
    console.warn(`[models] ${router.id}: HTTP ${res.status} ${res.statusText}`);
    return [];
  }

  const body = (await res.json()) as { data?: UpstreamModel[] };
  return body.data ?? [];
}

export const OPTIONS: APIRoute = async () => {
  return new Response(null, { status: 204, headers: CORS_HEADERS });
};

export const GET: APIRoute = async ({ request, locals }) => {
  // 1. Auth
  const token = extractToken(request);
  if (!token) return corsJson({ error: "unauthorized" }, 401);

  const kv = (locals as any).runtime.env.JOBS as KVNamespace;
  const user = await resolveUser(token, kv);
  if (!user) return corsJson({ error: "unauthorized" }, 401);

  // 2. Cache check
  const cache = (caches as unknown as { default: Cache }).default;
  const cacheKey = new Request(`https://chomp-cache/v1/models?token=${token}`);

  const cached = await cache.match(cacheKey);
  if (cached) return cached;

  // 3. Determine which routers the user has keys for
  const userRouters = routers.filter((r) => user.keys[r.id]);

  // 4. Fetch models from all routers in parallel
  const results = await Promise.allSettled(
    userRouters.map((router) =>
      fetchRouterModels(router, user.keys[router.id]).then((models) =>
        models.map((m) => ({
          id: `${router.id}/${m.id}`,
          object: "model" as const,
          created: m.created ?? 0,
          owned_by: m.owned_by ?? router.id,
        })),
      ),
    ),
  );

  // 5. Aggregate successful results
  const data: Array<{
    id: string;
    object: "model";
    created: number;
    owned_by: string;
  }> = [];

  for (const result of results) {
    if (result.status === "fulfilled") {
      data.push(...result.value);
    } else {
      console.warn("[models] router fetch rejected:", result.reason);
    }
  }

  // 6. Build OpenAI-format response
  const response = corsJson({ object: "list", data });
  response.headers.set("Cache-Control", `public, max-age=${CACHE_TTL}`);

  // 7. Store in cache (fire-and-forget)
  cache
    .put(cacheKey, response.clone())
    .catch((err: unknown) => console.warn("[models] cache put failed:", err));

  return response;
};
