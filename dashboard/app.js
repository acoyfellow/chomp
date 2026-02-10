let selectedLoop = null;
let activeFilter = 'all';
let dispatchTarget = null; // task being dispatched

function formatTokens(n) {
  if (n >= 1000000) return (n/1000000).toFixed(1) + 'M';
  if (n >= 1000) return (n/1000).toFixed(0) + 'k';
  return n.toString();
}

function agentBadge(agentId, model, size) {
  const a = AGENTS[agentId] || AGENTS['custom'];
  const sz = size || 'sm';
  return `<span class="agent-badge agent-${sz}" style="--agent-color:${a.color}" title="${a.name} / ${model}">
    <span class="agent-dot" style="background:${a.color}"></span>
    <span class="agent-name">${a.name}</span>
    <span class="agent-model">${model}</span>
  </span>`;
}

function commitPills(commits) {
  if (!commits || !commits.length) return '';
  return `<span class="commit-pills">${commits.map(c =>
    `<span class="commit-sha" title="${c}">${c.substring(0,7)}</span>`
  ).join('')}</span>`;
}

function renderHeaderStats() {
  const el = document.getElementById('header-stats');
  const activeCount = LOOPS.length;
  const totalBurn = [...LOOPS, ...DONE].reduce((s, t) => s + (t.totalTokens || t.tokens || 0), 0);
  const totalSessions = LOOPS.reduce((s, l) => s + l.sessions.length, 0);
  const totalCommits = LOOPS.reduce((s, l) => s + l.sessions.reduce((ss, se) => ss + (se.commits?.length || 0), 0), 0);
  // count unique agents in use
  const agentsInUse = new Set();
  LOOPS.forEach(l => l.sessions.forEach(s => agentsInUse.add(s.agent)));
  el.innerHTML = `
    <div class="stat"><span class="live-dot"></span> <span class="stat-value">${activeCount}</span> loops</div>
    <div class="stat">\u26a1 <span class="stat-value">${totalSessions}</span> sessions</div>
    <div class="stat">\ud83d\udd25 <span class="stat-value burn">${formatTokens(totalBurn)}</span> burned</div>
    <div class="stat">\ud83d\udcbe <span class="stat-value">${totalCommits}</span> commits</div>
    <div class="stat">\ud83e\udd16 <span class="stat-value">${agentsInUse.size}</span> agents</div>
  `;
}

function renderQueue() {
  document.getElementById('queue-count').textContent = QUEUE.length;
  document.getElementById('queue-list').innerHTML = QUEUE.map(t => `
    <div class="task-item" onclick="showDispatch('${t.id}')">
      <div class="task-id">#${t.id} <span class="dispatch-hint">click to dispatch</span></div>
      <div class="task-prompt">${t.prompt}</div>
    </div>
  `).join('');
}

function renderDone() {
  document.getElementById('done-count').textContent = DONE.length;
  document.getElementById('done-list').innerHTML = DONE.map(t => {
    const agentTags = (t.agents||[]).map(a => {
      const ag = AGENTS[a.agent] || AGENTS['custom'];
      return `<span class="done-agent" style="color:${ag.color}">${ag.name}\u00b7${a.commits}c</span>`;
    }).join(' ');
    return `<div class="task-item done">
      <div class="task-id">#${t.id} \u00b7 ${formatTokens(t.tokens)}</div>
      <div class="task-prompt">${t.prompt}</div>
      <div class="done-agents">${agentTags}</div>
    </div>`;
  }).join('');
}

function renderLoops() {
  const grid = document.getElementById('loops-grid');
  const filtered = activeFilter === 'all' ? LOOPS : LOOPS.filter(l => l.state === activeFilter);

  grid.innerHTML = filtered.map(loop => {
    const gatesPass = loop.gates.filter(g => g.status === 'pass').length;
    const isSelected = selectedLoop && selectedLoop.id === loop.id;
    // unique agents used
    const agentSet = new Map();
    loop.sessions.forEach(s => {
      const key = s.agent;
      if (!agentSet.has(key)) agentSet.set(key, { agent: s.agent, count: 0 });
      agentSet.get(key).count++;
    });
    const agentIcons = [...agentSet.values()].map(a => {
      const ag = AGENTS[a.agent] || AGENTS['custom'];
      return `<span class="loop-agent-dot" style="background:${ag.color}" title="${ag.name} (${a.count} sessions)"></span>`;
    }).join('');

    return `
    <div class="loop-card state-${loop.state} ${isSelected ? 'selected' : ''}" onclick="selectLoop('${loop.id}')">
      <div class="loop-top">
        <div>
          <div class="loop-title">${loop.prompt}</div>
          <div class="loop-id">#${loop.id} \u00b7 ${loop.dir.split('/').pop()} \u00b7 ${agentIcons}</div>
        </div>
        <span class="loop-state ${loop.state}">${loop.state}</span>
      </div>
      <div class="loop-meta">
        <span>\ud83d\udd04 ${loop.sessions.length} sessions</span>
        <span>\ud83d\udee1\ufe0f ${gatesPass}/${loop.gates.length} gates</span>
        <span>\ud83d\udd25 ${formatTokens(loop.totalTokens)}</span>
        <span>\ud83d\udcbe ${loop.sessions.reduce((s,se) => s + (se.commits?.length||0), 0)} commits</span>
      </div>
      <div class="session-timeline">
        ${loop.sessions.map(s => {
          const ag = AGENTS[s.agent] || AGENTS['custom'];
          return `<div class="session-pip ${s.outcome === 'active' ? 'active' : 'done-' + s.outcome}" style="border-bottom: 2px solid ${ag.color}" title="${ag.name}/${s.model} \u2014 ${s.outcome}">${s.id}</div>`;
        }).join('')}
      </div>
      <div class="gate-bar">
        ${loop.gates.map(g => `<div class="gate-pip ${g.status}"></div>`).join('')}
      </div>
    </div>`;
  }).join('');
}

function selectLoop(id) {
  selectedLoop = LOOPS.find(l => l.id === id) || null;
  renderLoops();
  renderDetail();
}

function renderDetail() {
  const panel = document.getElementById('detail-panel');
  if (!selectedLoop) {
    panel.innerHTML = '<div class="detail-empty">Select a loop to inspect</div>';
    return;
  }
  const loop = selectedLoop;
  const gatesPass = loop.gates.filter(g => g.status === 'pass').length;
  const burnPct = Math.min(100, (loop.totalTokens / 1500000) * 100);
  const totalCommits = loop.sessions.reduce((s,se) => s + (se.commits?.length||0), 0);

  // Agent breakdown
  const agentMap = new Map();
  loop.sessions.forEach(s => {
    const key = `${s.agent}/${s.model}`;
    if (!agentMap.has(key)) agentMap.set(key, { agent: s.agent, model: s.model, sessions: 0, tokens: 0, commits: 0 });
    const entry = agentMap.get(key);
    entry.sessions++;
    entry.tokens += s.tokens;
    entry.commits += (s.commits?.length || 0);
  });

  panel.innerHTML = `
    <div class="detail-header">
      <div class="detail-title">#${loop.id} ${loop.prompt}</div>
      <div class="detail-prompt">${loop.dir}</div>
      <span class="loop-state ${loop.state}">${loop.state}</span>
    </div>

    <div class="detail-section">
      <h4>Token Burn</h4>
      <div style="display:flex;justify-content:space-between;font-size:11px;color:var(--text2);margin-bottom:4px">
        <span>${formatTokens(loop.totalTokens)} used</span>
        <span style="color:var(--text3)">~1.5M budget</span>
      </div>
      <div class="burn-bar"><div class="burn-fill" style="width:${burnPct}%"></div></div>
    </div>

    <div class="detail-section">
      <h4>Agent Breakdown</h4>
      ${[...agentMap.values()].map(a => {
        const ag = AGENTS[a.agent] || AGENTS['custom'];
        return `<div class="agent-row">
          ${agentBadge(a.agent, a.model)}
          <div class="agent-stats">
            <span>${a.sessions}s</span>
            <span>${formatTokens(a.tokens)}</span>
            <span>${a.commits}c</span>
          </div>
        </div>`;
      }).join('')}
    </div>

    <div class="detail-section">
      <h4>Gates (${gatesPass}/${loop.gates.length})</h4>
      ${loop.gates.map(g => `
        <div class="gate-row">
          <span class="gate-icon">${g.status === 'pass' ? '\u2705' : g.status === 'fail' ? '\u274c' : '\u2b1c'}</span>
          <span class="gate-name">${g.name}</span>
          <span class="gate-status ${g.status}">${g.status}</span>
        </div>
      `).join('')}
    </div>

    <div class="detail-section">
      <h4>Sessions (${loop.sessions.length}) \u00b7 ${totalCommits} commits</h4>
      ${loop.sessions.map(s => {
        const ag = AGENTS[s.agent] || AGENTS['custom'];
        return `
        <div class="session-row">
          <span class="session-dot ${s.outcome === 'active' ? 'active' : s.outcome === 'pass' ? 'pass' : s.outcome === 'fail' ? 'fail' : 'handoff'}"></span>
          <span class="session-label">
            <strong>#${s.id}</strong> ${s.summary.substring(0, 55)}${s.summary.length > 55 ? '...' : ''}
          </span>
        </div>
        <div class="session-meta">
          ${agentBadge(s.agent, s.model, 'xs')}
          <span>${formatTokens(s.tokens)}</span>
          <span>${s.duration}</span>
          ${commitPills(s.commits)}
        </div>`;
      }).join('')}
    </div>

    <div class="detail-section">
      <h4>Git Audit</h4>
      <div class="git-audit">
        <code>git log --format="%h %s [%an]"</code>
        <div class="git-log">
          ${loop.sessions.slice().reverse().flatMap(s => {
            const ag = AGENTS[s.agent] || AGENTS['custom'];
            return (s.commits||[]).map(c =>
              `<div class="git-line"><span class="git-sha">${c.substring(0,7)}</span> <span class="git-msg">${s.summary.substring(0,40)}</span> <span class="git-agent" style="color:${ag.color}">[${ag.name}/${s.model}]</span></div>`
            );
          }).join('')}
        </div>
      </div>
    </div>

    <div class="detail-actions">
      ${loop.state === 'failing' ? '<button class="btn btn-danger" onclick="alert(\'Kill loop #' + loop.id + '\'">Kill</button>' : ''}
      ${loop.state === 'running' ? '<button class="btn btn-warn" onclick="alert(\'Pause #' + loop.id + '\'">Pause</button>' : ''}
      <button class="btn" onclick="showSwapAgent('${loop.id}')">Swap Agent</button>
      <button class="btn" onclick="alert(\'Re-dispatch #' + loop.id + '\'">Re-dispatch</button>
    </div>
  `;
}

// --- Dispatch modal (pick agent + model for a queued task) ---
function showDispatch(taskId) {
  dispatchTarget = QUEUE.find(t => t.id === taskId);
  if (!dispatchTarget) return;
  renderModal('dispatch');
}

function showSwapAgent(loopId) {
  const loop = LOOPS.find(l => l.id === loopId);
  if (!loop) return;
  dispatchTarget = loop;
  renderModal('swap');
}

function renderModal(mode) {
  let existing = document.getElementById('modal-overlay');
  if (existing) existing.remove();

  const title = mode === 'dispatch'
    ? `Dispatch #${dispatchTarget.id}: ${dispatchTarget.prompt.substring(0,50)}...`
    : `Swap agent for loop #${dispatchTarget.id}`;

  const overlay = document.createElement('div');
  overlay.id = 'modal-overlay';
  overlay.className = 'modal-overlay';
  overlay.onclick = (e) => { if (e.target === overlay) overlay.remove(); };

  overlay.innerHTML = `
    <div class="modal">
      <div class="modal-title">${title}</div>
      <div class="modal-subtitle">Pick an agent and model</div>
      <div class="agent-grid">
        ${Object.entries(AGENTS).map(([id, a]) => `
          <div class="agent-card" onclick="selectAgent(this, '${id}')" data-agent="${id}">
            <div class="agent-card-dot" style="background:${a.color}"></div>
            <div class="agent-card-name">${a.name}</div>
            <div class="agent-card-cli">${a.cli || 'custom'}</div>
            <select class="agent-model-select" onclick="event.stopPropagation()">
              ${a.models.map(m => `<option value="${m}">${m}</option>`).join('')}
            </select>
          </div>
        `).join('')}
      </div>
      <div class="modal-footer">
        <button class="btn" onclick="document.getElementById('modal-overlay').remove()">Cancel</button>
        <button class="btn btn-primary" onclick="confirmDispatch('${mode}')">\ud83d\ude80 ${mode === 'dispatch' ? 'Dispatch' : 'Swap & Restart'}</button>
      </div>
    </div>
  `;
  document.body.appendChild(overlay);
}

let selectedAgent = null;
function selectAgent(el, agentId) {
  document.querySelectorAll('.agent-card').forEach(c => c.classList.remove('selected'));
  el.classList.add('selected');
  selectedAgent = agentId;
}

function confirmDispatch(mode) {
  if (!selectedAgent) { alert('Pick an agent first'); return; }
  const card = document.querySelector(`.agent-card[data-agent="${selectedAgent}"]`);
  const model = card.querySelector('.agent-model-select').value;
  const a = AGENTS[selectedAgent];

  if (mode === 'dispatch') {
    alert(`Would run:\nchomp run --agent ${selectedAgent} --model ${model}\n\nTask: ${dispatchTarget.prompt}\n\nCommit trailer:\nAgent: ${a.name}/${model}`);
  } else {
    alert(`Would swap loop #${dispatchTarget.id} to ${a.name}/${model}\n\nNext session will use this agent.\nAll commits tagged: Agent: ${a.name}/${model}`);
  }
  document.getElementById('modal-overlay').remove();
  selectedAgent = null;
}

// Filter buttons
document.querySelectorAll('.filter-btn').forEach(btn => {
  btn.addEventListener('click', () => {
    document.querySelectorAll('.filter-btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    activeFilter = btn.dataset.filter;
    renderLoops();
  });
});

function addTask() {
  const prompt = window.prompt('Task prompt:');
  if (prompt) alert('Would run: chomp add "' + prompt + '"');
}

// Simulate token burn ticking
setInterval(() => {
  LOOPS.forEach(l => {
    if (l.state === 'running') {
      l.totalTokens += Math.floor(Math.random() * 800) + 200;
      const active = l.sessions.find(s => s.outcome === 'active');
      if (active) active.tokens += Math.floor(Math.random() * 800) + 200;
    }
  });
  renderHeaderStats();
  if (selectedLoop) renderDetail();
}, 2000);

// Init
renderHeaderStats();
renderQueue();
renderDone();
renderLoops();
