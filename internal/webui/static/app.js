// Kalita universal client: a pure projection of /api/meta. No business logic
// lives here — what this actor can see and press comes from the server.
import { h, render, html, useState, useEffect } from '/vendor/preact-standalone.module.js';

// --- session & api -----------------------------------------------------------

const session = {
  get() { try { return JSON.parse(localStorage.kalitaSession || 'null'); } catch { return null; } },
  set(s) { localStorage.kalitaSession = JSON.stringify(s); },
  clear() { delete localStorage.kalitaSession; },
};

async function api(path, opts = {}) {
  const s = session.get();
  const resp = await fetch(path, {
    ...opts,
    headers: {
      'Content-Type': 'application/json',
      'Authorization': s?.token ? `Bearer ${s.token}` : '',
      ...(opts.headers || {}),
    },
  });
  const body = await resp.json().catch(() => ({}));
  if (!resp.ok) throw body;
  return body;
}

let me = { id: '', role: '' }; // filled from /api/meta after sign-in
const basis = () => ({ type: 'human', id: me.id });

// --- routing (hash) ------------------------------------------------------------

function useRoute() {
  const [route, setRoute] = useState(location.hash.slice(1) || '/inbox');
  useEffect(() => {
    const f = () => setRoute(location.hash.slice(1) || '/inbox');
    addEventListener('hashchange', f);
    return () => removeEventListener('hashchange', f);
  }, []);
  return route;
}
const nav = (r) => { location.hash = r; };

// --- field rendering -------------------------------------------------------------

function FieldInput({ field, value, onChange }) {
  if (field.type === 'enum') return html`<select value=${value || ''} onChange=${e => onChange(e.target.value)}>
    <option value="">—</option>${field.values.map(v => html`<option value=${v}>${v}</option>`)}</select>`;
  if (field.type === 'bool') return html`<select value=${String(value ?? '')} onChange=${e => onChange(e.target.value === 'true')}>
    <option value="">—</option><option value="true">true</option><option value="false">false</option></select>`;
  if (field.type === 'text') return html`<textarea rows="3" value=${value || ''} onInput=${e => onChange(e.target.value)} />`;
  const numeric = ['int', 'float', 'money'].includes(field.type);
  return html`<input type=${numeric ? 'number' : field.type === 'date' ? 'date' : 'text'}
    value=${value ?? ''} onInput=${e => onChange(numeric ? Number(e.target.value) : e.target.value)} />`;
}

const fmt = (v) => v === null || v === undefined ? '' :
  typeof v === 'object' ? JSON.stringify(v) : String(v);

// --- views ----------------------------------------------------------------------

function Login() {
  const [token, setToken] = useState('');
  return html`<div class="login card">
    <h2>Kalita</h2>
    <div class="muted" style="margin-bottom:10px">Paste your access token (issued by the node admin: <code>kalita user add</code>). Passkeys arrive in v0.2.</div>
    <label>Access token</label><input type="password" value=${token} onInput=${e => setToken(e.target.value)} />
    <button class="btn green" onClick=${() => { if (token.trim()) { session.set({ token: token.trim() }); location.reload(); } }}>Enter</button>
  </div>`;
}

function Inbox({ meta, refresh }) {
  const [data, setData] = useState({ approvals: [], proposals: [], tasks: [] });
  const [err, setErr] = useState(null);
  const load = async () => {
    const [a, p, t] = await Promise.all([
      api('/api/approvals'), api('/api/proposals'), api('/api/tasks?status=open')]);
    setData({ approvals: a.approvals || [], proposals: p.proposals || [], tasks: t.tasks || [] });
  };
  useEffect(() => { load(); }, []);
  const decide = async (id, grant) => {
    setErr(null);
    try { await api(`/api/approvals/${id}/decide`, { method: 'POST', body: JSON.stringify({ grant, basis: basis() }) }); await load(); refresh(); }
    catch (e) { setErr(e); }
  };
  const decideProposal = async (id, grant) => {
    setErr(null);
    try { await api(`/api/proposals/${id}/decide`, { method: 'POST', body: JSON.stringify({ grant, basis: basis() }) }); await load(); refresh(); }
    catch (e) { setErr(e); }
  };
  return html`<div>
    <h2>Inbox</h2>
    ${err && html`<div class="err">${err.message || JSON.stringify(err)}</div>`}
    <h3>Signatures requested · ${data.approvals.length}</h3>
    ${data.approvals.map(a => html`<div class="card">
      <b>${a.action}</b>: ${a.entity} <span class="pill">${a.from} → ${a.to}</span>
      <div class="muted">requested by ${a.requested_by.id} (${a.requested_by.type}) · record <a onClick=${() => nav(`/e/${a.entity}/${a.record_id}`)}>${a.record_id.slice(0, 8)}…</a></div>
      <div style="margin-top:8px">
        <button class="btn green" onClick=${() => decide(a.id, true)}>Approve</button>
        <button class="btn red" onClick=${() => decide(a.id, false)}>Reject</button>
      </div></div>`)}
    <h3>Definition changes · ${data.proposals.length}</h3>
    ${data.proposals.map(p => html`<div class="card">
      <b>${p.description || 'proposal'}</b> <span class="muted">by ${p.author.id}</span>
      <pre>${(p.plan || []).join('\n')}</pre>
      <button class="btn green" onClick=${() => decideProposal(p.id, true)}>Sign & apply</button>
      <button class="btn red" onClick=${() => decideProposal(p.id, false)}>Reject</button>
    </div>`)}
    <h3>My role's tasks · ${data.tasks.length}</h3>
    ${data.tasks.map(t => html`<div class="card">
      <span class="pill">${t.kind}</span> ${t.action || ''} ${t.entity ? html` on <a onClick=${() => nav(`/e/${t.entity}/${t.record_id}`)}>${t.entity}</a>` : ''}
      <div class="muted">${t.args || ''}</div></div>`)}
  </div>`;
}

function CreateForm({ ent, onDone }) {
  const [vals, setVals] = useState({}); const [err, setErr] = useState(null);
  const writable = ent.fields.filter(f => !f.computed && f.name !== ent.workflow_field);
  const submit = async () => {
    setErr(null);
    try {
      await api(`/api/records/${ent.name}`, { method: 'POST', body: JSON.stringify({ values: vals, basis: basis() }) });
      onDone();
    } catch (e) { setErr(e); }
  };
  return html`<div class="card">
    <h3>New ${ent.name}</h3>
    ${writable.map(f => html`<label>${f.name}${f.required ? ' *' : ''}</label>
      <${FieldInput} field=${f} value=${vals[f.name]} onChange=${v => setVals({ ...vals, [f.name]: v })} />`)}
    ${err && html`<div class="err">${err.message} ${err.fix_hint ? `— ${err.fix_hint}` : ''}</div>`}
    <button class="btn green" onClick=${submit}>Create</button>
  </div>`;
}

function EntityList({ ent }) {
  const [rows, setRows] = useState([]); const [creating, setCreating] = useState(false);
  const cols = ent.ui.list_columns?.length ? ent.ui.list_columns : ent.fields.filter(f => f.readable).slice(0, 6).map(f => f.name);
  const load = () => api(`/api/records/${ent.name}`).then(r => setRows(r.records || []));
  useEffect(() => { load(); setCreating(false); }, [ent.name]);
  return html`<div>
    <h2>${ent.name} ${ent.ui.board_by && html`<a class="muted" style="font-size:13px" onClick=${() => nav(`/board/${ent.name}`)}>board view</a>`}</h2>
    ${ent.can_create && html`<button class="btn" onClick=${() => setCreating(!creating)}>${creating ? 'Cancel' : `+ New ${ent.name}`}</button>`}
    ${creating && html`<${CreateForm} ent=${ent} onDone=${() => { setCreating(false); load(); }} />`}
    <table style="margin-top:10px"><thead><tr>${cols.map(c => html`<th>${c}</th>`)}</tr></thead>
    <tbody>${rows.map(r => html`<tr onClick=${() => nav(`/e/${ent.name}/${r.id}`)}>
      ${cols.map(c => html`<td>${fmt(r.values[c])}</td>`)}</tr>`)}</tbody></table>
    ${rows.length === 0 && html`<div class="muted" style="margin-top:8px">no records visible to your role</div>`}
  </div>`;
}

function Board({ ent }) {
  const [rows, setRows] = useState([]);
  useEffect(() => { api(`/api/records/${ent.name}`).then(r => setRows(r.records || [])); }, [ent.name]);
  const field = ent.fields.find(f => f.name === ent.ui.board_by);
  const title = ent.ui.list_columns?.[0] || ent.fields[0]?.name;
  return html`<div>
    <h2>${ent.name} <a class="muted" style="font-size:13px" onClick=${() => nav(`/e/${ent.name}`)}>list view</a></h2>
    <div class="cols">${(field?.values || []).map(v => html`<div class="col"><h4>${v} · ${rows.filter(r => r.values[ent.ui.board_by] === v).length}</h4>
      ${rows.filter(r => r.values[ent.ui.board_by] === v).map(r => html`
        <div class="kcard" onClick=${() => nav(`/e/${ent.name}/${r.id}`)}>${fmt(r.values[title])}</div>`)}
    </div>`)}</div>
  </div>`;
}

function RecordView({ ent, id, refresh }) {
  const [rec, setRec] = useState(null); const [journal, setJournal] = useState(null);
  const [edit, setEdit] = useState({}); const [err, setErr] = useState(null); const [note, setNote] = useState(null);
  const load = () => api(`/api/records/${ent.name}/${id}`).then(setRec).catch(setErr);
  useEffect(() => { load(); setJournal(null); setEdit({}); }, [ent.name, id]);

  if (err) return html`<div class="err">${err.message}</div>`;
  if (!rec) return html`<div class="muted">loading…</div>`;
  const state = rec.values[ent.workflow_field];
  const actions = (ent.actions || []).filter(a => a.can_act && (a.from === state || a.from === 'any'));

  const act = async (action) => {
    setErr(null); setNote(null);
    try {
      const res = await api(`/api/records/${ent.name}/${id}/act`, { method: 'POST', body: JSON.stringify({ action, basis: basis() }) });
      if (res.status === 'pending_approval') setNote('parked for signature — see the approver’s inbox');
      load(); refresh();
    } catch (e) { setErr(e); }
  };
  const save = async () => {
    setErr(null);
    try { await api(`/api/records/${ent.name}/${id}`, { method: 'PATCH', body: JSON.stringify({ values: edit, basis: basis() }) }); setEdit({}); load(); }
    catch (e) { setErr(e); }
  };
  const showJournal = () => api(`/api/records/${ent.name}/${id}/journal`).then(j => setJournal(j.events));

  return html`<div>
    <h2>${ent.name} <span class="muted">${id.slice(0, 8)}…</span> ${state && html`<span class="pill">${state}</span>`}</h2>
    <div style="margin-bottom:10px">
      ${actions.map(a => html`<button class="btn" onClick=${() => act(a.action)}>${a.action}${a.requires_approval ? ' ✍' : ''}</button>`)}
      <button class="btn" onClick=${journal ? () => setJournal(null) : showJournal}>${journal ? 'hide journal' : 'journal'}</button>
    </div>
    ${note && html`<div class="muted" style="margin:8px 0">${note}</div>`}
    ${err && html`<div class="err">${err.message} ${err.rule ? `(${err.rule})` : ''} ${err.fix_hint || ''}</div>`}
    ${journal ? html`<div class="card">${journal.map(e => html`
        <div style="padding:4px 0;border-bottom:1px solid var(--line)">
          <span class="pill">${e.kind}</span> <b>${e.actor.id}</b> <span class="muted">(${e.actor.type})</span>
          ${e.basis && html`<span class="muted"> · basis: ${e.basis.type}:${e.basis.id.slice(0, 18)}</span>`}
          <pre style="margin:4px 0 0">${JSON.stringify(e.payload)}</pre>
        </div>`)}</div>`
      : html`<div class="card">
      ${ent.fields.filter(f => f.readable).map(f => {
        const editable = f.writable && f.name !== ent.workflow_field;
        return html`<label>${f.name}${f.computed ? ' (computed)' : ''}</label>
          ${editable
            ? html`<${FieldInput} field=${f} value=${f.name in edit ? edit[f.name] : rec.values[f.name]} onChange=${v => setEdit({ ...edit, [f.name]: v })} />`
            : html`<div style="padding:4px 0 10px">${fmt(rec.values[f.name]) || html`<span class="muted">—</span>`}</div>`}`;
      })}
      ${Object.keys(edit).length > 0 && html`<button class="btn green" onClick=${save}>Save changes</button>`}
    </div>`}
  </div>`;
}

// --- shell ----------------------------------------------------------------------

function App() {
  const route = useRoute();
  const [meta, setMeta] = useState(null); const [err, setErr] = useState(null);
  const [inboxCount, setInboxCount] = useState(0);
  const refresh = async () => {
    try {
      const [a, p] = await Promise.all([api('/api/approvals'), api('/api/proposals')]);
      setInboxCount((a.approvals?.length || 0) + (p.proposals?.length || 0));
    } catch { /* ignore */ }
  };
  useEffect(() => { api('/api/meta').then(m => { me = { id: m.actor_id, role: m.role }; setMeta(m); refresh(); }).catch(setErr); }, []);

  if (err) return html`<div class="login card"><div class="err">${err.message || 'node unreachable'}</div>
    <button class="btn" onClick=${() => { session.clear(); location.reload(); }}>Sign in again</button></div>`;
  if (!meta) return html`<div class="login muted">connecting…</div>`;

  const parts = route.split('/').filter(Boolean); // e.g. ["e","Card","id"]
  const ent = parts[0] === 'e' || parts[0] === 'board' ? meta.entities.find(x => x.name === parts[1]) : null;
  let view = html`<${Inbox} meta=${meta} refresh=${refresh} />`;
  if (parts[0] === 'e' && ent && parts[2]) view = html`<${RecordView} ent=${ent} id=${parts[2]} refresh=${refresh} />`;
  else if (parts[0] === 'e' && ent) view = html`<${EntityList} ent=${ent} />`;
  else if (parts[0] === 'board' && ent) view = html`<${Board} ent=${ent} />`;

  return html`<div class="shell">
    <div class="side">
      <h1>Kalita</h1>
      <div class="who">${meta.pack || '(genesis)'} · v${meta.def_version}<br/>${meta.actor_id} — ${meta.role}
        <a style="display:block" onClick=${() => { session.clear(); location.reload(); }}>sign out</a></div>
      <div class="nav">
        <a class=${route === '/inbox' ? 'on' : ''} onClick=${() => nav('/inbox')}>Inbox ${inboxCount > 0 && html`<span class="badge">${inboxCount}</span>`}</a>
        ${meta.entities.map(e => html`<a class=${parts[1] === e.name ? 'on' : ''} onClick=${() => nav(`/e/${e.name}`)}>${e.name}</a>`)}
      </div>
    </div>
    <div class="main">${view}</div>
  </div>`;
}

render(session.get() ? html`<${App} />` : html`<${Login} />`, document.getElementById('root'));
