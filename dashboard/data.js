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
      { id: 1, outcome: "handoff", tokens: 195000, duration: "8m12s", summary: "Extracted JWT utils, added refresh endpoint skeleton" },
      { id: 2, outcome: "handoff", tokens: 210400, duration: "11m03s", summary: "Implemented refresh flow, login gate passing" },
      { id: 3, outcome: "fail", tokens: 178300, duration: "6m41s", summary: "Revoke endpoint 500s on missing token — context limit hit" },
      { id: 4, outcome: "active", tokens: 263500, duration: "4m22s", summary: "Fixing revoke handler, added error boundary" },
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
      { id: 1, outcome: "handoff", tokens: 156000, duration: "5m30s", summary: "Created test harness, wrote create/list tests" },
      { id: 2, outcome: "active", tokens: 156800, duration: "3m10s", summary: "Running gate checks after delete tests" },
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
      { id: 1, outcome: "pass", tokens: 120000, duration: "4m00s", summary: "Scanned 8 repos, catalogued READMEs" },
      { id: 2, outcome: "handoff", tokens: 198500, duration: "9m15s", summary: "Rewrote chomp + myfilepath READMEs" },
      { id: 3, outcome: "handoff", tokens: 270600, duration: "12m40s", summary: "Stuck on formwing — code doesn't match docs. Need human input." },
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
      { id: 1, outcome: "handoff", tokens: 180000, duration: "7m20s", summary: "Set up webhook route, signature verification passing" },
      { id: 2, outcome: "fail", tokens: 220000, duration: "9m50s", summary: "subscribe handler wrong event shape" },
      { id: 3, outcome: "fail", tokens: 195400, duration: "8m10s", summary: "Still failing — using old Stripe API format" },
      { id: 4, outcome: "fail", tokens: 210000, duration: "10m30s", summary: "Tried v2 API, type errors in D1 binding" },
      { id: 5, outcome: "fail", tokens: 216000, duration: "7m55s", summary: "Same 3 gates failing. Likely needs Stripe test fixtures." },
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
      { id: 1, outcome: "active", tokens: 98700, duration: "2m15s", summary: "Reading CF docs, drafting outline" },
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
  { id: "1", prompt: "Set up CI pipeline for myfilepath", tokens: 245000, platform: "exe.dev" },
  { id: "2", prompt: "Fix TypeScript strict mode errors", tokens: 189000, platform: "opencode" },
  { id: "4", prompt: "Add dark mode to dashboard", tokens: 67000, platform: "opencode" },
  { id: "6", prompt: "Write database migration scripts", tokens: 312000, platform: "exe.dev" },
  { id: "8", prompt: "Optimize bundle size", tokens: 156000, platform: "opencode" },
  { id: "10", prompt: "Set up error tracking with Sentry", tokens: 423000, platform: "exe.dev" },
];
