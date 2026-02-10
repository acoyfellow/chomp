// Agent catalog — every CLI agent chomp can dispatch to
const AGENTS = {
  'shelley':      { name: 'Shelley',      cli: 'shelley',      color: '#f59e0b', models: ['claude-sonnet-4', 'claude-opus-4'] },
  'claude-code':  { name: 'Claude Code',  cli: 'claude',       color: '#a855f7', models: ['claude-sonnet-4', 'claude-opus-4', 'claude-haiku'] },
  'codex':        { name: 'Codex CLI',    cli: 'codex',        color: '#22c55e', models: ['codex-mini', 'o3', 'o4-mini'] },
  'opencode':     { name: 'OpenCode',     cli: 'opencode',     color: '#3b82f6', models: ['sonnet-4', 'gpt-4.1', 'gemini-2.5-pro'] },
  'aider':        { name: 'Aider',        cli: 'aider',        color: '#ef4444', models: ['sonnet-4', 'gpt-4.1', 'deepseek-v3'] },
  'amp':          { name: 'Amp',          cli: 'amp',          color: '#06b6d4', models: ['claude-sonnet-4', 'claude-opus-4'] },
  'goose':        { name: 'Goose',        cli: 'goose',        color: '#f97316', models: ['sonnet-4', 'gpt-4.1'] },
  'custom':       { name: 'Custom',       cli: null,           color: '#71717a', models: ['any'] },
};

// Mock data representing chomp loops
const LOOPS = [
  {
    id: "3", prompt: "Refactor auth module to use JWT refresh tokens",
    dir: "/home/exedev/myfilepath-new", platform: "exe.dev",
    state: "running", totalTokens: 847200,
    gates: [
      { name: "auth-login", status: "pass" },
      { name: "auth-refresh", status: "pass" },
      { name: "auth-revoke", status: "fail" },
      { name: "auth-middleware", status: "pending" },
      { name: "auth-e2e", status: "pending" },
    ],
    sessions: [
      { id: 1, agent: 'shelley', model: 'claude-sonnet-4', outcome: "handoff", tokens: 195000, duration: "8m12s", summary: "Extracted JWT utils, added refresh endpoint skeleton", commits: ['a1b2c3d'] },
      { id: 2, agent: 'claude-code', model: 'claude-sonnet-4', outcome: "handoff", tokens: 210400, duration: "11m03s", summary: "Implemented refresh flow, login gate passing", commits: ['e4f5g6h', 'i7j8k9l'] },
      { id: 3, agent: 'codex', model: 'o3', outcome: "fail", tokens: 178300, duration: "6m41s", summary: "Revoke endpoint 500s on missing token \u2014 context limit hit", commits: ['m0n1o2p'] },
      { id: 4, agent: 'shelley', model: 'claude-sonnet-4', outcome: "active", tokens: 263500, duration: "4m22s", summary: "Fixing revoke handler, added error boundary", commits: ['q3r4s5t'] },
    ]
  },
  {
    id: "5", prompt: "Write comprehensive tests for /api/session endpoints",
    dir: "/home/exedev/myfilepath-new", platform: "opencode",
    state: "gating", totalTokens: 312800,
    gates: [
      { name: "session-create", status: "pass" },
      { name: "session-list", status: "pass" },
      { name: "session-delete", status: "pass" },
      { name: "session-multi", status: "pending" },
    ],
    sessions: [
      { id: 1, agent: 'opencode', model: 'sonnet-4', outcome: "handoff", tokens: 156000, duration: "5m30s", summary: "Created test harness, wrote create/list tests", commits: ['u5v6w7x'] },
      { id: 2, agent: 'opencode', model: 'gemini-2.5-pro', outcome: "active", tokens: 156800, duration: "3m10s", summary: "Running gate checks after delete tests", commits: ['y8z9a0b'] },
    ]
  },
  {
    id: "7", prompt: "Audit and rewrite README for accuracy across all repos",
    dir: "/home/exedev", platform: "exe.dev",
    state: "handoff", totalTokens: 589100,
    gates: [
      { name: "readme-exists", status: "pass" },
      { name: "readme-accurate", status: "fail" },
      { name: "readme-examples", status: "fail" },
    ],
    sessions: [
      { id: 1, agent: 'shelley', model: 'claude-sonnet-4', outcome: "pass", tokens: 120000, duration: "4m00s", summary: "Scanned 8 repos, catalogued READMEs", commits: [] },
      { id: 2, agent: 'aider', model: 'sonnet-4', outcome: "handoff", tokens: 198500, duration: "9m15s", summary: "Rewrote chomp + myfilepath READMEs", commits: ['c1d2e3f', 'g4h5i6j', 'k7l8m9n'] },
      { id: 3, agent: 'claude-code', model: 'claude-opus-4', outcome: "handoff", tokens: 270600, duration: "12m40s", summary: "Stuck on formwing \u2014 code doesn't match docs. Need human input.", commits: ['o0p1q2r'] },
    ]
  },
  {
    id: "9", prompt: "Implement Stripe webhook handler for subscription changes",
    dir: "/home/exedev/myfilepath-new", platform: "exe.dev",
    state: "failing", totalTokens: 1021400,
    gates: [
      { name: "webhook-verify", status: "pass" },
      { name: "webhook-subscribe", status: "fail" },
      { name: "webhook-cancel", status: "fail" },
      { name: "webhook-upgrade", status: "fail" },
      { name: "webhook-idempotent", status: "pending" },
    ],
    sessions: [
      { id: 1, agent: 'shelley', model: 'claude-sonnet-4', outcome: "handoff", tokens: 180000, duration: "7m20s", summary: "Set up webhook route, signature verification passing", commits: ['s3t4u5v'] },
      { id: 2, agent: 'codex', model: 'o3', outcome: "fail", tokens: 220000, duration: "9m50s", summary: "subscribe handler wrong event shape", commits: ['w6x7y8z'] },
      { id: 3, agent: 'codex', model: 'o4-mini', outcome: "fail", tokens: 195400, duration: "8m10s", summary: "Still failing \u2014 using old Stripe API format", commits: ['a9b0c1d'] },
      { id: 4, agent: 'claude-code', model: 'claude-opus-4', outcome: "fail", tokens: 210000, duration: "10m30s", summary: "Tried v2 API, type errors in D1 binding", commits: ['e2f3g4h', 'i5j6k7l'] },
      { id: 5, agent: 'aider', model: 'deepseek-v3', outcome: "fail", tokens: 216000, duration: "7m55s", summary: "Same 3 gates failing. Likely needs Stripe test fixtures.", commits: ['m8n9o0p'] },
    ]
  },
  {
    id: "11", prompt: "Research Cloudflare Durable Objects best practices and write internal guide",
    dir: "/home/exedev", platform: "opencode",
    state: "running", totalTokens: 98700,
    gates: [
      { name: "guide-exists", status: "pending" },
      { name: "guide-examples", status: "pending" },
    ],
    sessions: [
      { id: 1, agent: 'amp', model: 'claude-sonnet-4', outcome: "active", tokens: 98700, duration: "2m15s", summary: "Reading CF docs, drafting outline", commits: [] },
    ]
  },
];

const QUEUE = [
  { id: "12", prompt: "Add rate limiting to all API endpoints", dir: "/home/exedev/myfilepath-new" },
  { id: "13", prompt: "Set up monitoring dashboards in Grafana", dir: "/home/exedev" },
  { id: "14", prompt: "Migrate formwing-v3 to latest SvelteKit", dir: "/home/exedev/formwing-v3" },
  { id: "15", prompt: "Write PRD for conductor runtime", dir: "/home/exedev/myfilepath-new" },
];

const DONE = [
  { id: "1", prompt: "Set up CI pipeline for myfilepath", tokens: 245000, agents: [{agent:'shelley', model:'claude-sonnet-4', commits:3}] },
  { id: "2", prompt: "Fix TypeScript strict mode errors", tokens: 189000, agents: [{agent:'opencode', model:'sonnet-4', commits:5}] },
  { id: "4", prompt: "Add dark mode to dashboard", tokens: 67000, agents: [{agent:'codex', model:'o4-mini', commits:2}] },
  { id: "6", prompt: "Write database migration scripts", tokens: 312000, agents: [{agent:'shelley', model:'claude-sonnet-4', commits:4}, {agent:'claude-code', model:'claude-sonnet-4', commits:2}] },
  { id: "8", prompt: "Optimize bundle size", tokens: 156000, agents: [{agent:'aider', model:'sonnet-4', commits:3}] },
  { id: "10", prompt: "Set up error tracking with Sentry", tokens: 423000, agents: [{agent:'shelley', model:'claude-sonnet-4', commits:6}, {agent:'codex', model:'o3', commits:1}] },
];
