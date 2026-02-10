let selectedLoop = null;
let activeFilter = 'all';

function formatTokens(n) {
  if (n >= 1000000) return (n/1000000).toFixed(1) + 'M';
  if (n >= 1000) return (n/1000).toFixed(0) + 'k';
  return n.toString();
}

function renderHeaderStats() {
  const el = document.getElementById('header-stats');
  const activeCount = LOOPS.length;
  const totalBurn = [...LOOPS, ...DONE].reduce((s, t) => s + (t.totalTokens || t.tokens || 0), 0);
  const totalSessions = LOOPS.reduce((s, l) => s + l.sessions.length, 0);
  el.innerHTML = `
    <div class="stat"><span class="live-dot"></span> <span class="stat-value">${activeCount}</span> active</div>
    <div class="stat">⚡ <span class="stat-value">${totalSessions}</span> sessions</div>
    <div class="stat">🔥 <span class="stat-value burn">${formatTokens(totalBurn)}</span> tokens burned</div>
    <div class="stat">✅ <span class="stat-value">${DONE.length}</span> completed</div>
  `;
}

function renderQueue() {
  document.getElementById('queue-count').textContent = QUEUE.length;
  document.getElementById('queue-list').innerHTML = QUEUE.map(t => `
    <div class="task-item">
      <div class="task-id">#${t.id}</div>
      <div class="task-prompt">${t.prompt}</div>
    </div>
  `).join('');
}

function renderDone() {
  document.getElementById('done-count').textContent = DONE.length;
  document.getElementById('done-list').innerHTML = DONE.map(t => `
    <div class="task-item done">
      <div class="task-id">#${t.id} · ${formatTokens(t.tokens)} · ${t.platform}</div>
      <div class="task-prompt">${t.prompt}</div>
    </div>
  `).join('');
}

function renderLoops() {
  const grid = document.getElementById('loops-grid');
  const filtered = activeFilter === 'all' ? LOOPS : LOOPS.filter(l => l.state === activeFilter);

  grid.innerHTML = filtered.map(loop => {
    const gatesPass = loop.gates.filter(g => g.status === 'pass').length;
    const gatesTotal = loop.gates.length;
    const isSelected = selectedLoop && selectedLoop.id === loop.id;

    return `
    <div class="loop-card state-${loop.state} ${isSelected ? 'selected' : ''}" onclick="selectLoop('${loop.id}')">
      <div class="loop-top">
        <div>
          <div class="loop-title">${loop.prompt}</div>
          <div class="loop-id">#${loop.id} · ${loop.platform} · ${loop.dir.split('/').pop()}</div>
        </div>
        <span class="loop-state ${loop.state}">${loop.state}</span>
      </div>
      <div class="loop-meta">
        <span>🔄 ${loop.sessions.length} sessions</span>
        <span>🛡️ ${gatesPass}/${gatesTotal} gates</span>
        <span>🔥 ${formatTokens(loop.totalTokens)}</span>
      </div>
      <div class="session-timeline">
        ${loop.sessions.map(s => `<div class="session-pip ${s.outcome === 'active' ? 'active' : 'done-' + s.outcome}" title="Session ${s.id}: ${s.outcome}">${s.id}</div>`).join('')}
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

  panel.innerHTML = `
    <div class="detail-header">
      <div class="detail-title">#${loop.id} ${loop.prompt}</div>
      <div class="detail-prompt">${loop.platform} · ${loop.dir}</div>
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
      <h4>Gates (${gatesPass}/${loop.gates.length})</h4>
      ${loop.gates.map(g => `
        <div class="gate-row">
          <span class="gate-icon">${g.status === 'pass' ? '✅' : g.status === 'fail' ? '❌' : '⬜'}</span>
          <span class="gate-name">${g.name}</span>
          <span class="gate-status ${g.status}">${g.status}</span>
        </div>
      `).join('')}
    </div>

    <div class="detail-section">
      <h4>Sessions (${loop.sessions.length})</h4>
      ${loop.sessions.map(s => `
        <div class="session-row">
          <span class="session-dot ${s.outcome === 'active' ? 'active' : s.outcome === 'pass' ? 'pass' : s.outcome === 'fail' ? 'fail' : 'handoff'}"></span>
          <span class="session-label">
            <strong>#${s.id}</strong> ${s.summary.substring(0, 60)}${s.summary.length > 60 ? '...' : ''}
          </span>
        </div>
        <div style="display:flex;gap:12px;padding:0 8px 6px 24px;font-size:10px;color:var(--text3)">
          <span>${formatTokens(s.tokens)} tokens</span>
          <span>${s.duration}</span>
          <span>${s.outcome}</span>
        </div>
      `).join('')}
    </div>

    <div class="detail-actions">
      ${loop.state === 'failing' ? '<button class="btn btn-danger" onclick="alert(\'Would drop task #' + loop.id + '\')">Kill Loop</button>' : ''}
      ${loop.state === 'running' ? '<button class="btn btn-warn" onclick="alert(\'Would pause task #' + loop.id + '\')">Pause</button>' : ''}
      <button class="btn" onclick="alert(\'Would re-dispatch task #' + loop.id + '\')">Re-dispatch</button>
    </div>
  `;
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
