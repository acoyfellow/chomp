import { getSandbox } from '@cloudflare/sandbox';
import type { Sandbox as SandboxType } from '@cloudflare/sandbox';
export { Sandbox } from '@cloudflare/sandbox';

interface Env {
  Sandbox: DurableObjectNamespace<SandboxType>;
  CHOMP_API: string;
  ANTHROPIC_API_KEY?: string;
  OPENAI_API_KEY?: string;
}

interface DispatchRequest {
  taskId: string;
  prompt: string;
  agent: string;
  model: string;
  repoUrl?: string;
  dir?: string;
  maxGateLoops?: number;
}

function json(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), {
    status,
    headers: {
      'Content-Type': 'application/json',
      'Access-Control-Allow-Origin': '*',
    },
  });
}

function corsHeaders(): HeadersInit {
  return {
    'Access-Control-Allow-Origin': '*',
    'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
    'Access-Control-Allow-Headers': 'Content-Type',
  };
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    // CORS preflight
    if (request.method === 'OPTIONS') {
      return new Response(null, { status: 204, headers: corsHeaders() });
    }

    // POST /dispatch — spin up sandbox, start agent
    if (url.pathname === '/dispatch' && request.method === 'POST') {
      return handleDispatch(request, env);
    }

    // GET /status/:sandboxId — check sandbox status
    if (url.pathname.startsWith('/status/')) {
      const sandboxId = url.pathname.split('/').pop();
      if (!sandboxId) return json({ error: 'missing sandbox id' }, 400);
      return handleStatus(env, sandboxId);
    }

    // POST /kill/:sandboxId — destroy sandbox
    if (url.pathname.startsWith('/kill/') && request.method === 'POST') {
      const sandboxId = url.pathname.split('/').pop();
      if (!sandboxId) return json({ error: 'missing sandbox id' }, 400);
      return handleKill(env, sandboxId);
    }

    // POST /exec/:sandboxId — run command in sandbox
    if (url.pathname.startsWith('/exec/') && request.method === 'POST') {
      const sandboxId = url.pathname.split('/').pop();
      if (!sandboxId) return json({ error: 'missing sandbox id' }, 400);
      return handleExec(request, env, sandboxId);
    }

    // GET /logs/:sandboxId/:processId — get process logs
    if (url.pathname.startsWith('/logs/')) {
      const parts = url.pathname.split('/');
      const processId = parts.pop();
      const sandboxId = parts.pop();
      if (!sandboxId || !processId) return json({ error: 'missing sandbox/process id' }, 400);
      return handleLogs(env, sandboxId, processId);
    }

    // Health check
    if (url.pathname === '/health') {
      return json({ status: 'ok', service: 'chomp-sandbox' });
    }

    return json({ error: 'not found' }, 404);
  },
};

async function handleDispatch(request: Request, env: Env): Promise<Response> {
  const body = await request.json() as DispatchRequest;
  const { taskId, prompt, agent, model, repoUrl, dir, maxGateLoops } = body;

  if (!taskId || !prompt || !agent || !model) {
    return json({ error: 'missing required fields: taskId, prompt, agent, model' }, 400);
  }

  const sandboxId = `task-${taskId}`;
  const sandbox = getSandbox(env.Sandbox, sandboxId, { keepAlive: true });

  try {
    // Clone repo if provided
    if (repoUrl) {
      const targetDir = dir || '/workspace/repo';
      await sandbox.gitCheckout(repoUrl, { depth: 1, targetDir });
    }

    // Set working directory
    const workDir = dir || (repoUrl ? '/workspace/repo' : '/workspace');

    // Start agent in background
    const process = await sandbox.startProcess(
      `run-agent "${agent}" "${model}" "${prompt.replace(/"/g, '\\"')}"`,
      {
        env: {
          CHOMP_API: env.CHOMP_API,
          TASK_ID: taskId,
          AGENT: agent,
          MODEL: model,
          REPO_DIR: workDir,
          ...(env.ANTHROPIC_API_KEY ? { ANTHROPIC_API_KEY: env.ANTHROPIC_API_KEY } : {}),
          ...(env.OPENAI_API_KEY ? { OPENAI_API_KEY: env.OPENAI_API_KEY } : {}),
          ...(maxGateLoops ? { MAX_GATE_LOOPS: String(maxGateLoops) } : {}),
        },
        cwd: workDir,
        processId: `agent-${taskId}`,
      }
    );

    return json({ sandboxId, status: 'started', agent, model, processId: process.id });
  } catch (err) {
    const message = err instanceof Error ? err.message : 'unknown error';
    return json({ error: `dispatch failed: ${message}` }, 500);
  }
}

async function handleStatus(env: Env, sandboxId: string): Promise<Response> {
  const sandbox = getSandbox(env.Sandbox, sandboxId);
  try {
    const result = await sandbox.exec('echo alive', { timeout: 5000 });
    return json({
      sandboxId,
      alive: result.exitCode === 0,
      exitCode: result.exitCode,
    });
  } catch {
    return json({ sandboxId, alive: false, error: 'unreachable' });
  }
}

async function handleExec(
  request: Request,
  env: Env,
  sandboxId: string
): Promise<Response> {
  const body = await request.json() as { command: string; timeout?: number; cwd?: string };
  if (!body.command) return json({ error: 'missing command' }, 400);

  const sandbox = getSandbox(env.Sandbox, sandboxId, { keepAlive: true });
  try {
    const result = await sandbox.exec(body.command, {
      timeout: body.timeout || 30000,
      cwd: body.cwd,
    });
    return json({
      sandboxId,
      success: result.success,
      exitCode: result.exitCode,
      stdout: result.stdout,
      stderr: result.stderr,
    });
  } catch (err) {
    const message = err instanceof Error ? err.message : 'exec failed';
    return json({ error: message }, 500);
  }
}

async function handleLogs(
  env: Env,
  sandboxId: string,
  processId: string
): Promise<Response> {
  const sandbox = getSandbox(env.Sandbox, sandboxId, { keepAlive: true });
  try {
    const proc = await sandbox.getProcess(processId);
    if (!proc) return json({ error: 'process not found' }, 404);
    const logs = await proc.getLogs();
    return json({ sandboxId, processId, logs });
  } catch (err) {
    const message = err instanceof Error ? err.message : 'logs failed';
    return json({ error: message }, 500);
  }
}

async function handleKill(env: Env, sandboxId: string): Promise<Response> {
  const sandbox = getSandbox(env.Sandbox, sandboxId);
  try {
    await sandbox.destroy();
    return json({ sandboxId, status: 'destroyed' });
  } catch {
    return json({ sandboxId, status: 'already destroyed or not found' });
  }
}
