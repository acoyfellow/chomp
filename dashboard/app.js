let currentTab = 'active';
let selectedLoop = null;

const fmt = n => n >= 1e6 ? (n/1e6).toFixed(1)+'M' : n >= 1e3 ? Math.round(n/1e3)+'k' : ''+n;
const circ = 113.1;

function ago(ts) {
  if (!ts) return '';
  const ms = Date.now() - new Date(ts).getTime();
  if (ms < 0) return 'just now';
  const s = Math.floor(ms/1000), m = Math.floor(s/60), h = Math.floor(m/60);
  if (h > 0) return h + 'h ' + (m%60) + 'm';
  if (m > 0) return m + 'm ' + (s%60) + 's';
  return s + 's';
}
function isStale(ts, thresholdMin) {
  if (!ts) return false;
  return (Date.now() - new Date(ts).getTime()) > thresholdMin * 60000;
}

// Error toast
function showToast(msg, isError) {
  const el = document.createElement('div');
  el.className = 'toast' + (isError ? ' toast-err' : '');
  el.textContent = msg;
  document.body.appendChild(el);
  setTimeout(() => el.classList.add('show'), 10);
  setTimeout(() => { el.classList.remove('show'); setTimeout(() => el.remove(), 300); }, 4000);
}

// Delete task
async function deleteTask(id, ev) {
  if (ev) ev.stopPropagation();
  if (!confirm('Delete this task?')) return;
  try {
    const res = await fetch('/api/tasks/delete', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({id}) });
    if (!res.ok) throw new Error(await res.text());
    showToast('Task deleted');
    closeSheet();
    await refresh();
  } catch(e) { showToast('Delete failed: ' + e.message, true); }
}

// ── Summary ──
function renderSummary() {
  const burns = [...LOOPS,...DONE].reduce((s,t) => s+(t.totalTokens||t.tokens||0), 0);
  const sess = LOOPS.reduce((s,l) => s+l.sessions.length, 0);
  const live = LOOPS.filter(l => l.state === 'running').length;
  document.getElementById('summary').innerHTML = `
    <div class="sum-item"><div class="sum-val">${live ? '<span class="sum-live"></span>' : ''}${live}</div><div class="sum-label">Live</div></div>
    <div class="sum-item"><div class="sum-val">${LOOPS.length + QUEUE.length + DONE.length}</div><div class="sum-label">Tasks</div></div>
    <div class="sum-item"><div class="sum-val">${sess || LOOPS.length}</div><div class="sum-label">Sessions</div></div>
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
  if (!LOOPS.length) {
    el.innerHTML = `<div class="empty"><div class="empty-icon">\u25ce</div><div class="empty-text">No active loops</div><div class="empty-sub">Tap + to create and start a task</div></div>`;
    return;
  }
  el.innerHTML = LOOPS.map(loop => {
    const gt = loop.gates.length;
    const gp = loop.gates.filter(g => g.status === 'pass').length;
    const pct = gt ? Math.round(gp/gt*100) : 0;
    const offset = circ - (circ * pct / 100);
    const hasGates = gt > 0;
    const color = 'var(--green)';
    const stale = isStale(loop.created, 5);
    const elapsed = ago(loop.created);
    return `
    <div class="card ${stale?'card-stale':''}" onclick="openSheet('${loop.id}')">
      <div class="card-top">
        <div class="card-title">${loop.prompt}</div>
        <div class="badge badge-running"><span class="spinner"></span> active</div>
      </div>
      <div class="card-agents">
        ${loop.platform ? `<div class="agent-chip">${AGENTS[loop.platform]?.name || loop.platform}</div>` : ''}
        <div class="agent-chip chip-time">${elapsed}</div>
        ${stale ? '<div class="agent-chip chip-warn">\u26a0 stale</div>' : ''}
      </div>
      ${hasGates ? `
      <div class="card-progress">
        <div class="progress-ring"><svg width="40" height="40" viewBox="0 0 40 40"><circle class="progress-ring-bg" cx="20" cy="20" r="18"/><circle class="progress-ring-fill" cx="20" cy="20" r="18" stroke="${color}" stroke-dasharray="${circ}" stroke-dashoffset="${offset}"/></svg><div class="progress-ring-text">${gp}/${gt}</div></div>
        <div class="progress-info"><div class="progress-label">${gp} of ${gt} gates passing</div><div class="progress-sub">${fmt(loop.totalTokens)} tokens</div></div>
      </div>` : `<div class="card-meta-row"><span class="card-meta">${fmt(loop.totalTokens)} tokens burned</span></div>`}
      <div class="level-bar"><div class="level-track"><div class="level-fill ${stale?'lv-orange':'lv-green'}" style="width:${hasGates ? pct : 50}%"></div></div></div>
    </div>`;
  }).join('');
}

function renderQueue(el) {
  if (!QUEUE.length) {
    el.innerHTML = `<div class="empty"><div class="empty-icon">\u2191</div><div class="empty-text">Queue empty</div><div class="empty-sub">Tap + to add a task</div></div>`;
    return;
  }
  el.innerHTML = QUEUE.map(t => `
    <div class="q-card">
      <div class="q-top">
        <div onclick="openCreate('${t.id}')" style="flex:1;cursor:pointer">
          <div class="q-num">#${t.id} \u00b7 ${ago(t.created) || 'just now'}</div>
          <div class="q-prompt">${t.prompt}</div>
        </div>
        <button class="del-btn" onclick="deleteTask('${t.id}')" aria-label="Delete">\u2715</button>
      </div>
    </div>`).join('');
}

function renderDone(el) {
  if (!DONE.length) {
    el.innerHTML = `<div class="empty"><div class="empty-icon">\u2713</div><div class="empty-text">Nothing completed yet</div><div class="empty-sub">Completed tasks appear here</div></div>`;
    return;
  }
  el.innerHTML = DONE.map(t => {
    const dots = (t.agents||[]).map(a => `<div class="agent-dot" style="background:${AGENTS[a]?.color||'#999'};width:6px;height:6px;border-radius:50%"></div>`).join('');
    return `<div class="d-card">
      <div class="d-prompt"><span class="d-check">${t.status === 'failed' ? '\u2717' : '\u2713'}</span>${t.prompt}</div>
      <div class="d-meta">${dots ? `<div class="d-agents-row">${dots}</div>` : ''}${t.platform ? `<span>${t.platform}</span>` : ''}<span>${fmt(t.tokens)} burned</span></div>
    </div>`;
  }).join('');
}

// ── Detail sheet ──
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
  const stale = isStale(l.created, 5);
  const elapsed = ago(l.created);
  const agentName = AGENTS[l.platform]?.name || l.platform || 'Unassigned';
  const agentColor = AGENTS[l.platform]?.color || '#999';

  // Elapsed time formatted nicely
  const startDate = l.created ? new Date(l.created) : null;
  const startStr = startDate ? startDate.toLocaleString(undefined, {month:'short',day:'numeric',hour:'numeric',minute:'2-digit'}) : '—';

  // Sessions placeholder (will be populated when we have session tracking)
  const sessCount = l.sessions?.length || 0;
  const sessHtml = sessCount > 0 ? l.sessions.map((s, i) => `
    <div class="detail-session">
      <div class="detail-ses-dot" style="background:${s.status === 'pass' ? 'var(--green)' : s.status === 'fail' ? 'var(--red)' : 'var(--orange)'}"></div>
      <div class="detail-ses-info">
        <div class="detail-ses-num">Session ${i+1}</div>
        ${s.summary ? `<div class="detail-ses-summary">${s.summary}</div>` : ''}
      </div>
      ${s.tokens ? `<div class="detail-ses-tokens">${fmt(s.tokens)}</div>` : ''}
    </div>`).join('') : `<div class="detail-empty-ses">No session history yet</div>`;

  document.getElementById('sheet-body').innerHTML = `
    <div class="detail-header">
      <div class="detail-status">
        <span class="spinner"></span>
        <span class="detail-status-text">Running</span>
        <span class="detail-elapsed">${elapsed}</span>
      </div>
      ${stale ? '<div class="detail-stale">\u26a0 No progress in 5+ min</div>' : ''}
    </div>

    <div class="sh-title" style="margin-bottom:4px">${l.prompt}</div>
    ${l.dir ? `<div class="sh-dir">${l.dir}</div>` : ''}

    <div class="detail-meta">
      <div class="detail-meta-item">
        <div class="detail-meta-label">Agent</div>
        <div class="detail-meta-val" style="color:${agentColor}">${agentName}</div>
      </div>
      <div class="detail-meta-item">
        <div class="detail-meta-label">Started</div>
        <div class="detail-meta-val">${startStr}</div>
      </div>
      <div class="detail-meta-item">
        <div class="detail-meta-label">Tokens</div>
        <div class="detail-meta-val">${fmt(l.totalTokens)}</div>
      </div>
      <div class="detail-meta-item">
        <div class="detail-meta-label">Sessions</div>
        <div class="detail-meta-val">${sessCount || '—'}</div>
      </div>
    </div>

    <div class="sh-section">
      <div class="sh-label">Activity</div>
      <div class="detail-sessions">${sessHtml}</div>
    </div>

    <div class="detail-live">
      <div class="detail-live-indicator"><span class="spinner" style="width:14px;height:14px;border-width:2px"></span></div>
      <div class="detail-live-text">Agent is working...</div>
    </div>

    <div class="sh-actions">
      <button class="btn btn-danger" onclick="deleteTask('${l.id}')">Delete Task</button>
      <button class="btn btn-secondary" onclick="closeSheet()">Close</button>
    </div>`;
}

// ── Create flow (stepped: task → agent → gateway → model → start) ──
let cStep = 0, cPrompt = '', cDir = '', cAgent = null, cRouter = null, cModel = null, cExistingId = null;

function openCreate(existingId) {
  cStep = existingId ? 1 : 0;
  cPrompt = ''; cDir = ''; cAgent = null; cRouter = null; cModel = null;
  cExistingId = existingId || null;
  renderCreate();
  document.getElementById('create-bg').classList.add('open');
  document.getElementById('create-sheet').classList.add('open');
  if (!existingId) setTimeout(() => { const el = document.getElementById('c-prompt'); if(el) el.focus(); }, 350);
}
function closeCreate() {
  document.getElementById('create-bg').classList.remove('open');
  document.getElementById('create-sheet').classList.remove('open');
}
function openAdd() { openCreate(); }

function renderCreate() {
  const body = document.getElementById('create-body');
  const labels = ['Task', 'Agent', 'Gateway', 'Model'];
  const dots = labels.map((s,i) => `<span class="step-dot ${i < cStep ? 'done' : ''} ${i === cStep ? 'current' : ''}">${i < cStep ? '\u2713' : i+1}</span>`).join('');
  const bar = `<div class="step-bar">${dots}</div>`;

  if (cStep === 0) {
    body.innerHTML = `
      <div class="sh-title">What needs doing?</div>${bar}
      <div class="add-form">
        <textarea id="c-prompt" class="add-input" rows="3" placeholder="Refactor the auth module..." oninput="cPrompt=this.value">${cPrompt}</textarea>
        <label class="add-label">Directory (optional)</label>
        <input class="add-input" type="text" placeholder="/home/exedev/myproject" value="${cDir}" oninput="cDir=this.value">
        <button class="btn btn-primary add-btn" onclick="cNextStep()">Next</button>
      </div>`;
  } else if (cStep === 1) {
    body.innerHTML = `
      <div class="sh-title">Pick an agent</div>${bar}
      <div class="pick-agents">
        ${Object.entries(AGENTS).map(([id,a]) => `
          <div class="pick-agent ${cAgent===id?'picked':''}" onclick="cAgent='${id}';renderCreate()">
            <div class="pick-agent-dot" style="background:${a.color}"></div>
            <div class="pick-agent-name">${a.name}</div>
          </div>`).join('')}
      </div>
      <div class="step-nav">
        ${!cExistingId ? '<button class="btn btn-secondary" onclick="cPrevStep()">Back</button>' : ''}
        <button class="btn btn-primary" style="flex:1" onclick="cNextStep()" ${!cAgent?'disabled':''}>Next</button>
      </div>`;
  } else if (cStep === 2) {
    body.innerHTML = `
      <div class="sh-title">Pick a gateway</div>${bar}
      <div class="pick-routers">
        ${Object.entries(ROUTERS).map(([id,r]) => `
          <div class="pick-router ${cRouter===id?'picked':''}" onclick="cRouter='${id}';renderCreate()" style="${cRouter===id?'color:'+r.color:''}">
            ${r.short}
          </div>`).join('')}
      </div>
      <div class="step-nav">
        <button class="btn btn-secondary" onclick="cPrevStep()">Back</button>
        <button class="btn btn-primary" style="flex:1" onclick="cNextStep()" ${!cRouter?'disabled':''}>Next</button>
      </div>`;
  } else if (cStep === 3) {
    const models = AGENTS[cAgent]?.models || [];
    body.innerHTML = `
      <div class="sh-title">Pick a model</div>${bar}
      <div class="model-list">
        ${models.map(m => `
          <div class="model-opt ${cModel===m?'picked':''}" onclick="cModel='${m}';renderCreate()">
            <span class="model-name">${m}</span>
            ${cModel===m ? '<span class="model-check">\u2713</span>' : ''}
          </div>`).join('')}
      </div>
      <div class="step-nav">
        <button class="btn btn-secondary" onclick="cPrevStep()">Back</button>
        <button class="btn btn-primary" style="flex:1" onclick="cSubmit()" ${!cModel?'disabled':''}>Start</button>
      </div>`;
  }
}

function cNextStep() {
  if (cStep === 0 && !cPrompt.trim()) return;
  if (cStep === 1 && !cAgent) return;
  if (cStep === 2 && !cRouter) return;
  cStep++;
  if (cStep === 3) cModel = null;
  renderCreate();
}
function cPrevStep() { cStep = Math.max(0, cStep - 1); renderCreate(); }

async function cSubmit() {
  if (!cAgent || !cRouter || !cModel) return;
  let taskId = cExistingId;
  if (!taskId) {
    try {
      const res = await fetch('/api/tasks', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({prompt:cPrompt.trim(), dir:cDir.trim()||undefined}) });
      if (!res.ok) { showToast('Create failed: ' + await res.text(), true); return; }
      const task = await res.json();
      taskId = task.id;
    } catch(e) { showToast('Create failed: ' + e.message, true); return; }
  }
  try {
    const res = await fetch('/api/tasks/run', { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({id:taskId, agent:cAgent, router:cRouter}) });
    if (!res.ok) { showToast('Start failed: ' + await res.text(), true); return; }
    closeCreate();
    showToast('Task started');
    currentTab = 'active';
    document.querySelectorAll('.tab').forEach(t => t.classList.toggle('active', t.dataset.tab === 'active'));
    await refresh();
  } catch(e) { showToast('Start failed: ' + e.message, true); }
}

// ── Tabs ──
document.querySelectorAll('.tab').forEach(t => t.addEventListener('click', () => {
  document.querySelectorAll('.tab').forEach(b => b.classList.remove('active'));
  t.classList.add('active');
  currentTab = t.dataset.tab;
  renderContent();
}));

// ── Settings drawer ──
async function openSettings() {
  document.getElementById('settings-bg').classList.add('open');
  document.getElementById('settings-sheet').classList.add('open');
  document.getElementById('settings-body').innerHTML = '<div class="sh-title">Loading...</div><div class="spinner" style="margin:20px auto;display:block;width:20px;height:20px"></div>';
  try {
    const res = await fetch('/api/config');
    if (!res.ok) throw new Error(await res.text());
    const cfg = await res.json();
    renderSettings(cfg);
  } catch(e) {
    document.getElementById('settings-body').innerHTML = `<div class="sh-title">Settings</div><div style="color:var(--red);padding:20px">Failed to load config: ${e.message}</div>`;
  }
}
function closeSettings() {
  document.getElementById('settings-bg').classList.remove('open');
  document.getElementById('settings-sheet').classList.remove('open');
}

let settingsCfg = null;
let editingKey = null; // env_var name currently being edited

function renderSettings(cfg) {
  settingsCfg = cfg;
  const body = document.getElementById('settings-body');
  
  // Agents section
  const agentsHtml = Object.entries(cfg.agents).map(([id, a]) => {
    const color = AGENTS[id]?.color || '#999';
    return `<div class="cfg-item">
      <div class="cfg-dot ${a.available ? 'ok' : 'miss'}"></div>
      <div class="cfg-info">
        <div class="cfg-name" style="color:${color}">${a.name}</div>
        <div class="cfg-detail">${a.note}</div>
      </div>
      <div class="cfg-badge ${a.available ? 'ok' : 'miss'}">${a.available ? 'Ready' : 'Missing'}</div>
    </div>`;
  }).join('');

  // Routers section with editable keys
  const routersHtml = Object.entries(cfg.routers).map(([id, r]) => {
    const allSet = r.keys.every(k => k.set);
    const someSet = r.keys.some(k => k.set);
    const dotCls = allSet ? 'ok' : someSet ? 'warn' : 'miss';
    const badgeCls = allSet ? 'ok' : 'miss';
    const badgeText = allSet ? 'Ready' : `${r.keys.filter(k=>!k.set).length} missing`;
    const routerColor = ROUTERS[id]?.color || '#999';
    
    const keysHtml = r.keys.map(k => {
      const isEditing = editingKey === k.env_var;
      if (isEditing) {
        return `<div class="cfg-key cfg-key-editing">
          <div class="cfg-key-name">${k.name}</div>
          <div class="cfg-key-edit-row">
            <input type="text" class="cfg-key-input" id="key-input-${k.env_var}" 
              placeholder="Paste key..." autocomplete="off" spellcheck="false">
            <button class="cfg-key-save" onclick="saveKey('${k.env_var}')">Save</button>
            <button class="cfg-key-cancel" onclick="cancelEditKey()">\u2715</button>
          </div>
        </div>`;
      }
      if (k.set) {
        return `<div class="cfg-key">
          <div class="cfg-key-name">${k.name}</div>
          <div class="cfg-key-val">${k.preview}</div>
          <button class="cfg-key-action cfg-key-edit" onclick="startEditKey('${k.env_var}')" title="Replace">\u270E</button>
          <button class="cfg-key-action cfg-key-del" onclick="deleteKey('${k.env_var}', '${k.name}')" title="Delete">\u2715</button>
        </div>`;
      }
      return `<div class="cfg-key cfg-key-missing">
        <div class="cfg-key-name">${k.name}</div>
        <div class="cfg-key-val cfg-key-env">${k.env_var}</div>
        <button class="cfg-key-action cfg-key-add" onclick="startEditKey('${k.env_var}')" title="Add key">+</button>
      </div>`;
    }).join('');

    return `<div class="cfg-item" style="flex-direction:column;align-items:stretch">
      <div style="display:flex;align-items:center;gap:10px">
        <div class="cfg-dot ${dotCls}"></div>
        <div class="cfg-info">
          <div class="cfg-name" style="color:${routerColor}">${r.name}</div>
        </div>
        <div class="cfg-badge ${badgeCls}">${badgeText}</div>
      </div>
      <div class="cfg-keys">${keysHtml}</div>
    </div>`;
  }).join('');

  // Count totals
  const allKeys = Object.values(cfg.routers).flatMap(r => r.keys);
  const setCount = allKeys.filter(k => k.set).length;
  const totalCount = allKeys.length;

  body.innerHTML = `
    <div class="sh-title">Settings</div>
    <div class="cfg-summary">${setCount} of ${totalCount} keys configured</div>
    <div class="cfg-section">
      <div class="sh-label">Agents</div>
      ${agentsHtml}
    </div>
    <div class="cfg-section">
      <div class="sh-label">API Keys</div>
      ${routersHtml}
    </div>
    <button class="btn btn-secondary" style="width:100%;margin-top:8px" onclick="closeSettings()">Done</button>`;

  // Focus input if editing
  if (editingKey) {
    setTimeout(() => {
      const inp = document.getElementById('key-input-' + editingKey);
      if (inp) inp.focus();
    }, 50);
  }
}

function startEditKey(envVar) {
  editingKey = envVar;
  if (settingsCfg) renderSettings(settingsCfg);
}

function cancelEditKey() {
  editingKey = null;
  if (settingsCfg) renderSettings(settingsCfg);
}

async function saveKey(envVar) {
  const inp = document.getElementById('key-input-' + envVar);
  const val = inp ? inp.value.trim() : '';
  if (!val) { showToast('Key cannot be empty', true); return; }
  try {
    const res = await fetch('/api/config/keys', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({key: envVar, value: val})
    });
    if (!res.ok) throw new Error(await res.text());
    editingKey = null;
    showToast('Key saved');
    // Reload config
    const cfgRes = await fetch('/api/config');
    if (cfgRes.ok) renderSettings(await cfgRes.json());
  } catch(e) { showToast('Save failed: ' + e.message, true); }
}

async function deleteKey(envVar, name) {
  if (!confirm(`Delete ${name}?`)) return;
  try {
    const res = await fetch('/api/config/keys', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({key: envVar, value: ''})
    });
    if (!res.ok) throw new Error(await res.text());
    showToast('Key removed');
    const cfgRes = await fetch('/api/config');
    if (cfgRes.ok) renderSettings(await cfgRes.json());
  } catch(e) { showToast('Delete failed: ' + e.message, true); }
}

// ── Theme ──
function toggleTheme() {
  const isDark = document.documentElement.getAttribute('data-theme') === 'dark';
  document.documentElement.setAttribute('data-theme', isDark ? '' : 'dark');
  localStorage.setItem('theme', isDark ? 'light' : 'dark');
}
(function(){ const s = localStorage.getItem('theme'); if (s === 'dark') document.documentElement.setAttribute('data-theme', 'dark'); })();

// ── Unlock flash ──
function showUnlock(taskName) {
  const div = document.createElement('div');
  div.className = 'unlock-flash';
  div.innerHTML = `<div class="unlock-content"><div class="unlock-ring">\u2713</div><div class="unlock-title">${taskName}</div><div class="unlock-sub">All gates passed</div></div>`;
  document.body.appendChild(div);
  setTimeout(() => div.remove(), 3600);
}

// ── Escape closes any open sheet ──
document.addEventListener('keydown', e => {
  if (e.key === 'Escape') { closeSheet(); closeCreate(); closeSettings(); }
});

// ── Poll + Init ──
async function refresh() {
  await fetchState();
  renderSummary();
  renderContent();
}
refresh();
setInterval(refresh, 3000);
