const AGENTS = {
  shelley:  { name: 'Shelley',  color: '#E8C872', icon: 'S' },
  opencode: { name: 'OpenCode', color: '#7B93DB', icon: 'O' },
  pi:       { name: 'Pi',       color: '#C97BDB', icon: 'P' },
};

const ROUTERS = {
  'cf-ai':      { name: 'Cloudflare AI Gateway', short: 'CF AI',     color: '#F6821F' },
  'zen':        { name: 'OpenCode Zen',          short: 'Zen',       color: '#7B93DB' },
  'openrouter': { name: 'OpenRouter',            short: 'OpenRouter', color: '#8B5CF6' },
};

const LOOPS = [
  {
    id: '3', prompt: 'Refactor auth module to JWT refresh tokens',
    dir: '/home/exedev/myfilepath-new', state: 'running', totalTokens: 847200,
    gates: [
      { name: 'auth-login', status: 'pass' },
      { name: 'auth-refresh', status: 'pass' },
      { name: 'auth-revoke', status: 'fail' },
      { name: 'auth-middleware', status: 'pending' },
      { name: 'auth-e2e', status: 'pending' },
    ],
    sessions: [
      { id: 1, agent: 'shelley', router: 'cf-ai', model: 'claude-sonnet-4', outcome: 'handoff', tokens: 195000, duration: '8m 12s', summary: 'Extracted JWT utils, added refresh endpoint skeleton', commits: ['a1b2c3d'] },
      { id: 2, agent: 'opencode', router: 'zen', model: 'claude-sonnet-4', outcome: 'handoff', tokens: 210400, duration: '11m 03s', summary: 'Implemented refresh flow, login gate now passing', commits: ['e4f5g6h', 'i7j8k9l'] },
      { id: 3, agent: 'pi', router: 'openrouter', model: 'o3', outcome: 'fail', tokens: 178300, duration: '6m 41s', summary: 'Revoke endpoint 500s on missing token', commits: ['m0n1o2p'] },
      { id: 4, agent: 'shelley', router: 'cf-ai', model: 'claude-sonnet-4', outcome: 'active', tokens: 263500, duration: '4m 22s', summary: 'Fixing revoke handler, added error boundary', commits: ['q3r4s5t'] },
    ]
  },
  {
    id: '5', prompt: 'Write tests for /api/session endpoints',
    dir: '/home/exedev/myfilepath-new', state: 'gating', totalTokens: 312800,
    gates: [
      { name: 'session-create', status: 'pass' },
      { name: 'session-list', status: 'pass' },
      { name: 'session-delete', status: 'pass' },
      { name: 'session-multi', status: 'pending' },
    ],
    sessions: [
      { id: 1, agent: 'opencode', router: 'zen', model: 'sonnet-4', outcome: 'handoff', tokens: 156000, duration: '5m 30s', summary: 'Created test harness, wrote create/list tests', commits: ['u5v6w7x'] },
      { id: 2, agent: 'opencode', router: 'openrouter', model: 'gemini-2.5-pro', outcome: 'active', tokens: 156800, duration: '3m 10s', summary: 'Gate checks running after delete tests', commits: ['y8z9a0b'] },
    ]
  },
  {
    id: '7', prompt: 'Audit and rewrite all READMEs',
    dir: '/home/exedev', state: 'handoff', totalTokens: 589100,
    gates: [
      { name: 'readme-exists', status: 'pass' },
      { name: 'readme-accurate', status: 'fail' },
      { name: 'readme-examples', status: 'fail' },
    ],
    sessions: [
      { id: 1, agent: 'shelley', router: 'cf-ai', model: 'claude-sonnet-4', outcome: 'pass', tokens: 120000, duration: '4m 00s', summary: 'Scanned 8 repos, catalogued READMEs', commits: [] },
      { id: 2, agent: 'pi', router: 'openrouter', model: 'sonnet-4', outcome: 'handoff', tokens: 198500, duration: '9m 15s', summary: 'Rewrote chomp + myfilepath READMEs', commits: ['c1d2e3f', 'g4h5i6j'] },
      { id: 3, agent: 'opencode', router: 'zen', model: 'claude-opus-4', outcome: 'handoff', tokens: 270600, duration: '12m 40s', summary: 'Stuck on formwing \u2014 code doesn\'t match docs', commits: ['o0p1q2r'] },
    ]
  },
  {
    id: '9', prompt: 'Implement Stripe webhook handler',
    dir: '/home/exedev/myfilepath-new', state: 'failing', totalTokens: 1021400,
    gates: [
      { name: 'webhook-verify', status: 'pass' },
      { name: 'webhook-subscribe', status: 'fail' },
      { name: 'webhook-cancel', status: 'fail' },
      { name: 'webhook-upgrade', status: 'fail' },
      { name: 'webhook-idempotent', status: 'pending' },
    ],
    sessions: [
      { id: 1, agent: 'shelley', router: 'cf-ai', model: 'claude-sonnet-4', outcome: 'handoff', tokens: 180000, duration: '7m 20s', summary: 'Set up webhook route, sig verification passing', commits: ['s3t4u5v'] },
      { id: 2, agent: 'pi', router: 'openrouter', model: 'o3', outcome: 'fail', tokens: 220000, duration: '9m 50s', summary: 'subscribe handler wrong event shape', commits: ['w6x7y8z'] },
      { id: 3, agent: 'opencode', router: 'zen', model: 'o4-mini', outcome: 'fail', tokens: 195400, duration: '8m 10s', summary: 'Still failing \u2014 old Stripe API format', commits: ['a9b0c1d'] },
      { id: 4, agent: 'opencode', router: 'openrouter', model: 'claude-opus-4', outcome: 'fail', tokens: 210000, duration: '10m 30s', summary: 'Tried v2 API, type errors in D1 binding', commits: ['e2f3g4h', 'i5j6k7l'] },
      { id: 5, agent: 'shelley', router: 'cf-ai', model: 'deepseek-v3', outcome: 'fail', tokens: 216000, duration: '7m 55s', summary: 'Same 3 gates failing. Needs Stripe fixtures.', commits: ['m8n9o0p'] },
    ]
  },
  {
    id: '11', prompt: 'Research Durable Objects best practices',
    dir: '/home/exedev', state: 'running', totalTokens: 98700,
    gates: [
      { name: 'guide-exists', status: 'pending' },
      { name: 'guide-examples', status: 'pending' },
    ],
    sessions: [
      { id: 1, agent: 'pi', router: 'openrouter', model: 'claude-sonnet-4', outcome: 'active', tokens: 98700, duration: '2m 15s', summary: 'Reading CF docs, drafting outline', commits: [] },
    ]
  },
];

const QUEUE = [
  { id: '12', prompt: 'Add rate limiting to all API endpoints' },
  { id: '13', prompt: 'Set up monitoring dashboards' },
  { id: '14', prompt: 'Migrate formwing-v3 to latest SvelteKit' },
  { id: '15', prompt: 'Write PRD for conductor runtime' },
];

const DONE = [
  { id: '1', prompt: 'Set up CI pipeline', tokens: 245000, sessions: 2, agents: ['shelley'] },
  { id: '2', prompt: 'Fix TypeScript strict mode', tokens: 189000, sessions: 1, agents: ['opencode'] },
  { id: '4', prompt: 'Add dark mode', tokens: 67000, sessions: 1, agents: ['pi'] },
  { id: '6', prompt: 'Database migration scripts', tokens: 312000, sessions: 3, agents: ['shelley','opencode'] },
  { id: '8', prompt: 'Optimize bundle size', tokens: 156000, sessions: 2, agents: ['pi'] },
  { id: '10', prompt: 'Error tracking with Sentry', tokens: 423000, sessions: 4, agents: ['shelley','opencode'] },
];
