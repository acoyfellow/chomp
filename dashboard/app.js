let sel = null, filter = 'all';

const fmt = n => n >= 1e6 ? (n/1e6).toFixed(1)+'M' : n >= 1e3 ? (n/1e3).toFixed(0)+'k' : ''+n;

function stats() {
  const el = document.getElementById('top-stats');
  const burns = [...LOOPS,...DONE].reduce((s,t) => s+(t.totalTokens||t.tokens||0), 0);
  const sess = LOOPS.reduce((s,l) => s+l.sessions.length, 0);
  const commits = LOOPS.reduce((s,l) => s+l.sessions.reduce((ss,se) => ss+(se.commits?.length||0),0),0);
  const agents = new Set(); LOOPS.forEach(l => l.sessions.forEach(s => agents.add(s.agent)));
  el.innerHTML = `
    <div class="stat-pill"><span class="live-pip"></span><span class="stat-val">${LOOPS.length}</span> live</div>
    <div class="stat-pill"><span class="stat-val">${sess}</span> sessions</div>
    <div class="stat-pill"><span class="stat-val hot">${fmt(burns)}</span> burned</div>
    <div class="stat-pill"><span class="stat-val">${commits}</span> commits</div>
    <div class="stat-pill"><span class="stat-val">${agents.size}</span> agents</div>`;
}

function queue() {
  document.getElementById('queue').innerHTML = QUEUE.map(t => `
    <div class="q-item" onclick="dispatch('${t.id}')">
      <div class="q-id">#${t.id} <span class="q-hint">dispatch \u2192</span></div>
      <div class="q-text">${t.prompt}</div>
    </div>`).join('');
}

function done() {
  document.getElementById('completed').innerHTML = DONE.map(t => {
    const dots = (t.agents||[]).map(a => `<div class="d-dot" style="background:${AGENTS[a]?.color||'#555'}" title="${AGENTS[a]?.name||a}"></div>`).join('');
    return `<div class="d-item">
      <div class="q-id">#${t.id} \u00b7 ${fmt(t.tokens)} \u00b7 ${t.sessions}s</div>
      <div class="q-text">${t.prompt}</div>
      <div class="d-agents">${dots}</div>
    </div>`;
  }).join('');
}

function loops() {
  const list = filter === 'all' ? LOOPS : LOOPS.filter(l => l.state === filter || (filter === 'handoff' && l.state === 'handoff'));
  document.getElementById('loops').innerHTML = list.map(loop => {
    const gp = loop.gates.filter(g=>g.status==='pass').length;
    const cm = loop.sessions.reduce((s,se)=>s+(se.commits?.length||0),0);
    const ag = [...new Set(loop.sessions.map(s=>s.agent))];
    return `
    <div class="loop ${sel?.id===loop.id?'sel':''}" onclick="pick('${loop.id}')">
      <div class="loop-indicator ${loop.state}"></div>
      <div class="loop-row1">
        <div class="loop-title">${loop.prompt}</div>
        <div class="loop-badge ${loop.state}">${loop.state}</div>
      </div>
      <div class="loop-row2">
        <div class="agent-dots">${ag.map(a=>`<div class="agent-dot-sm" style="background:${AGENTS[a]?.color}" title="${AGENTS[a]?.name}"></div>`).join('')}</div>
        <div class="loop-meta"><b>${loop.sessions.length}</b> sessions</div>
        <div class="loop-meta"><b>${gp}</b>/${loop.gates.length} gates</div>
        <div class="loop-meta"><b>${fmt(loop.totalTokens)}</b> tokens</div>
        <div class="loop-meta"><b>${cm}</b> commits</div>
      </div>
      <div class="chain">
        ${loop.sessions.map(s => {
          const a = AGENTS[s.agent];
          return `<div class="chain-pip c-${s.outcome === 'active' ? 'active' : s.outcome}" title="#${s.id} ${a?.name}/${s.model}">${s.id}</div>`;
        }).join('')}
      </div>
      <div class="gate-track">
        ${loop.gates.map(g=>`<div class="gate-seg g-${g.status}"></div>`).join('')}
      </div>
    </div>`;
  }).join('');
}

function pick(id) {
  sel = LOOPS.find(l=>l.id===id)||null;
  loops(); inspector();
}

function inspector() {
  const el = document.getElementById('inspector');
  if (!sel) { el.innerHTML = '<div class="inspector-empty"><div class="inspector-empty-icon">\u25ce</div><div>Select a loop</div></div>'; return; }
  const l = sel;
  const gp = l.gates.filter(g=>g.status==='pass').length;
  const bp = Math.min(100, l.totalTokens/1500000*100);
  const cm = l.sessions.reduce((s,se)=>s+(se.commits?.length||0),0);

  // agent breakdown
  const am = new Map();
  l.sessions.forEach(s => {
    const k = s.agent+'|'+s.model+'|'+s.router;
    if (!am.has(k)) am.set(k, {agent:s.agent,model:s.model,router:s.router,sess:0,tok:0,cm:0});
    const e = am.get(k); e.sess++; e.tok+=s.tokens; e.cm+=(s.commits?.length||0);
  });

  el.innerHTML = `
  <div class="ins-head">
    <div class="ins-id">#${l.id} \u00b7 <span class="loop-badge ${l.state}" style="display:inline">${l.state}</span></div>
    <div class="ins-title">${l.prompt}</div>
    <div class="ins-dir">${l.dir}</div>
  </div>

  <div class="ins-section">
    <div class="ins-label">Token Burn</div>
    <div class="tok-row"><span>${fmt(l.totalTokens)}</span><span>~1.5M</span></div>
    <div class="tok-bar"><div class="tok-fill" style="width:${bp}%"></div></div>
  </div>

  <div class="ins-section">
    <div class="ins-label">Agents \u00b7 Routers</div>
    ${[...am.values()].map(a => {
      const ag = AGENTS[a.agent]; const rt = ROUTERS[a.router];
      return `<div class="ab-row">
        <div class="ab-dot" style="background:${ag?.color}"></div>
        <div class="ab-name">${ag?.name} <span style="color:var(--t4)">/ ${a.model}</span></div>
        <div class="ab-router" style="border-left:2px solid ${rt?.color||'#555'}">${rt?.short||a.router}</div>
      </div>
      <div style="display:flex;gap:10px;padding:0 0 4px 16px;font-family:var(--mono);font-size:9px;color:var(--t4)">
        <span>${a.sess} sess</span><span>${fmt(a.tok)} tok</span><span>${a.cm} commits</span>
      </div>`;
    }).join('')}
  </div>

  <div class="ins-section">
    <div class="ins-label">Gates ${gp}/${l.gates.length}</div>
    ${l.gates.map(g => `
      <div class="gate-row">
        <div class="gate-icon">${g.status==='pass'?'\u25cf':g.status==='fail'?'\u25cf':'\u25cb'}</div>
        <div class="gate-name" style="${g.status==='pass'?'color:var(--green)':g.status==='fail'?'color:var(--red)':''}">${g.name}</div>
        <div class="gate-st g-${g.status}">${g.status}</div>
      </div>`).join('')}
  </div>

  <div class="ins-section">
    <div class="ins-label">Sessions ${l.sessions.length} \u00b7 ${cm} commits</div>
    ${l.sessions.map(s => {
      const ag = AGENTS[s.agent]; const rt = ROUTERS[s.router];
      return `<div class="ses-item">
        <div class="ses-row1">
          <div class="ses-dot s-${s.outcome==='active'?'active':s.outcome}"></div>
          <div class="ses-num">#${s.id}</div>
          <div class="ses-sum">${s.summary}</div>
        </div>
        <div class="ses-row2">
          <span class="ses-agent"><span class="ses-agent-dot" style="background:${ag?.color}"></span>${ag?.name} / ${s.model}</span>
          <span class="ses-agent" style="border-left:2px solid ${rt?.color}">${rt?.short}</span>
          <span class="ses-meta">${fmt(s.tokens)} \u00b7 ${s.duration}</span>
          ${(s.commits||[]).map(c=>`<span class="ses-sha">${c.substring(0,7)}</span>`).join('')}
        </div>
      </div>`;
    }).join('')}
  </div>

  <div class="ins-section">
    <div class="ins-label">Git Audit</div>
    ${l.sessions.slice().reverse().flatMap(s => {
      const ag = AGENTS[s.agent];
      return (s.commits||[]).map(c =>
        `<div class="git-line"><span class="git-sha">${c.substring(0,7)}</span><span class="git-msg">${s.summary.substring(0,30)}</span><span class="git-tag" style="color:${ag?.color}">${ag?.name}/${s.model}</span></div>`
      );
    }).join('')}
  </div>

  <div class="ins-actions">
    ${l.state==='failing'?'<button class="btn-s danger">Kill</button>':''}
    <button class="btn-s" onclick="swapAgent('${l.id}')">Swap Agent</button>
    <button class="btn-s primary" onclick="dispatch(null,'${l.id}')">Re-dispatch</button>
  </div>`;
}

// === MODAL ===
let pickedAgent = null, pickedRouter = null;

function dispatch(queueId, loopId) {
  pickedAgent = null; pickedRouter = null;
  const title = queueId
    ? `Dispatch #${queueId}`
    : `Re-dispatch loop #${loopId}`;
  openModal(title, 'dispatch');
}
function swapAgent(loopId) {
  pickedAgent = null; pickedRouter = null;
  openModal(`Swap agent for #${loopId}`, 'swap');
}

function openModal(title, mode) {
  const root = document.getElementById('modal-root');
  root.innerHTML = `
  <div class="modal-bg" onclick="if(event.target===this)closeModal()">
    <div class="modal-box">
      <div class="modal-title">${title}</div>
      <div class="modal-sub">Choose agent, model, and router</div>
      <div class="modal-grid">
        ${Object.entries(AGENTS).map(([id,a]) => `
          <div class="modal-agent" data-a="${id}" onclick="pickAgent(this,'${id}')">
            <div class="modal-agent-dot" style="background:${a.color}"></div>
            <div class="modal-agent-name">${a.name}</div>
            <div class="modal-agent-cli">${a.icon}</div>
          </div>`).join('')}
      </div>
      <div class="modal-router-section">
        <div class="ins-label">Router</div>
        <div class="router-opts">
          ${Object.entries(ROUTERS).map(([id,r]) => `
            <div class="router-opt" data-r="${id}" onclick="pickRouter(this,'${id}')">
              <div class="router-name" style="color:${r.color}">${r.short}</div>
            </div>`).join('')}
        </div>
      </div>
      <div class="modal-foot">
        <button class="btn-s" onclick="closeModal()">Cancel</button>
        <button class="btn-s primary" onclick="confirmModal('${mode}')">${mode==='dispatch'?'\ud83d\ude80 Dispatch':'Swap & Go'}</button>
      </div>
    </div>
  </div>`;
}
function pickAgent(el, id) {
  document.querySelectorAll('.modal-agent').forEach(e=>e.classList.remove('picked'));
  el.classList.add('picked'); pickedAgent = id;
}
function pickRouter(el, id) {
  document.querySelectorAll('.router-opt').forEach(e=>e.classList.remove('picked'));
  el.classList.add('picked'); pickedRouter = id;
}
function closeModal() { document.getElementById('modal-root').innerHTML = ''; }
function confirmModal(mode) {
  if (!pickedAgent||!pickedRouter) return;
  const a = AGENTS[pickedAgent], r = ROUTERS[pickedRouter];
  closeModal();
  // In real: chomp run --agent shelley --router cf-ai --model claude-sonnet-4
}

// === TABS ===
document.querySelectorAll('.tab').forEach(t => t.addEventListener('click', () => {
  document.querySelectorAll('.tab').forEach(b=>b.classList.remove('active'));
  t.classList.add('active'); filter = t.dataset.f; loops();
}));

function addTask() {
  const p = prompt('Task:'); if(p) alert('chomp add "'+p+'"');
}

// Tick
setInterval(() => {
  LOOPS.forEach(l => {
    if (l.state==='running') {
      l.totalTokens += Math.floor(Math.random()*600)+100;
      const a = l.sessions.find(s=>s.outcome==='active');
      if(a) a.tokens += Math.floor(Math.random()*600)+100;
    }
  });
  stats();
  if(sel) inspector();
}, 2500);

// Init
stats(); queue(); done(); loops();
