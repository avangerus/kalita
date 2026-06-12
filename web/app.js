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

// i18n: prefer the declared label, fall back to the raw name.
const elab = (e) => e.label || e.name;
const flab = (f) => f.label || f.name;
const colLabel = (ent, c) => { const f = ent.fields.find(x => x.name === c); return f ? flab(f) : c; };
// humanize a workflow action identifier for a button: close_incident → Close incident
const humanize = (s) => s.replace(/_/g, ' ').replace(/^./, c => c.toUpperCase());

// reclabel: a short human label for one record — its first non-uuid string value.
const reclabel = (values) => {
  for (const k in values) { const v = values[k]; if (typeof v === 'string' && v && !v.match(/^[0-9a-f-]{36}$/)) return v; }
  return (values.id || '').slice(0, 8) || '—';
};

// fieldSpan: how many of the 3 form columns a control should occupy. Big inputs
// (long text, json, attachments, multi-pickers) take the full row; a ref picker
// half; short scalars a third. Keeps dense forms readable instead of a 1-wide column.
function fieldSpan(f) {
  if (['text', 'json', 'array_file', 'array_ref'].includes(f.type)) return 3;
  if (['ref', 'tags', 'multiselect', 'url'].includes(f.type)) return 2;
  return 1;
}

// --- field rendering -------------------------------------------------------------

// RefInput: an async search picker for the target entity. It NEVER loads the
// whole table — it queries by typed term (limit 20), so it scales to millions of
// rows (users, config items). core.* targets fall back to a raw id input until
// the core pack exists.
function RefInput({ field, value, onChange }) {
  const [term, setTerm] = useState('');
  const [opts, setOpts] = useState(null);
  const [open, setOpen] = useState(false);
  const [current, setCurrent] = useState(null);
  useEffect(() => {
    if (!value) { setCurrent(null); return; }
    api(`/api/records/${field.ref}/${value}`).then(r => setCurrent(reclabel(r.values)))
      .catch(() => setCurrent(String(value).slice(0, 8)));
  }, [value, field.ref]);
  useEffect(() => {
    if (!open) return;
    const t = setTimeout(() => {
      api(`/api/query/${field.ref}`, { method: 'POST', body: JSON.stringify({ search: term, limit: 20 }) })
        .then(r => setOpts(r.records || [])).catch(() => setOpts([]));
    }, 200);
    return () => clearTimeout(t);
  }, [term, open, field.ref]);
  const pick = (id) => { onChange(id); setOpen(false); };
  return html`<div style="position:relative">
    <input placeholder=${current ? '' : 'search…'} value=${open ? term : (current || '')}
      onFocus=${() => { setOpen(true); setTerm(''); setOpts(null); }}
      onBlur=${() => setTimeout(() => setOpen(false), 160)}
      onInput=${e => setTerm(e.target.value)} />
    ${open && html`<div style="position:absolute;z-index:9;left:0;right:0;top:100%;background:var(--panel);border:1px solid var(--line);border-radius:6px;max-height:240px;overflow:auto;box-shadow:0 6px 20px #0008">
      ${value && html`<div style="padding:7px 10px;cursor:pointer;color:var(--dim)" onMouseDown=${() => pick('')}>— clear —</div>`}
      ${opts === null ? html`<div style="padding:7px 10px" class="muted">start typing…</div>`
        : opts.length === 0 ? html`<div style="padding:7px 10px" class="muted">nothing found</div>`
        : opts.map(r => html`<div style="padding:7px 10px;cursor:pointer;border-top:1px solid var(--line)"
            onMouseDown=${() => pick(r.id)} onMouseEnter=${e => e.target.style.background='#1c232c'} onMouseLeave=${e => e.target.style.background=''}>${reclabel(r.values)}</div>`)}
    </div>`}
  </div>`;
}

// FileInput: drag-drop or pick a document, upload it, hold the returned ref.
function FileInput({ value, onChange }) {
  const [busy, setBusy] = useState(false); const [err, setErr] = useState(null);
  const upload = async (file) => {
    if (!file) return;
    setBusy(true); setErr(null);
    try {
      const s = session.get();
      const form = new FormData(); form.append('file', file);
      const resp = await fetch('/api/files', { method: 'POST',
        headers: s?.token ? { Authorization: `Bearer ${s.token}` } : {}, body: form });
      const ref = await resp.json();
      if (!resp.ok) throw ref;
      onChange(ref);
    } catch (e) { setErr(e.message || 'upload failed'); }
    setBusy(false);
  };
  const inputId = 'fi-' + (value?.hash || 'new');
  return html`<div
      onDragOver=${e => e.preventDefault()}
      onDrop=${e => { e.preventDefault(); upload(e.dataTransfer.files[0]); }}
      onClick=${() => document.getElementById(inputId)?.click()}
      style="border:1px dashed var(--line);border-radius:6px;padding:12px;text-align:center;margin:3px 0 10px;cursor:pointer">
    ${busy ? html`<span class="muted">loading…</span>`
      : value?.name ? html`<span>📄 ${value.name} <span class="muted">(${Math.round((value.size || 0) / 1024)} KB)</span></span>`
      : html`<span class="muted">drop a file here or click to choose</span>`}
    <input id=${inputId} type="file" style="display:none" onChange=${e => upload(e.target.files[0])} />
    ${err && html`<div class="err">${err}</div>`}
  </div>`;
}

// TagsInput: free labels (array[string]) as add/remove chips.
function TagsInput({ value, onChange, options }) {
  const list = Array.isArray(value) ? value : [];
  const [draft, setDraft] = useState('');
  const add = (t) => { t = t.trim(); if (t && !list.includes(t)) onChange([...list, t]); setDraft(''); };
  return html`<div style="margin:3px 0 10px">
    <div>${list.map(t => html`<span class="pill" style="margin:0 6px 4px 0;display:inline-block">${t}
      <span style="cursor:pointer;color:var(--dim)" onClick=${() => onChange(list.filter(x => x !== t))}> ✕</span></span>`)}</div>
    ${options
      ? html`<select value="" onChange=${e => e.target.value && add(e.target.value)}>
          <option value="">+ add…</option>${options.filter(o => !list.includes(o)).map(o => html`<option value=${o}>${o}</option>`)}</select>`
      : html`<input style="margin:0" placeholder="add a tag, Enter" value=${draft}
          onInput=${e => setDraft(e.target.value)} onKeyDown=${e => e.key === 'Enter' && (e.preventDefault(), add(draft))} />`}
  </div>`;
}

function FieldInput({ field, value, onChange }) {
  if (field.type === 'file') return html`<${FileInput} value=${value} onChange=${onChange} />`;
  if (field.type === 'ref') return html`<${RefInput} field=${field} value=${value} onChange=${onChange} />`;
  if (field.type === 'tags') return html`<${TagsInput} value=${value} onChange=${onChange} />`;
  if (field.type === 'multiselect') return html`<${TagsInput} value=${value} onChange=${onChange} options=${field.values} />`;
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

// --- branding (white-label; fetched before auth) --------------------------------
const BRAND = { name: 'Kalita', accent: '', tagline: '' };
async function loadBrand() {
  try {
    const b = await (await fetch('/api/brand')).json();
    if (b?.name) Object.assign(BRAND, b);
  } catch { /* default */ }
  document.title = BRAND.name;
  if (BRAND.accent) document.documentElement.style.setProperty('--acc', BRAND.accent);
}

// --- views ----------------------------------------------------------------------

function Login() {
  const [token, setToken] = useState('');
  return html`<div class="login card">
    <h2>${BRAND.name}</h2>
    <div class="muted" style="margin-bottom:10px">${BRAND.tagline || html`Enter your access token.`}</div>
    <label>Access token</label><input type="password" value=${token} onInput=${e => setToken(e.target.value)} />
    <button class="btn green" onClick=${() => { if (token.trim()) { session.set({ token: token.trim() }); location.reload(); } }}>Sign in</button>
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
  const writable = ent.fields.filter(f => f.writable);
  const submit = async () => {
    setErr(null);
    try {
      await api(`/api/records/${ent.name}`, { method: 'POST', body: JSON.stringify({ values: vals, basis: basis() }) });
      onDone();
    } catch (e) { setErr(e); }
  };
  return html`<div class="card">
    <h3>New · ${elab(ent)}</h3>
    <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:2px 18px">
      ${writable.map(f => html`<div style=${'grid-column:span ' + fieldSpan(f)}>
        <label>${flab(f)}${f.required ? ' *' : ''}</label>
        <${FieldInput} field=${f} value=${vals[f.name]} onChange=${v => setVals({ ...vals, [f.name]: v })} />
      </div>`)}
    </div>
    ${err && html`<div class="err">${err.message} ${err.fix_hint ? `— ${err.fix_hint}` : ''}</div>`}
    <button class="btn green" onClick=${submit}>Create</button>
  </div>`;
}

// Search: the product face of KnowVault — ask a question over the documents,
// get an answer with sources. Backed by POST /api/search (node proxies a
// search worker; the node already enforced what this actor may see).
function SearchView() {
  const [q, setQ] = useState(''); const [busy, setBusy] = useState(false);
  const [res, setRes] = useState(null); const [err, setErr] = useState(null);
  const ask = async () => {
    if (!q.trim()) return;
    setBusy(true); setErr(null); setRes(null);
    try { setRes(await api('/api/search', { method: 'POST', body: JSON.stringify({ question: q }) })); }
    catch (e) { setErr(e); }
    setBusy(false);
  };
  return html`<div>
    <h2>Search documents</h2>
    <div style="display:flex;gap:8px;align-items:flex-start;max-width:720px">
      <input style="margin:0" placeholder="Ask anything about your documents…"
        value=${q} onInput=${e => setQ(e.target.value)}
        onKeyDown=${e => e.key === 'Enter' && ask()} />
      <button class="btn green" onClick=${ask} disabled=${busy}>${busy ? '…' : 'Ask'}</button>
    </div>
    ${err && html`<div class="err">${err.message || JSON.stringify(err)}</div>`}
    ${res && html`<div class="card" style="max-width:720px;margin-top:14px">
      <div style="white-space:pre-wrap;line-height:1.55">${res.answer}</div>
      ${res.sources?.length > 0 && html`<div class="muted" style="margin-top:12px">
        Sources: ${res.sources.map(s => html`<span class="pill" style="margin-right:6px">${s}</span>`)}</div>`}
    </div>`}
    ${busy && html`<div class="muted" style="margin-top:12px">searching documents and composing an answer…</div>`}
  </div>`;
}

// Agents screen: the actor directory — who acts on this node, which model
// stands behind each agent, and the revoke switch. Humans only (server-side).
function AgentsView() {
  const [actors, setActors] = useState(null); const [err, setErr] = useState(null);
  const load = () => api('/api/actors').then(r => setActors(r.actors || [])).catch(setErr);
  useEffect(load, []);
  const disable = async (id) => {
    setErr(null);
    try { await api(`/api/actors/${id}/disable`, { method: 'POST', body: '{}' }); load(); }
    catch (e) { setErr(e); }
  };
  if (err) return html`<div class="err">${err.message || 'humans only'}</div>`;
  if (!actors) return html`<div class="muted">loading…</div>`;
  return html`<div>
    <h2>Agents & users</h2>
    <div class="muted" style="margin-bottom:10px">Registered actors of this node. Revoking kills the token and signatures immediately.</div>
    <table><thead><tr><th>id</th><th>type</th><th>role</th><th>model</th><th>owner</th><th>status</th><th></th></tr></thead>
    <tbody>${actors.map(a => html`<tr style="cursor:default">
      <td><b>${a.id}</b></td>
      <td><span class="pill">${a.type}</span></td>
      <td>${a.role}</td>
      <td>${a.meta?.model || html`<span class="muted">—</span>`}</td>
      <td>${a.meta?.owner || html`<span class="muted">—</span>`}</td>
      <td>${a.disabled ? html`<span class="pill" style="background:#33201c">revoked</span>` : html`<span class="pill" style="background:#15301f">active</span>`}</td>
      <td>${!a.disabled && html`<button class="btn red" onClick=${() => disable(a.id)}>Revoke</button>`}</td>
    </tr>`)}</tbody></table>
    <div class="muted" style="margin-top:8px">New actors: <code>kalita user|agent add --id … --role … [--model …]</code> on the node.</div>
  </div>`;
}

// Singletons (settings-style entities) skip the list: straight to the one
// record, or its creation form.
function SingletonView({ ent, refresh }) {
  const [rows, setRows] = useState(null);
  useEffect(() => { api(`/api/records/${ent.name}`).then(r => setRows(r.records || [])); }, [ent.name]);
  if (rows === null) return html`<div class="muted">loading…</div>`;
  if (rows.length === 0) return html`<div><h2>${ent.name}</h2>
    ${ent.can_create ? html`<${CreateForm} ent=${ent} onDone=${() => location.reload()} />`
      : html`<div class="muted">not configured yet — ask a role that can create it</div>`}</div>`;
  return html`<${RecordView} ent=${ent} id=${rows[0].id} refresh=${refresh} />`;
}

const PAGE = 25;

function EntityList({ ent }) {
  const [rows, setRows] = useState([]); const [creating, setCreating] = useState(false);
  const [page, setPage] = useState(0); const [q, setQ] = useState('');
  const cols = ent.ui.list_columns?.length ? ent.ui.list_columns : ent.fields.filter(f => f.readable).slice(0, 6).map(f => f.name);
  const load = () => api(`/api/records/${ent.name}?limit=${PAGE + 1}&offset=${page * PAGE}`).then(r => setRows(r.records || []));
  useEffect(() => { load(); setCreating(false); }, [ent.name, page]);
  const hasNext = rows.length > PAGE;
  const visible = rows.slice(0, PAGE).filter(r => !q ||
    cols.some(c => String(r.values[c] ?? '').toLowerCase().includes(q.toLowerCase())));
  return html`<div>
    <h2>${elab(ent)} ${ent.ui.board_by && html`<a class="muted" style="font-size:13px" onClick=${() => nav(`/board/${ent.name}`)}>board</a>`}</h2>
    <div style="display:flex;gap:8px;align-items:flex-start">
      ${ent.can_create && html`<button class="btn" onClick=${() => setCreating(!creating)}>${creating ? 'Cancel' : `+ ${elab(ent)}`}</button>`}
      <input style="max-width:240px;margin:0" placeholder="filter this page…" value=${q} onInput=${e => setQ(e.target.value)} />
    </div>
    ${creating && html`<${CreateForm} ent=${ent} onDone=${() => { setCreating(false); load(); }} />`}
    <table style="margin-top:10px"><thead><tr>${cols.map(c => html`<th>${colLabel(ent, c)}</th>`)}</tr></thead>
    <tbody>${visible.map(r => html`<tr onClick=${() => nav(`/e/${ent.name}/${r.id}`)}>
      ${cols.map(c => html`<td>${fmt(r.values[c])}</td>`)}</tr>`)}</tbody></table>
    ${visible.length === 0 && html`<div class="muted" style="margin-top:8px">no records visible to your role</div>`}
    <div style="margin-top:10px">
      ${page > 0 && html`<button class="btn" onClick=${() => setPage(page - 1)}>← prev</button>`}
      ${(page > 0 || hasNext) && html`<span class="muted" style="margin:0 8px">page ${page + 1}</span>`}
      ${hasNext && html`<button class="btn" onClick=${() => setPage(page + 1)}>next →</button>`}
    </div>
  </div>`;
}

function Board({ ent }) {
  const [rows, setRows] = useState([]);
  useEffect(() => { api(`/api/records/${ent.name}`).then(r => setRows(r.records || [])); }, [ent.name]);
  const field = ent.fields.find(f => f.name === ent.ui.board_by);
  const title = ent.ui.list_columns?.[0] || ent.fields[0]?.name;
  return html`<div>
    <h2>${elab(ent)} <a class="muted" style="font-size:13px" onClick=${() => nav(`/e/${ent.name}`)}>list</a></h2>
    <div class="cols">${(field?.values || []).map(v => html`<div class="col"><h4>${v} · ${rows.filter(r => r.values[ent.ui.board_by] === v).length}</h4>
      ${rows.filter(r => r.values[ent.ui.board_by] === v).map(r => html`
        <div class="kcard" onClick=${() => nav(`/e/${ent.name}/${r.id}`)}>${fmt(r.values[title])}</div>`)}
    </div>`)}</div>
  </div>`;
}

// RefValue: render a ref field in READ mode as the target's label, not its uuid.
function RefValue({ field, value }) {
  const [text, setText] = useState(null);
  useEffect(() => {
    if (!value) { setText(null); return; }
    api(`/api/records/${field.ref}/${value}`).then(r => setText(reclabel(r.values)))
      .catch(() => setText(String(value).slice(0, 8)));
  }, [value, field.ref]);
  return html`${text || html`<span class="muted">—</span>`}`;
}

function RecordView({ ent, id, refresh }) {
  const [rec, setRec] = useState(null); const [journal, setJournal] = useState(null);
  const [edit, setEdit] = useState({}); const [editing, setEditing] = useState(false);
  const [err, setErr] = useState(null); const [note, setNote] = useState(null);
  const load = () => api(`/api/records/${ent.name}/${id}`).then(setRec).catch(setErr);
  useEffect(() => { load(); setJournal(null); setEdit({}); setEditing(false); }, [ent.name, id]);

  if (err && !rec) return html`<div class="err">${err.message}</div>`;
  if (!rec) return html`<div class="muted">loading…</div>`;
  const state = rec.values[ent.workflow_field];
  const actions = (ent.actions || []).filter(a => a.can_act && (a.from === state || a.from === 'any'));

  const act = async (action) => {
    setErr(null); setNote(null);
    try {
      const res = await api(`/api/records/${ent.name}/${id}/act`, { method: 'POST', body: JSON.stringify({ action, basis: basis() }) });
      if (res.status === 'pending_approval') setNote('Action sent for signature — see the approvals inbox.');
      load(); refresh();
    } catch (e) { setErr(e); }
  };
  const save = async () => {
    setErr(null);
    try { await api(`/api/records/${ent.name}/${id}`, { method: 'PATCH', body: JSON.stringify({ values: edit, basis: basis() }) }); setEdit({}); setEditing(false); load(); }
    catch (e) { setErr(e); }
  };
  const cancel = () => { setEdit({}); setEditing(false); setErr(null); };
  const showJournal = () => api(`/api/records/${ent.name}/${id}/journal`).then(j => setJournal(j.events));

  return html`<div>
    <h2>${elab(ent)} <span class="muted">${reclabel(rec.values)}</span> ${state && html`<span class="pill">${state}</span>`}</h2>
    <div style="margin-bottom:12px;display:flex;gap:8px;flex-wrap:wrap">
      ${!editing && actions.map(a => html`<button class="btn ${a.requires_approval ? '' : 'green'}" onClick=${() => act(a.action)}>${a.label || humanize(a.action)}${a.requires_approval ? ' ✍' : ''}</button>`)}
      ${!editing && ent.can_update && html`<button class="btn" onClick=${() => setEditing(true)}>✎ Edit</button>`}
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
      <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:2px 18px">
      ${ent.fields.filter(f => f.readable).map(f => {
        const editable = editing && f.writable && f.name !== ent.workflow_field;
        const val = f.name in edit ? edit[f.name] : rec.values[f.name];
        return html`<div style=${'grid-column:span ' + fieldSpan(f)}>
          <label>${flab(f)}${f.computed ? ' (computed)' : ''}</label>
          ${editable
            ? html`<${FieldInput} field=${f} value=${val} onChange=${v => setEdit({ ...edit, [f.name]: v })} />`
            : html`<div style="padding:4px 0 10px;min-height:20px">${
                f.type === 'ref' ? html`<${RefValue} field=${f} value=${rec.values[f.name]} />`
                  : (fmt(rec.values[f.name]) || html`<span class="muted">—</span>`)}</div>`}
        </div>`;
      })}
      </div>
      ${editing && html`<div style="margin-top:10px">
        <button class="btn green" onClick=${save}>Save</button>
        <button class="btn" onClick=${cancel}>Cancel</button>
      </div>`}
    </div>`}
    <${Thread} ent=${ent} id=${id} />
  </div>`;
}

// Thread: the comment conversation on a record. Staff (anyone who can update
// the record) may post internal notes the customer never sees.
function Thread({ ent, id }) {
  const [items, setItems] = useState(null);
  const [body, setBody] = useState(''); const [internal, setInternal] = useState(false);
  const [err, setErr] = useState(null);
  const load = () => api(`/api/records/${ent.name}/${id}/comments`).then(r => setItems(r.comments || [])).catch(() => setItems([]));
  useEffect(load, [ent.name, id]);
  const post = async () => {
    if (!body.trim()) return;
    setErr(null);
    try {
      await api(`/api/records/${ent.name}/${id}/comments`, { method: 'POST',
        body: JSON.stringify({ body, internal, basis: basis() }) });
      setBody(''); setInternal(false); load();
    } catch (e) { setErr(e); }
  };
  const canInternal = ent.can_update;
  return html`<div class="card" style="margin-top:12px">
    <h3 style="margin-top:0">Discussion</h3>
    ${(items || []).map(c => html`<div style="padding:6px 0;border-bottom:1px solid var(--line)">
      <b>${c.author.id}</b> ${c.internal && html`<span class="pill" style="background:#33201c">internal</span>`}
      <span class="muted" style="font-size:11px"> · ${(c.ts || '').slice(0, 16).replace('T', ' ')}</span>
      <div style="white-space:pre-wrap">${c.body}</div>
    </div>`)}
    ${(items && items.length === 0) && html`<div class="muted">no messages yet</div>`}
    ${err && html`<div class="err">${err.message}</div>`}
    <div style="margin-top:8px">
      <textarea rows="2" placeholder="write a message…" value=${body} onInput=${e => setBody(e.target.value)} />
      <div style="display:flex;align-items:center;gap:10px">
        <button class="btn green" onClick=${post}>Send</button>
        ${canInternal && html`<label class="muted" style="cursor:pointer">
          <input type="checkbox" style="width:auto;margin-right:5px" checked=${internal}
            onChange=${e => setInternal(e.target.checked)} /> internal note</label>`}
      </div>
    </div>
  </div>`;
}

// Dashboards: table-wide aggregate tiles (count/sum/avg/min/max), some grouped.
// Each tile already respects the viewer's row permissions server-side.
function Tile({ t }) {
  const grouped = t.groups && t.groups.length;
  const max = grouped ? Math.max(...t.groups.map(g => g.value), 1) : 0;
  return html`<div class="card" style=${grouped ? 'flex:1 1 320px' : 'flex:0 0 200px'}>
    <div class="muted" style="font-size:12px;margin-bottom:6px">${t.label}</div>
    ${grouped
      ? html`<div>${t.groups.map(g => html`<div style="display:flex;align-items:center;gap:8px;margin:4px 0">
          <span style="width:96px;font-size:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${g.key}</span>
          <span style="flex:1;background:#1f2630;border-radius:4px;height:14px;position:relative">
            <span style="position:absolute;inset:0 auto 0 0;width:${Math.round(g.value / max * 100)}%;background:var(--acc);border-radius:4px"></span></span>
          <b style="width:32px;text-align:right">${g.value}</b></div>`)}</div>`
      : html`<div style="font-size:30px;font-weight:600">${t.value}</div>`}
  </div>`;
}

function Dashboards() {
  const [list, setList] = useState(null);
  const [active, setActive] = useState(null);
  const [data, setData] = useState(null);
  const [err, setErr] = useState(null);
  useEffect(() => { api('/api/dashboards').then(r => {
    const ds = r.dashboards || []; setList(ds);
    if (ds.length) setActive(ds[0].name);
  }).catch(setErr); }, []);
  useEffect(() => { if (active) { setData(null); api(`/api/dashboards/${active}`).then(setData).catch(setErr); } }, [active]);
  if (err) return html`<div class="err">${err.message}</div>`;
  if (!list) return html`<div class="muted">loading…</div>`;
  if (!list.length) return html`<div class="muted">no dashboards in this pack</div>`;
  return html`<div>
    <h2>Dashboards</h2>
    <div class="tabs" style="margin-bottom:16px">
      ${list.map(d => html`<a class=${active === d.name ? 'on' : ''} onClick=${() => setActive(d.name)}>${d.title || d.name}</a>`)}
    </div>
    ${!data ? html`<div class="muted">loading…</div>`
      : html`<div style="display:flex;flex-wrap:wrap;gap:12px;align-items:flex-start">
          ${data.tiles.map(t => html`<${Tile} t=${t} />`)}</div>`}
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
  if (parts[0] === 'search') view = html`<${SearchView} />`;
  else if (parts[0] === 'dashboards') view = html`<${Dashboards} />`;
  else if (parts[0] === 'agents') view = html`<${AgentsView} />`;
  else if (parts[0] === 'e' && ent && parts[2]) view = html`<${RecordView} ent=${ent} id=${parts[2]} refresh=${refresh} />`;
  else if (parts[0] === 'e' && ent && ent.singleton) view = html`<${SingletonView} ent=${ent} refresh=${refresh} />`;
  else if (parts[0] === 'e' && ent) view = html`<${EntityList} ent=${ent} />`;
  else if (parts[0] === 'board' && ent) view = html`<${Board} ent=${ent} />`;

  return html`<div class="shell">
    <div class="side">
      <h1>${BRAND.name}</h1>
      <div class="who">${meta.pack || '(genesis)'} · v${meta.def_version}<br/>${meta.actor_id} — ${meta.role}
        <a style="display:block" onClick=${() => { session.clear(); location.reload(); }}>sign out</a></div>
      <div class="nav">
        ${meta.search && html`<a class=${route === '/search' ? 'on' : ''} onClick=${() => nav('/search')}>🔍 Search</a>`}
        <a class=${route === '/inbox' ? 'on' : ''} onClick=${() => nav('/inbox')}>Inbox ${inboxCount > 0 && html`<span class="badge">${inboxCount}</span>`}</a>
        <a class=${route === '/dashboards' ? 'on' : ''} onClick=${() => nav('/dashboards')}>📊 Dashboards</a>
        ${meta.entities.map(e => html`<a class=${parts[1] === e.name ? 'on' : ''} onClick=${() => nav(`/e/${e.name}`)}>${elab(e)}</a>`)}
        <a class=${route === '/agents' ? 'on' : ''} onClick=${() => nav('/agents')} style="margin-top:10px;border-top:1px solid var(--line);padding-top:10px">Agents</a>
      </div>
    </div>
    <div class="main">${view}</div>
  </div>`;
}

loadBrand().then(() =>
  render(session.get() ? html`<${App} />` : html`<${Login} />`, document.getElementById('root')));
