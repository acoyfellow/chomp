// Agent and router definitions (config, not data)
const AGENTS = {
  shelley:  { name: 'Shelley',  color: '#C8A630', darkColor: '#E8C872', icon: 'S',
    models: ['claude-sonnet-4', 'claude-opus-4'] },
  opencode: { name: 'OpenCode', color: '#4F6EC5', darkColor: '#7B93DB', icon: 'O',
    models: ['claude-sonnet-4', 'claude-opus-4', 'gpt-4.1', 'gemini-2.5-pro', 'o3', 'o4-mini'] },
  pi:       { name: 'Pi',       color: '#9B4FBF', darkColor: '#C97BDB', icon: 'P',
    models: ['claude-sonnet-4', 'claude-opus-4', 'gpt-4.1', 'gemini-2.5-pro'] },
};

const ROUTERS = {
  'cf-ai':      { name: 'Cloudflare AI Gateway', short: 'CF AI',      color: '#D96F0E' },
  'zen':        { name: 'OpenCode Zen',          short: 'Zen',        color: '#4F6EC5' },
  'openrouter': { name: 'OpenRouter',            short: 'OpenRouter',  color: '#7C3AED' },
};

// Live state — populated from API
let LOOPS = [];
let QUEUE = [];
let DONE = [];

async function fetchState() {
  try {
    const res = await fetch('/api/state');
    if (!res.ok) return;
    const state = await res.json();
    const tasks = state.tasks || [];

    LOOPS = tasks.filter(t => t.status === 'active').map(t => ({
      id: t.id,
      prompt: t.prompt,
      dir: t.dir || '',
      state: 'running',
      totalTokens: t.tokens || 0,
      platform: t.platform || '',
      created: t.created || '',
      gates: [],
      sessions: [],
    }));

    QUEUE = tasks.filter(t => t.status === 'queued').map(t => ({
      id: t.id,
      prompt: t.prompt,
      dir: t.dir || '',
      created: t.created || '',
    }));

    DONE = tasks.filter(t => t.status === 'done' || t.status === 'failed').map(t => ({
      id: t.id,
      prompt: t.prompt,
      tokens: t.tokens || 0,
      platform: t.platform || '',
      result: t.result || '',
      status: t.status,
      agents: t.platform ? [t.platform] : [],
      sessions: 1,
    }));
  } catch (e) {
    console.error('fetch state failed:', e);
    return { error: e.message };
  }
}
