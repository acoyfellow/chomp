let currentTab = 'active';
let selectedLoop = null;

const fmt = n => n >= 1e6 ? (n/1e6).toFixed(1)+'M' : n >= 1e3 ? Math.round(n/1e3)+'k' : ''+n;
const circ = 113.1; // 2*PI*18

// ── Summary ──
function renderSummary() {
  const burns = [...LOOPS,...DONE].reduce((s,t) => s+(t.totalTokens||t.tokens||0), 0);
  const sess = LOOPS.reduce((s,l) => s+l.sessions.length, 0);
  const live = LOOPS.filter(l => l.state === 'running').length;
  document.getElementById('summary').innerHTML = `
    <div class="sum-item"><div class="sum-val"><span class="sum-live"></span>${live}</div><div class="sum-label">Live</div></div>
    <div class="sum-item"><div class="sum-val">${LOOPS.length}</div><div class="sum-label">Loops</div></div>
    <div class="sum-item"><div class="sum-val">${sess}</div><div class="sum-label">Sessions</div></div>
    <div class="sum-item"><div class="sum-val hot">${fmt(burns)}</div><div class="sum-label">Burned</div></div>`;
}

// ── Content ──
function renderContent() {
  const el = document.getElementById('content');
  if (currentTab === 'active') renderLoops(el);
  else if (currentTab === 'queue') renderQueue(el);
  else renderDone(el);
}

function renderLoops(el) {
  if (!LOOPS.length) { el.innerHTML = '<div class="empty"><div class="empty-icon">\u25ce</div><div class="empty-text">No active loops</div></div>'; return; }
  el.innerHTML = LOOPS.map(loop => {
    const gp = loop.gates.filter(g => g.status === 'pass').length;
    const gt = loop.gates.length;
    const pct = gt ? Math.round(gp/gt*100) : 0;
    const offset = circ - (circ * pct / 100);
    const agents = [...new Set(loop.sessions.map(s => s.agent))];
    const color = loop.state === 'running' ? 'var(--green)' : loop.state === 'failing' ? 'var(--red)' : loop.state === 'gating' ? 'var(--blue)' : 'var(--orange)';

    return `
    <div class="card" onclick="openSheet('${loop.id}')">
      <div class="card-top">
        <div class="card-title">${loop.prompt}</div>
        <div class="badge badge-${loop.state}">${loop.state}</div>
      </div>
      <div class="card-agents">
        ${agents.map(a => `<div class="agent-chip"><div class="agent-dot" style="background:${AGENTS[a]?.color}"></div>${AGENTS[a]?.name}</div>`).join('')}
      </div>
      <div class="card-progress">
        <div class="progress-ring">
          <svg width="40" height="40" viewBox="0 0 40 40">
            <circle class="progress-ring-bg" cx="20" cy="20" r="18"/>
            <circle class="progress-ring-fill" cx="20" cy="20" r="18" stroke="${color}" stroke-dasharray="${circ}" stroke-dashoffset="${offset}"/>
          </svg>
          <div class="progress-ring-text">${gp}/${gt}</div>
        </div>
        <div class="progress-info">
          <div class="progress-label">${gp} of ${gt} gates passing</div>
          <div class="progress-sub">${fmt(loop.totalTokens)} tokens · ${loop.sessions.length} sessions</div>
        </div>
      </div>
      <div class="card-sessions">
        ${loop.sessions.map(s => `<div class="ses-pip s-${s.outcome === 'active' ? 'active' : s.outcome}"></div>`).join('')}
        <span class="ses-count">${loop.sessions.length} sessions</span>
      </div>
      <div class="level-bar">
        <div class="level-row">
          <span class="level-label">${loop.state === 'running' ? 'In progress' : loop.state === 'gating' ? 'Verifying' : loop.state === 'failing' ? 'Stuck' : 'Waiting'}</span>
          <span class="level-xp">${fmt(loop.totalTokens)} burned</span>
        </div>
        <div class="level-track"><div class="level-fill lv-${loop.state === 'running' ? 'green' : loop.state === 'gating' ? 'blue' : loop.state === 'failing' ? 'red' : 'orange'}" style="width:${pct}%"></div></div>
      </div>
    </div>`;
  }).join('');
}

function renderQueue(el) {
  if (!QUEUE.length) { el.innerHTML = '<div class="empty"><div class="empty-icon">\u2191</div><div class="empty-text">Queue empty</div></div>'; return; }
  el.innerHTML = QUEUE.map(t => `
    <div class="q-card" onclick="openPicker('dispatch','${t.id}')">
      <div class="q-num">#${t.id}</div>
      <div class="q-prompt">${t.prompt}</div>
    </div>`).join('');
}

function renderDone(el) {
  if (!DONE.length) { el.innerHTML = '<div class="empty"><div class="empty-icon">\u2713</div><div class="empty-text">Nothing completed yet</div></div>'; return; }
  el.innerHTML = DONE.map(t => {
    const dots = (t.agents||[]).map(a => `<div class="agent-dot" style="background:${AGENTS[a]?.color};width:6px;height:6px;border-radius:50%"></div>`).join('');
    return `
    <div class="d-card">
      <div class="d-prompt"><span class="d-check">\u2713</span>${t.prompt}</div>
      <div class="d-meta">
        <div class="d-agents-row">${dots}</div>
        <span>${fmt(t.tokens)} burned</span>
        <span>${t.sessions} sessions</span>
      </div>
    </div>`;
  }).join('');
}

// ── Sheet (detail) ──
function openSheet(id) {
  selectedLoop = LOOPS.find(l => l.id === id);
  if (!selectedLoop) return;
  renderSheet();
  document.getElementById('sheet-bg').classList.add('open');
  document.getElementById('sheet').classList.add('open');
}
function closeSheet() {
  document.getElementById('sheet-bg').classList.remove('open');
  document.getElementById('sheet').classList.remove('open');
  selectedLoop = null;
}

function renderSheet() {
  const l = selectedLoop;
  if (!l) return;
  const gp = l.gates.filter(g => g.status === 'pass').length;
  const bp = Math.min(100, l.totalTokens / 1500000 * 100);
  const cm = l.sessions.reduce((s, se) => s + (se.commits?.length || 0), 0);

  document.getElementById('sheet-body').innerHTML = `
    <div class="badge badge-${l.state}" style="display:inline-block;margin-bottom:10px">${l.state}</div>
    <div class="sh-title">${l.prompt}</div>
    <div class="sh-dir">${l.dir}</div>

    <div class="sh-section">
      <div class="sh-label">Token Burn</div>
      <div class="tok-labels"><span>${fmt(l.totalTokens)}</span><span>~1.5M</span></div>
      <div class="tok-bar"><div class="tok-fill" style="width:${bp}%"></div></div>
    </div>

    <div class="sh-section">
      <div class="sh-label">Gates — ${gp} of ${l.gates.length}</div>
      ${l.gates.map(g => {
        const cls = g.status === 'pass' ? 'gp' : g.status === 'fail' ? 'gf' : 'gpn';
        return `<div class="gate-row">
          <div class="gate-dot ${cls}"></div>
          <div class="gate-name">${g.name}</div>
          <div class="gate-st ${cls}">${g.status}</div>
        </div>`;
      }).join('')}
    </div>

    <div class="sh-section">
      <div class="sh-label">Sessions — ${l.sessions.length} runs · ${cm} commits</div>
      ${l.sessions.map(s => {
        const ag = AGENTS[s.agent]; const rt = ROUTERS[s.router];
        const dotCls = s.outcome === 'active' ? 's-active' : 's-'+s.outcome;
        return `<div class="sh-ses">
          <div class="sh-ses-top">
            <div class="sh-ses-dot ses-pip ${dotCls}"></div>
            <div class="sh-ses-num">#${s.id}</div>
            <div class="sh-ses-sum">${s.summary}</div>
          </div>
          <div class="sh-ses-meta">
            <span class="sh-chip"><span class="sh-chip-dot" style="background:${ag?.color}"></span>${ag?.name}</span>
            <span class="sh-chip" style="border-left:2px solid ${rt?.color}">${rt?.short}</span>
            <span class="sh-tok">${fmt(s.tokens)} · ${s.duration}</span>
            ${(s.commits||[]).map(c => `<span class="sh-sha">${c.substring(0,7)}</span>`).join(' ')}
          </div>
        </div>`;
      }).join('')}
    </div>

    <div class="sh-section">
      <div class="sh-label">Git Audit</div>
      ${l.sessions.slice().reverse().flatMap(s => {
        const ag = AGENTS[s.agent];
        return (s.commits||[]).map(c =>
          `<div class="git-row"><span class="git-sha">${c.substring(0,7)}</span><span class="git-msg">${s.summary.substring(0,32)}</span><span class="git-agent" style="color:${ag?.color}">${ag?.name}</span></div>`
        );
      }).join('')}
    </div>

    <div class="sh-actions">
      ${l.state === 'failing' ? '<button class="btn btn-danger">Kill Loop</button>' : ''}
      <button class="btn btn-secondary" onclick="closeSheet();openPicker('swap','${l.id}')">Swap Agent</button>
      <button class="btn btn-primary">Re-dispatch</button>
    </div>`;
}

// ── Picker (agent + router) ──
let pickerMode = null, pickerTarget = null, pickedAgent = null, pickedRouter = null;

function openPicker(mode, targetId) {
  pickerMode = mode; pickerTarget = targetId;
  pickedAgent = null; pickedRouter = null;
  renderPicker();
  document.getElementById('picker-bg').classList.add('open');
  document.getElementById('picker').classList.add('open');
}
function closePicker() {
  document.getElementById('picker-bg').classList.remove('open');
  document.getElementById('picker').classList.remove('open');
}

function renderPicker() {
  const title = pickerMode === 'dispatch' ? `Dispatch #${pickerTarget}` : `Swap agent`;
  document.getElementById('picker-body').innerHTML = `
    <div class="pick-title">${title}</div>
    <div class="pick-sub">Choose agent and router</div>

    <div class="pick-label">Agent</div>
    <div class="pick-agents">
      ${Object.entries(AGENTS).map(([id,a]) => `
        <div class="pick-agent ${pickedAgent===id?'picked':''}" onclick="pickAgent('${id}')">
          <div class="pick-agent-dot" style="background:${a.color}"></div>
          <div class="pick-agent-name">${a.name}</div>
        </div>`).join('')}
    </div>

    <div class="pick-label">Router</div>
    <div class="pick-routers">
      ${Object.entries(ROUTERS).map(([id,r]) => `
        <div class="pick-router ${pickedRouter===id?'picked':''}" onclick="pickRouter('${id}')" style="${pickedRouter===id?'color:'+r.color:''}">
          ${r.short}
        </div>`).join('')}
    </div>

    <button class="btn btn-primary" style="width:100%" onclick="confirmPick()">${pickerMode==='dispatch'?'Dispatch':'Swap & Go'}</button>`;
}

function pickAgent(id) { pickedAgent = id; renderPicker(); }
function pickRouter(id) { pickedRouter = id; renderPicker(); }
function confirmPick() {
  if (!pickedAgent || !pickedRouter) return;
  closePicker();
}

// ── Tabs ──
document.querySelectorAll('.tab').forEach(t => t.addEventListener('click', () => {
  document.querySelectorAll('.tab').forEach(b => b.classList.remove('active'));
  t.classList.add('active');
  currentTab = t.dataset.tab;
  renderContent();
}));

// ── Tick ──
setInterval(() => {
  LOOPS.forEach(l => {
    if (l.state === 'running') {
      const inc = Math.floor(Math.random()*500)+100;
      l.totalTokens += inc;
      const a = l.sessions.find(s => s.outcome === 'active');
      if (a) a.tokens += inc;
    }
  });
  renderSummary();
  if (selectedLoop) renderSheet();
}, 3000);

// ── Unlock flash (call when all gates pass) ──
function showUnlock(taskName) {
  const div = document.createElement('div');
  div.className = 'unlock-flash';
  div.innerHTML = `<div class="unlock-content">
    <div class="unlock-ring">\u2713</div>
    <div class="unlock-title">${taskName}</div>
    <div class="unlock-sub">All gates passed</div>
  </div>`;
  document.body.appendChild(div);
  setTimeout(() => div.remove(), 2600);
}

// ── Init ──
renderSummary();
renderContent();
