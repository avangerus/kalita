// Kalita universal client: a pure projection of /api/meta. No business logic
// lives here — what this actor can see and press comes from the server.
import { h, render, html, useState, useEffect } from '/vendor/preact-standalone.module.js';
// kalita-ui (LLD-05 PK-1): the atoms/molecules FieldInput/FieldValue and
// friends dispatch to, so the type-rendering switch has ONE source of truth
// shared with web/catalog.html and (via tokens/format.js) @kalita/sdk.
// Relative path, not "/packages/..." — see packages/kalita-ui/atoms.js's
// file header and cmd/kalita/main.go for why this resolves under every
// serving mode (disk, --ui-dir, embedded single-binary).
import { FieldAtom, RefLink, MultiSelect } from '../packages/kalita-ui/atoms.js';
import { ApprovalCard, TimelineItem, DashboardTile, isMutation } from '../packages/kalita-ui/molecules.js';
import { reclabel, humanize, fieldSpan, fmtPlain } from '../packages/kalita-ui/tokens/format.js';

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
// humanize/reclabel/fieldSpan: shared with kalita-ui and @kalita/sdk, see
// packages/kalita-ui/tokens/format.js — imported above, not redefined here.

// refFetcher/gotoRef: the network + navigation capabilities RefLink (kalita-ui)
// needs injected — RefLink itself never imports app.js's authenticated `api()`
// or hash-router `nav()` (see atoms.js's file header on capability injection).
const refFetcher = (entity, id) => api(`/api/records/${entity}/${id}`);
const gotoRef = (entity, id) => nav(`/e/${entity}/${id}`);

// uploadFile: the network capability FieldAtom's file/array_file atoms need
// injected as `uploader` — a plain multipart POST to /api/files with this
// session's bearer token.
async function uploadFile(file) {
  const s = session.get();
  const form = new FormData(); form.append('file', file);
  const resp = await fetch('/api/files', { method: 'POST',
    headers: s?.token ? { Authorization: `Bearer ${s.token}` } : {}, body: form });
  const ref = await resp.json();
  if (!resp.ok) throw ref;
  return ref;
}

// --- field rendering -------------------------------------------------------------

// LookupModal: the "grown-up" reference picker — a modal lookup (ServiceNow's
// reference lookup, Salesforce's lookup dialog) for when the inline dropdown
// isn't enough: search + a multi-column results table for context + pagination,
// single-pick or multi-pick. Columns are inferred from the result rows.
const LK_PAGE = 12;
function LookupModal({ entity, multi, value, onPick, onClose }) {
  const [term, setTerm] = useState('');
  const [rows, setRows] = useState(null);
  const [page, setPage] = useState(0);
  const [sel, setSel] = useState(() => multi ? (Array.isArray(value) ? [...value] : []) : null);
  useEffect(() => {
    const t = setTimeout(() => {
      api(`/api/query/${entity}`, { method: 'POST', body: JSON.stringify({ search: term, limit: LK_PAGE + 1, offset: page * LK_PAGE }) })
        .then(r => setRows(r.records || [])).catch(() => setRows([]));
    }, 200);
    return () => clearTimeout(t);
  }, [term, page, entity]);
  const visible = (rows || []).slice(0, LK_PAGE);
  const hasNext = rows && rows.length > LK_PAGE;
  // columns: the first few primitive (non-uuid) value fields seen across results
  const cols = [];
  for (const r of visible) for (const k in r.values) {
    const v = r.values[k];
    if (cols.length < 4 && !cols.includes(k) && (typeof v === 'number' ||
        (typeof v === 'string' && v && !v.match(/^[0-9a-f-]{36}$/) && v.length < 60))) cols.push(k);
  }
  const toggle = (id) => setSel(s => s.includes(id) ? s.filter(x => x !== id) : [...s, id]);
  return html`<div class="modal-bg" onMouseDown=${onClose} onKeyDown=${e => e.key === 'Escape' && onClose()}>
    <div class="modal" onMouseDown=${e => e.stopPropagation()}>
      <div class="modal-head">
        <b>Select ${entity}</b>
        <input autofocus placeholder="search…" value=${term} onInput=${e => { setTerm(e.target.value); setPage(0); }} style="margin:8px 0 0" />
      </div>
      <div class="modal-body">
        ${rows === null ? html`<div class="muted" style="padding:14px">loading…</div>`
          : visible.length === 0 ? html`<div class="muted" style="padding:14px">nothing found</div>`
          : html`<table><thead><tr>${multi ? html`<th style="width:28px"></th>` : ''}${cols.map(c => html`<th>${c}</th>`)}</tr></thead>
            <tbody>${visible.map(r => html`<tr onClick=${() => multi ? toggle(r.id) : (onPick(r.id), onClose())}>
              ${multi ? html`<td><input type="checkbox" style="width:auto;margin:0" checked=${sel.includes(r.id)}
                  onClick=${e => e.stopPropagation()} onChange=${() => toggle(r.id)} /></td>` : ''}
              ${cols.map(c => html`<td>${fmtPlain(r.values[c])}</td>`)}</tr>`)}</tbody></table>`}
      </div>
      <div class="modal-foot">
        ${page > 0 ? html`<button class="btn" onClick=${() => setPage(page - 1)}>← prev</button>` : ''}
        ${hasNext ? html`<button class="btn" onClick=${() => setPage(page + 1)}>next →</button>` : ''}
        <span style="flex:1"></span>
        ${multi ? html`<button class="btn green" onClick=${() => { onPick(sel); onClose(); }}>Add ${sel.length} selected</button>` : ''}
        <button class="btn" onClick=${onClose}>Cancel</button>
      </div>
    </div>
  </div>`;
}

// RefInput: an async search picker for the target entity. It NEVER loads the
// whole table — it queries by typed term (limit 20), so it scales to millions of
// rows (users, config items). The 🔍 button opens the full LookupModal when you
// need to see context before choosing. core.* targets resolve by id.
function RefInput({ field, value, onChange, noBrowse }) {
  const [term, setTerm] = useState('');
  const [opts, setOpts] = useState(null);
  const [open, setOpen] = useState(false);
  const [current, setCurrent] = useState(null);
  const [hi, setHi] = useState(0); // keyboard-highlighted row
  useEffect(() => {
    if (!value) { setCurrent(null); return; }
    api(`/api/records/${field.ref}/${value}`).then(r => setCurrent(reclabel(r.values)))
      .catch(() => setCurrent(String(value).slice(0, 8)));
  }, [value, field.ref]);
  useEffect(() => {
    if (!open) return;
    const t = setTimeout(() => {
      api(`/api/query/${field.ref}`, { method: 'POST', body: JSON.stringify({ search: term, limit: 20 }) })
        .then(r => { setOpts(r.records || []); setHi(0); }).catch(() => setOpts([]));
    }, 200);
    return () => clearTimeout(t);
  }, [term, open, field.ref]);
  const [lk, setLk] = useState(false); // the full lookup modal
  const pick = (id) => { onChange(id); setOpen(false); };
  // keyboard: ↓/↑ move, Enter selects, Esc closes — a picker you never touch the mouse for
  const onKey = (e) => {
    if (!open) { if (e.key === 'ArrowDown') { setOpen(true); setTerm(''); } return; }
    const n = opts ? opts.length : 0;
    if (e.key === 'ArrowDown') { e.preventDefault(); setHi(h => Math.min(h + 1, n - 1)); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setHi(h => Math.max(h - 1, 0)); }
    else if (e.key === 'Enter') { if (n && opts[hi]) { e.preventDefault(); pick(opts[hi].id); } }
    else if (e.key === 'Escape') { setOpen(false); }
  };
  return html`<div style="position:relative">
    <div style="display:flex;gap:6px;align-items:flex-start">
      <div style="flex:1;position:relative">
        <input placeholder=${current ? '' : 'search…'} value=${open ? term : (current || '')}
          onFocus=${() => { setOpen(true); setTerm(''); setOpts(null); }}
          onBlur=${() => setTimeout(() => setOpen(false), 160)}
          onKeyDown=${onKey}
          onInput=${e => setTerm(e.target.value)} />
        ${open && html`<div style="position:absolute;z-index:9;left:0;right:0;top:100%;background:var(--panel);border:1px solid var(--line);border-radius:6px;max-height:240px;overflow:auto;box-shadow:0 6px 20px #0008">
          ${value && html`<div style="padding:7px 10px;cursor:pointer;color:var(--dim)" onMouseDown=${() => pick('')}>— clear —</div>`}
          ${opts === null ? html`<div style="padding:7px 10px" class="muted">start typing…</div>`
            : opts.length === 0 ? html`<div style="padding:7px 10px" class="muted">nothing found</div>`
            : opts.map((r, i) => html`<div style=${'padding:7px 10px;cursor:pointer;border-top:1px solid var(--line)' + (i === hi ? ';background:#1c232c' : '')}
                onMouseDown=${() => pick(r.id)} onMouseEnter=${() => setHi(i)}>${reclabel(r.values)}</div>`)}
        </div>`}
      </div>
      ${!noBrowse && html`<button type="button" class="btn" style="margin:3px 0 10px;padding:7px 10px" title="browse with search…" onClick=${() => setLk(true)}>🔍</button>`}
    </div>
    ${lk && html`<${LookupModal} entity=${field.ref} onPick=${id => pick(id)} onClose=${() => setLk(false)} />`}
  </div>`;
}

// CapabilityPicker: a field whose value comes from a connector capability
// (meta gives field.picker = "<connector>.<fn>"). It calls
// GET /api/call/<picker>?<args> and renders by the SHAPE the worker returns:
//   tree → a folder/hierarchy browser (breadcrumbs + drill-in + pick a node)
//   list → a flat async picker (the RefInput pattern over a capability source)
// It is generic: no knowvault/files/Source knowledge — connector, capability
// and shape all arrive from the response, exactly like RefInput drives off meta.
function CapabilityPicker({ field, value, onChange }) {
  const [open, setOpen] = useState(false);
  const [cur, setCur] = useState('');     // node being browsed (tree shape)
  const [data, setData] = useState(null); // last capability response
  const [term, setTerm] = useState('');   // filter (list shape)
  const [err, setErr] = useState(null);
  const load = (path) => {
    setErr(null);
    api(`/api/call/${field.picker}?path=${encodeURIComponent(path || '')}`)
      .then(r => { setData(r); setCur(r.path != null ? r.path : (path || '')); })
      .catch(e => { setErr(e.message || 'capability unavailable'); setData({ items: [] }); });
  };
  useEffect(() => { if (open && data === null) load(''); }, [open]);
  const items = (data && data.items) || [];
  const filesOf = (it) => (it.meta && it.meta.files != null) ? it.meta.files : it.files;
  const crumbs = cur ? cur.split('/') : [];
  const segPath = (i) => crumbs.slice(0, i + 1).join('/');
  const shape = data && data.shape;
  return html`<div style="margin:3px 0 10px">
    <div style="display:flex;gap:6px;align-items:center">
      <input readOnly value=${value || ''} placeholder="nothing chosen" style="flex:1;margin:0" />
      <button type="button" class="btn" style="margin:0;padding:7px 10px"
        onClick=${() => setOpen(o => !o)}>${open ? 'Close' : 'Browse…'}</button>
    </div>
    ${open && html`<div style="margin-top:6px;border:1px solid var(--line);border-radius:6px;background:var(--panel);padding:8px">
      ${err && html`<div class="err">${err}</div>`}
      ${shape === 'list' ? html`
        <input autofocus placeholder="search…" value=${term} onInput=${e => setTerm(e.target.value)}
          style="margin:0 0 6px" />
        <div style="max-height:260px;overflow:auto">
          ${items.filter(it => !term || (it.label || '').toLowerCase().includes(term.toLowerCase()))
            .map(it => html`<div style="padding:6px 8px;cursor:pointer;border-top:1px solid var(--line)"
              onClick=${() => { onChange(it.id); setOpen(false); }}>${it.label}</div>`)}
        </div>`
      : html`
        <div style="display:flex;flex-wrap:wrap;gap:4px;align-items:center;margin-bottom:6px;font-size:13px">
          <span style="cursor:pointer;color:var(--accent,#4da3ff)" onClick=${() => load('')}>root</span>
          ${crumbs.map((seg, i) => html`<span>/</span><span style="cursor:pointer;color:var(--accent,#4da3ff)"
            onClick=${() => load(segPath(i))}>${seg}</span>`)}
        </div>
        <div style="max-height:260px;overflow:auto">
          ${data === null ? html`<div class="muted" style="padding:6px 8px">loading…</div>`
            : items.length === 0 ? html`<div class="muted" style="padding:6px 8px">no subfolders here</div>`
            : items.map(it => html`<div style="display:flex;align-items:center;gap:8px;padding:6px 8px;border-top:1px solid var(--line)">
                <span style="flex:1;cursor:${it.has_children ? 'pointer' : 'default'}"
                  onClick=${() => it.has_children && load(it.id)}>📁 ${it.label}
                  ${filesOf(it) != null && html`<span class="muted"> (${filesOf(it)} files)</span>`}
                  ${it.has_children && html`<span class="muted"> ›</span>`}</span>
                <button type="button" class="btn" style="margin:0;padding:4px 8px"
                  onClick=${() => { onChange(it.id); setOpen(false); }}>Choose</button>
              </div>`)}
        </div>
        <div style="margin-top:8px;text-align:right">
          <button type="button" class="btn" style="margin:0;padding:6px 10px"
            onClick=${() => { onChange(cur); setOpen(false); }}>Choose this folder</button>
        </div>`}
    </div>`}
  </div>`;
}

// ArrayRefInput: many refs — pick with the same async picker, remove as chips.
// The chip label comes from kalita-ui's RefLink (id -> target record label),
// not a local RefValue — see the "removed" comment near FieldValue below.
function ArrayRefInput({ field, value, onChange }) {
  const list = Array.isArray(value) ? value : [];
  const [lk, setLk] = useState(false);
  const add = (id) => { if (id && !list.includes(id)) onChange([...list, id]); };
  return html`<div style="margin:3px 0 10px">
    <div>${list.map(id => html`<span class="pill" style="margin:0 6px 4px 0;display:inline-block">
      <${RefLink} field=${field} value=${id} fetcher=${refFetcher} />
      <span style="cursor:pointer;color:var(--dim)" onClick=${() => onChange(list.filter(x => x !== id))}> ✕</span></span>`)}</div>
    <div style="display:flex;gap:6px;align-items:flex-start">
      <div style="flex:1"><${RefInput} field=${field} value="" onChange=${add} noBrowse=${true} /></div>
      <button type="button" class="btn" style="margin:3px 0 10px;padding:7px 10px" title="browse and pick several…" onClick=${() => setLk(true)}>🔍</button>
    </div>
    ${lk && html`<${LookupModal} entity=${field.ref} multi value=${list}
      onPick=${ids => onChange([...new Set([...list, ...ids])])} onClose=${() => setLk(false)} />`}
  </div>`;
}

// TagsInput: the free-tags case of kalita-ui's MultiSelect atom, plus the one
// bit of network business the atom deliberately does NOT own (see atoms.js's
// file header) — fetching values already used on other records of this
// entity, so a user picks an existing label instead of typing a near-dupe.
// multiselect (closed enum list, field.values) needs no such fetch and calls
// FieldAtom directly from FieldInput below.
function TagsInput({ field, entity, value, onChange }) {
  const [existing, setExisting] = useState(null);
  useEffect(() => {
    api(`/api/query/${entity}`, { method: 'POST', body: JSON.stringify({ limit: 100 }) })
      .then(r => { const set = new Set(); (r.records || []).forEach(rec => (rec.values[field.name] || []).forEach(t => set.add(t))); setExisting([...set]); })
      .catch(() => setExisting([]));
  }, [entity, field.name]);
  return html`<${MultiSelect} field=${field} value=${value} onChange=${onChange} suggestions=${existing || []} />`;
}

// FieldInput: the EDIT-mode dispatcher (meta field -> the right control).
// Three cases stay app.js-local because they are stateful search/browse UI
// with no atom equivalent (see FieldAtom's doc comment in atoms.js); every
// other type delegates straight to kalita-ui's FieldAtom — the type-render
// switch itself lives ONCE, there, not here.
function FieldInput({ field, value, onChange, entity }) {
  if (field.picker) return html`<${CapabilityPicker} field=${field} value=${value} onChange=${onChange} />`;
  if (field.type === 'ref') return html`<${RefInput} field=${field} value=${value} onChange=${onChange} />`;
  if (field.type === 'array_ref') return html`<${ArrayRefInput} field=${field} value=${value} onChange=${onChange} />`;
  if (field.type === 'tags') return html`<${TagsInput} field=${field} entity=${entity} value=${value} onChange=${onChange} />`;
  return html`<${FieldAtom} field=${field} value=${value} onChange=${onChange} uploader=${uploadFile} />`;
}

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
  const [mode, setMode] = useState('token'); // 'token' | 'invite'
  const [token, setToken] = useState('');
  const [invite, setInvite] = useState(''); const [id, setId] = useState('');
  const [err, setErr] = useState(null);
  const signInToken = () => { if (token.trim()) { session.set({ token: token.trim() }); location.reload(); } };
  const redeem = async () => {
    setErr(null);
    try {
      const r = await api('/api/register', { method: 'POST', body: JSON.stringify({ invite: invite.trim(), id: id.trim() }) });
      if (r.token) { session.set({ token: r.token }); location.reload(); }
    } catch (e) { setErr(e); }
  };
  return html`<div class="login card">
    <h2>${BRAND.name}</h2>
    <div class="muted" style="margin-bottom:10px">${BRAND.tagline || ''}</div>
    <div class="tabs" style="margin-bottom:12px">
      <a class=${mode === 'token' ? 'on' : ''} onClick=${() => setMode('token')}>Access token</a>
      <a class=${mode === 'invite' ? 'on' : ''} onClick=${() => setMode('invite')}>I have an invite</a>
    </div>
    ${err && html`<div class="err">${err.message || 'invalid invite'}</div>`}
    ${mode === 'token' ? html`
      <label>Access token</label>
      <input type="password" value=${token} onInput=${e => setToken(e.target.value)} onKeyDown=${e => e.key === 'Enter' && signInToken()} />
      <button class="btn green" onClick=${signInToken}>Sign in</button>`
    : html`
      <div class="muted" style="margin-bottom:8px">Redeem the one-time code your provider sent you.</div>
      <label>Invite code</label><input value=${invite} onInput=${e => setInvite(e.target.value)} />
      <label>Your email</label><input value=${id} onInput=${e => setId(e.target.value)} onKeyDown=${e => e.key === 'Enter' && redeem()} />
      <button class="btn green" onClick=${redeem}>Enter</button>`}
  </div>`;
}

// ApprovalCard is kalita-ui's molecule (imported above) — the "what am I
// signing?" expand-to-preview logic lives there now, generalized off this
// former app.js-local component (fetch/nav/related-panel injected below).

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
    ${data.approvals.map(a => html`<${ApprovalCard} a=${a}
      ent=${meta.entities.find(e => e.name === a.entity) || { name: a.entity, fields: [] }}
      onDecide=${decide} onOpenRecord=${gotoRef} fetchRecord=${refFetcher}
      renderRelated=${({ ent, id }) => html`<${RelatedPanel} ent=${ent} id=${id} meta=${meta} readOnly=${true} />`} />`)}
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

function CreateForm({ ent, onDone, initial, hide }) {
  const [vals, setVals] = useState(initial || {}); const [err, setErr] = useState(null);
  const writable = ent.fields.filter(f => f.writable && !(hide || []).includes(f.name));
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
        <${FieldInput} field=${f} entity=${ent.name} value=${vals[f.name]} onChange=${v => setVals({ ...vals, [f.name]: v })} />
      </div>`)}
    </div>
    ${err && html`<div class="err">${err.message} ${err.fix_hint ? `— ${err.fix_hint}` : ''}</div>`}
    <button class="btn green" onClick=${submit}>Create</button>
  </div>`;
}

// highlightTerms: split `text` on the words of `query` (>=2 chars) and wrap
// the matches in <mark> — used by the Search hero's source cards (ENH-04b) to
// show WHY a chunk matched, not just that it did. Builds an array of
// strings/vnodes (never raw HTML), so untrusted snippet text is never parsed
// as markup — safe by construction, not by escaping.
function highlightTerms(text, query) {
  if (!text) return text;
  const terms = (query || '').split(/\s+/).map(t => t.trim()).filter(t => t.length > 1);
  if (!terms.length) return text;
  const esc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const re = new RegExp('(' + terms.map(esc).join('|') + ')', 'ig');
  const termsLower = terms.map(t => t.toLowerCase());
  return text.split(re).map((part, i) => termsLower.includes(part.toLowerCase())
    ? html`<mark key=${i}>${part}</mark>` : part);
}

// Search: the product face of KnowVault — ask a question over the documents,
// get an answer with sources. Backed by POST /api/search (node proxies a
// search worker; the node already enforced what this actor may see).
//
// ENH-04b: the worker now returns `source_hits=[{file, anchor, snippet}]`
// alongside the older `sources=[name,...]` (see workers/knowvault_search's
// service.py — ragproxy.go forwards the whole body, no core change needed).
// source_hits renders as clickable cards; clicking one expands a panel with
// the retrieved chunk's snippet, matched terms highlighted. If an older
// worker/response has no source_hits, this degrades to the original
// name-chip rendering instead of showing nothing.
function SearchView() {
  const [q, setQ] = useState(''); const [busy, setBusy] = useState(false);
  const [res, setRes] = useState(null); const [err, setErr] = useState(null);
  const [projects, setProjects] = useState([]); const [project, setProject] = useState('');
  const [openHit, setOpenHit] = useState(null); // index of the expanded source card, or null
  const [askedQ, setAskedQ] = useState(''); // the question actually answered (for highlight)
  // the projects (Workspaces) to scope a search to one of
  // BUG-06: after loading workspaces, default to the last (most recent) one so
  // the user searches a real scope, not the broad "All" catch-all.  "All" is
  // still reachable as an explicit choice in the dropdown.
  useEffect(() => { api('/api/records/Workspace?limit=200').then(r => { const ws = r.records || []; setProjects(ws); if (ws.length > 0) setProject(ws[ws.length - 1].id); }).catch(() => {}); }, []);
  const ask = async () => {
    if (!q.trim()) return;
    setBusy(true); setErr(null); setRes(null); setOpenHit(null);
    try {
      const body = { question: q };
      if (project) body.workspace = project;
      const r = await api('/api/search', { method: 'POST', body: JSON.stringify(body) });
      setRes(r); setAskedQ(q);
    } catch (e) { setErr(e); }
    setBusy(false);
  };
  const hits = res?.source_hits?.length > 0 ? res.source_hits : null;
  return html`<div class="narrow search-hero">
    <h2>Search documents</h2>
    <div class="search-bar">
      ${projects.length > 0 && html`<select style="margin:0;max-width:200px" value=${project} onChange=${e => setProject(e.target.value)}>
        <option value="">All projects</option>${projects.map(p => html`<option value=${p.id}>${p.values.name || p.id.slice(0, 8)}</option>`)}</select>`}
      <input style="margin:0;flex:1" placeholder="Ask anything about your documents…"
        value=${q} onInput=${e => setQ(e.target.value)}
        onKeyDown=${e => e.key === 'Enter' && ask()} />
      <button class="btn green" onClick=${ask} disabled=${busy}>${busy ? '…' : 'Ask'}</button>
    </div>
    ${err && html`<div class="err">${err.message || JSON.stringify(err)}</div>`}
    ${!res && !busy && !err && html`<div class="muted search-hint">Ask a question to get started — the answer will cite the documents it came from.</div>`}
    ${busy && html`<div class="card" aria-busy="true" aria-live="polite">
      <div class="skel" style="height:20px;width:70%;margin-bottom:12px"></div>
      <div class="skel" style="height:14px;width:100%;margin-bottom:6px"></div>
      <div class="skel" style="height:14px;width:94%;margin-bottom:6px"></div>
      <div class="skel" style="height:14px;width:60%"></div>
      <span class="sr-only">searching documents and composing an answer…</span>
    </div>`}
    ${res && html`<div class="card search-answer">
      <div class="answer-text">${res.answer}</div>
      ${hits ? html`<div class="source-hits">
        <div class="muted">Sources · ${hits.length}</div>
        ${hits.map((h, i) => html`<div class="source-hit">
          <button type="button" class="source-hit-head" aria-expanded=${openHit === i ? 'true' : 'false'}
            onClick=${() => setOpenHit(openHit === i ? null : i)}>
            <span aria-hidden="true">📄</span>
            <span class="source-hit-name">${h.file}</span>
            ${typeof h.anchor === 'number' && html`<span class="source-hit-anchor">chunk ${h.anchor + 1}</span>`}
            <span class="source-hit-chevron" aria-hidden="true">${openHit === i ? '▾' : '▸'}</span>
          </button>
          ${openHit === i && html`<div class="source-hit-body" role="region" aria-label=${`fragment of ${h.file}`}>
            <p>${highlightTerms(h.snippet || '', askedQ)}</p>
          </div>`}
        </div>`)}
      </div>`
      : res.sources?.length > 0 && html`<div class="muted" style="margin-top:12px">
        Sources: ${res.sources.map(s => html`<span class="pill" style="margin-right:6px">${s}</span>`)}</div>`}
    </div>`}
  </div>`;
}

// UploadView: ENH-01 — drag N documents onto a zone, each goes through the
// existing uploadFile() (multipart POST /api/files, same call FieldAtom's
// FileAttachment uses), then a Source record is created the same way the
// generic "+ Source" CreateForm would (POST /api/records/Source with
// {workspace, kind, path, document, basis}) — no new upload endpoint. `kind`
// uses the "Upload" value the pack's enum already reserves for this
// (packs/knowvault/knowvault.dsl's Source.kind); `path` (required by that
// same entity, normally filled via the files.list_dirs folder picker) is
// filled with the dropped file's name since drag-drop deliberately skips
// browsing a folder. Each file gets its own progress/error, independent of
// the others.
function UploadView({ meta }) {
  const [projects, setProjects] = useState([]); const [project, setProject] = useState('');
  const [items, setItems] = useState([]); // [{id, file, status: queued|uploading|creating|done|error, error}]
  const [dragOver, setDragOver] = useState(false);
  useEffect(() => { api('/api/records/Workspace?limit=200').then(r => { const ws = r.records || []; setProjects(ws); if (ws.length > 0) setProject(ws[ws.length - 1].id); }).catch(() => {}); }, []);

  const patchItem = (id, patch) => setItems(list => list.map(it => (it.id === id ? { ...it, ...patch } : it)));

  const processFile = async (item, ws) => {
    patchItem(item.id, { status: 'uploading' });
    try {
      const ref = await uploadFile(item.file);
      patchItem(item.id, { status: 'creating' });
      await api('/api/records/Source', { method: 'POST', body: JSON.stringify({
        values: { workspace: ws, kind: 'Upload', path: item.file.name, document: ref },
        basis: basis(),
      }) });
      patchItem(item.id, { status: 'done' });
    } catch (e) {
      const msg = e?.message ? e.message + (e.fix_hint ? ` — ${e.fix_hint}` : '') : 'upload failed';
      patchItem(item.id, { status: 'error', error: msg });
    }
  };

  const addFiles = (fileList) => {
    if (!project) return;
    const files = Array.from(fileList || []);
    if (!files.length) return;
    const fresh = files.map(f => ({ id: Math.random().toString(36).slice(2), file: f, status: 'queued', error: null }));
    setItems(list => [...list, ...fresh]);
    fresh.forEach(it => processFile(it, project));
  };

  const clearDone = () => setItems(list => list.filter(it => it.status !== 'done'));
  const openPicker = () => project && document.getElementById('kv-upload-input')?.click();

  const STATUS_LABEL = { queued: 'queued', uploading: 'uploading…', creating: 'indexing…', done: '✓ done' };

  return html`<div class="narrow">
    <h2>Upload documents</h2>
    <div class="muted" style="margin-bottom:10px">Drop files to add them as Sources in the selected workspace.</div>
    <label>Workspace</label>
    <select value=${project} onChange=${e => setProject(e.target.value)}>
      <option value="">— choose workspace —</option>
      ${projects.map(p => html`<option value=${p.id}>${p.values.name || p.id.slice(0, 8)}</option>`)}
    </select>
    <div class="dropzone ${dragOver ? 'drag-over' : ''} ${!project ? 'disabled' : ''}"
      role="button" tabIndex=${project ? 0 : -1} aria-disabled=${!project ? 'true' : 'false'}
      aria-label="Drop files here, or press Enter to choose files to upload"
      onDragOver=${e => { e.preventDefault(); if (project) setDragOver(true); }}
      onDragLeave=${() => setDragOver(false)}
      onDrop=${e => { e.preventDefault(); setDragOver(false); addFiles(e.dataTransfer.files); }}
      onClick=${openPicker}
      onKeyDown=${e => { if (project && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); openPicker(); } }}>
      <div class="dropzone-icon" aria-hidden="true">⬆</div>
      ${project ? html`<div>Drop files here, or click to choose</div>` : html`<div class="muted">choose a workspace first</div>`}
      <input id="kv-upload-input" type="file" multiple style="display:none"
        onChange=${e => { addFiles(e.target.files); e.target.value = ''; }} />
    </div>
    ${items.length > 0 && html`<div class="upload-list">
      <div style="display:flex;align-items:center;gap:8px;margin:14px 0 6px">
        <b style="flex:1">Files · ${items.length}</b>
        <button class="btn" onClick=${clearDone}>Clear finished</button>
      </div>
      ${items.map(it => html`<div class="upload-row" key=${it.id}>
        <span class="upload-row-name" title=${it.file.name}>${it.file.name}</span>
        <span class="upload-row-size muted">${Math.round(it.file.size / 1024)} KB</span>
        <span class="upload-row-status ${it.status === 'error' ? 'status-error' : it.status === 'done' ? 'status-done' : ''}">
          ${it.status === 'error' ? `✕ ${it.error}` : STATUS_LABEL[it.status]}
        </span>
      </div>`)}
    </div>`}
  </div>`;
}

// InviteForm: issue a one-time code for a new user. Optionally bind it to a
// record (entity + record + the field that holds the actor id) so the new user
// sees only that row — the customer portal: same UI, scoped actor, no separate
// app. Everything is a select, nothing is typed (a record is a ref picker).
function InviteForm({ meta }) {
  const [role, setRole] = useState('');
  const [entity, setEntity] = useState(''); const [recordId, setRecordId] = useState(''); const [bindField, setBindField] = useState('');
  const [code, setCode] = useState(null); const [err, setErr] = useState(null);
  const humanRoles = (meta.roles || []).filter(r => !r.is_agent);
  const ent = meta.entities.find(e => e.name === entity);
  const bindFields = (ent?.fields || []).filter(f => f.type === 'ref' || f.type === 'string');
  const create = async () => {
    setErr(null); setCode(null);
    try {
      const body = { role };
      if (entity && recordId && bindField) Object.assign(body, { entity, record_id: recordId, bind_field: bindField });
      const r = await api('/api/invites', { method: 'POST', body: JSON.stringify(body) });
      setCode(r.invite_code);
    } catch (e) { setErr(e); }
  };
  return html`<div class="card">
    <h3 style="margin-top:0">Invite a user</h3>
    <div class="muted" style="margin-bottom:8px">A one-time code. Bind it to a record and the new user sees only that row — a customer portal on the same UI.</div>
    <div style="display:grid;grid-template-columns:1fr 1fr;gap:4px 18px">
      <div><label>Role</label>
        <select value=${role} onChange=${e => setRole(e.target.value)}>
          <option value="">— choose role —</option>${humanRoles.map(r => html`<option value=${r.name}>${r.name}</option>`)}</select></div>
      <div><label>Bind to (optional)</label>
        <select value=${entity} onChange=${e => { setEntity(e.target.value); setRecordId(''); setBindField(''); }}>
          <option value="">— no binding —</option>${meta.entities.map(e => html`<option value=${e.name}>${elab(e)}</option>`)}</select></div>
      ${entity && html`<div><label>Record</label><${RefInput} field=${{ ref: entity }} value=${recordId} onChange=${setRecordId} /></div>`}
      ${entity && html`<div><label>Field that binds the user</label>
        <select value=${bindField} onChange=${e => setBindField(e.target.value)}>
          <option value="">—</option>${bindFields.map(f => html`<option value=${f.name}>${flab(f)}</option>`)}</select></div>`}
    </div>
    ${err && html`<div class="err">${err.message} ${err.fix_hint || ''}</div>`}
    <button class="btn green" style="margin-top:8px" onClick=${create} disabled=${!role}>Create invite</button>
    ${code && html`<div style="margin-top:10px;padding:10px;background:#161b22;border-radius:6px">
      <div class="muted" style="font-size:12px">Share this one-time code (the user redeems it at the login screen):</div>
      <code style="font-size:15px;user-select:all">${code}</code></div>`}
  </div>`;
}

// Agents screen: the actor directory — who acts on this node, which model
// stands behind each agent, and the revoke switch. Humans only (server-side).
function AgentsView({ meta }) {
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
    ${meta && html`<${InviteForm} meta=${meta} />`}
    <div class="muted" style="margin:14px 0 10px">Registered actors of this node. Revoking kills the token and signatures immediately.</div>
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
function SingletonView({ ent, refresh, meta }) {
  const [rows, setRows] = useState(null);
  useEffect(() => { api(`/api/records/${ent.name}`).then(r => setRows(r.records || [])); }, [ent.name]);
  if (rows === null) return html`<div class="muted">loading…</div>`;
  if (rows.length === 0) return html`<div><h2>${ent.name}</h2>
    ${ent.can_create ? html`<${CreateForm} ent=${ent} onDone=${() => location.reload()} />`
      : html`<div class="muted">not configured yet — ask a role that can create it</div>`}</div>`;
  return html`<${RecordView} ent=${ent} id=${rows[0].id} refresh=${refresh} meta=${meta} />`;
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
      ${cols.map(c => { const f = ent.fields.find(x => x.name === c);
        return html`<td>${f ? html`<${FieldValue} field=${f} value=${r.values[c]} />` : fmtPlain(r.values[c])}</td>`; })}</tr>`)}</tbody></table>
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
        <div class="kcard" onClick=${() => nav(`/e/${ent.name}/${r.id}`)}>${fmtPlain(r.values[title])}</div>`)}
    </div>`)}</div>
  </div>`;
}

// FieldValue: the READ-mode dispatcher — refs resolved to labels (RefLink),
// arrays as chips, the rest type-formatted. All of that logic lives in
// kalita-ui's FieldAtom/tokens/format.js now (see atoms.js's RefLink doc
// comment for why the former useRefLabel/RefValue/ArrayRefValue moved
// there); this function is what the LLD calls the dispatcher staying put.
function FieldValue({ field, value }) {
  return html`<${FieldAtom} field=${field} value=${value} onNavigate=${gotoRef} fetcher=${refFetcher} />`;
}

// LinksPanel: the `link` construct surfaced — named bidirectional relations
// (blocks / blocked_by, relates_to). Shows linked records grouped by relation,
// each clickable and removable, and an "add a link" control: pick the relation,
// then the target record via the lookup modal. Only shown if the pack declares
// links for this entity (meta.links).
function LinksPanel({ ent, id }) {
  const [links, setLinks] = useState(null);
  const [rel, setRel] = useState('');
  const [lk, setLk] = useState(null);
  const [err, setErr] = useState(null);
  const defs = ent.links || [];
  const load = () => api(`/api/records/${ent.name}/${id}/links`).then(r => setLinks(r.links || [])).catch(() => setLinks([]));
  useEffect(load, [ent.name, id]);
  const mutate = async (method, name, otherId) => {
    setErr(null);
    try { await api(`/api/records/${ent.name}/${id}/links`, { method, body: JSON.stringify({ name, other_id: otherId, basis: basis() }) }); load(); }
    catch (e) { setErr(e); }
  };
  if (!links || (links.length === 0 && (defs.length === 0 || !ent.can_update))) return null;
  const byName = {};
  links.forEach(l => { (byName[l.name] = byName[l.name] || []).push(l); });
  const targetOf = (name) => (defs.find(d => d.name === name) || {}).target;
  return html`<div class="card">
    <h3 style="margin-top:0">Links</h3>
    ${err && html`<div class="err">${err.message} ${err.fix_hint || ''}</div>`}
    ${Object.keys(byName).length === 0 && html`<div class="muted">no links yet</div>`}
    ${Object.entries(byName).map(([name, items]) => html`<div style="margin-bottom:8px">
      <div class="muted" style="font-size:12px">${humanize(name)}</div>
      ${items.map(l => html`<span class="pill" style="margin:0 6px 4px 0;display:inline-block">
        <a onClick=${() => nav(`/e/${l.entity}/${l.record_id}`)}><${RefLink} field=${{ ref: l.entity }} value=${l.record_id} fetcher=${refFetcher} /></a>
        ${ent.can_update && html`<span style="cursor:pointer;color:var(--dim)" onClick=${() => mutate('DELETE', name, l.record_id)}> ✕</span>`}</span>`)}
    </div>`)}
    ${ent.can_update && defs.length > 0 && html`<div style="display:flex;gap:8px;align-items:flex-start;margin-top:8px">
      <select style="max-width:230px;margin:0" value=${rel} onChange=${e => setRel(e.target.value)}>
        <option value="">add a link…</option>${defs.map(d => html`<option value=${d.name}>${humanize(d.name)} → ${d.target}</option>`)}</select>
      ${rel && html`<button class="btn" onClick=${() => setLk({ name: rel, target: targetOf(rel) })}>🔍 pick ${targetOf(rel)}</button>`}
    </div>`}
    ${lk && html`<${LookupModal} entity=${lk.target} onPick=${oid => mutate('POST', lk.name, oid)} onClose=${() => setLk(null)} />`}
  </div>`;
}

// --- audit timeline (the differentiator surfaced) -------------------------------

// kindLabel/changeLines/isMutation moved to kalita-ui's molecules.js (used by
// its TimelineItem, and isMutation is needed here too — both imported above).

// Timeline: a record's audited history as a readable sequence — every change,
// who made it, and (for a supervisor) one-click undo of the last change or a
// rewind to any earlier point. The undo is itself a journaled event, so the
// timeline shows the undos too (basis: revert/rewind of seq N). Each row is
// kalita-ui's TimelineItem molecule; this component keeps only the
// load/undo/rewind orchestration.
function Timeline({ ent, id, canRevert, onChanged }) {
  const [events, setEvents] = useState(null);
  const [busy, setBusy] = useState(false); const [err, setErr] = useState(null);
  const load = () => api(`/api/records/${ent.name}/${id}/journal`).then(j => setEvents(j.events)).catch(setErr);
  useEffect(load, [ent.name, id]);
  const mutations = (events || []).filter(e => isMutation(e.kind));
  const lastMutSeq = mutations.length ? mutations[mutations.length - 1].seq : 0;

  const undoLast = async () => {
    setBusy(true); setErr(null);
    try { await api(`/api/records/${ent.name}/${id}/revert`, { method: 'POST', body: JSON.stringify({ basis: basis() }) }); await load(); onChanged(); }
    catch (e) { setErr(e); } setBusy(false);
  };
  const rewindTo = async (seq) => {
    setBusy(true); setErr(null);
    try { await api(`/api/records/${ent.name}/${id}/rewind`, { method: 'POST', body: JSON.stringify({ to_seq: seq, basis: basis() }) }); await load(); onChanged(); }
    catch (e) { setErr(e); } setBusy(false);
  };

  if (!events) return html`<div class="card muted">loading history…</div>`;
  return html`<div class="card">
    <div style="display:flex;align-items:center;gap:10px;margin-bottom:8px">
      <b style="flex:1">History · ${events.length} events</b>
      ${canRevert && lastMutSeq > 0 && html`<button class="btn" disabled=${busy} onClick=${undoLast} title="undo the most recent change">↩ Undo last</button>`}
    </div>
    ${err && html`<div class="err">${err.message} ${err.fix_hint || ''}</div>`}
    ${events.map(e => html`<${TimelineItem} e=${e} canRevert=${canRevert} busy=${busy} onRewind=${rewindTo} lastMutSeq=${lastMutSeq} />`)}
  </div>`;
}

// RelatedPanel: master-detail — the detail rows of every entity that points at
// this record (order lines, payments, findings). Generic; for the legal pack it
// IS the findings risk card. readOnly drops the row actions and the add button.
function RelatedPanel({ ent, id, meta, readOnly }) {
  const children = ent.children || [];
  if (!children.length) return null;
  return html`${children.map(c => html`<${ChildTable} parent=${id} child=${c} meta=${meta} readOnly=${readOnly} />`)}`;
}

function ChildTable({ parent, child, meta, readOnly }) {
  const cm = meta.entities.find(e => e.name === child.entity) || { fields: [], actions: [], ui: {} };
  const [rows, setRows] = useState(null); const [adding, setAdding] = useState(false); const [err, setErr] = useState(null);
  const load = () => api(`/api/records/${child.entity}?${child.field}=${parent}&limit=100`)
    .then(r => setRows(r.records || [])).catch(() => setRows([]));
  useEffect(load, [child.entity, child.field, parent]);
  if (!rows) return null;
  const wf = cm.workflow_field;
  const cols = ((cm.ui && cm.ui.list_columns && cm.ui.list_columns.length)
      ? cm.ui.list_columns : cm.fields.filter(f => f.readable && !f.computed).map(f => f.name))
    .filter(n => n !== child.field && cm.fields.some(f => f.name === n)).slice(0, 5);
  const act = async (rid, action) => {
    setErr(null);
    try { await api(`/api/records/${child.entity}/${rid}/act`, { method: 'POST', body: JSON.stringify({ action, basis: basis() }) }); load(); }
    catch (e) { setErr(e); }
  };
  if (rows.length === 0 && (readOnly || !cm.can_create)) return null;
  return html`<div class="card">
    <h3 style="margin-top:0">${child.label || child.entity}${rows.length ? ` · ${rows.length}` : ''}</h3>
    ${err && html`<div class="err">${err.message}</div>`}
    ${rows.length === 0 ? html`<div class="muted">none yet</div>` : html`
      <table><thead><tr>${cols.map(c => html`<th>${colLabel(cm, c)}</th>`)}${!readOnly ? html`<th></th>` : ''}</tr></thead>
      <tbody>${rows.map(r => {
        const st = wf && r.values[wf];
        const acts = readOnly ? [] : (cm.actions || []).filter(a => a.can_act && (a.from === st || a.from === 'any'));
        return html`<tr>
          ${cols.map(c => html`<td style="cursor:pointer" onClick=${() => nav(`/e/${child.entity}/${r.id}`)}>
            <${FieldValue} field=${cm.fields.find(f => f.name === c)} value=${r.values[c]} /></td>`)}
          ${!readOnly ? html`<td>${acts.map(a => html`<button class="btn" style="padding:3px 8px;font-size:12px;margin:0 4px 0 0"
            onClick=${() => act(r.id, a.action)}>${a.label || humanize(a.action)}${a.requires_approval ? ' ✍' : ''}</button>`)}</td>` : ''}
        </tr>`;
      })}</tbody></table>`}
    ${!readOnly && cm.can_create && html`<div style="margin-top:8px">
      ${adding ? html`<${CreateForm} ent=${cm} initial=${{ [child.field]: parent }} hide=${[child.field]} onDone=${() => { setAdding(false); load(); }} />`
        : html`<button class="btn" onClick=${() => setAdding(true)}>+ ${child.label || child.entity}</button>`}
    </div>`}
  </div>`;
}

function RecordView({ ent, id, refresh, meta }) {
  const [rec, setRec] = useState(null); const [showHist, setShowHist] = useState(false);
  const [edit, setEdit] = useState({}); const [editing, setEditing] = useState(false);
  const [err, setErr] = useState(null); const [note, setNote] = useState(null);
  const load = () => api(`/api/records/${ent.name}/${id}`).then(setRec).catch(setErr);
  useEffect(() => { load(); setShowHist(false); setEdit({}); setEditing(false); }, [ent.name, id]);

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

  return html`<div>
    <h2>${elab(ent)} <span class="muted">${reclabel(rec.values)}</span> ${state && html`<span class="pill">${state}</span>`}</h2>
    <div style="margin-bottom:12px;display:flex;gap:8px;flex-wrap:wrap">
      ${!editing && actions.map(a => html`<button class="btn ${a.requires_approval ? '' : 'green'}" onClick=${() => act(a.action)}>${a.label || humanize(a.action)}${a.requires_approval ? ' ✍' : ''}</button>`)}
      ${!editing && ent.can_update && html`<button class="btn" onClick=${() => setEditing(true)}>✎ Edit</button>`}
      <button class="btn ${showHist ? 'on' : ''}" onClick=${() => setShowHist(!showHist)}>${showHist ? 'hide history' : '🕰 History'}</button>
    </div>
    ${note && html`<div class="muted" style="margin:8px 0">${note}</div>`}
    ${err && html`<div class="err">${err.message} ${err.rule ? `(${err.rule})` : ''} ${err.fix_hint || ''}</div>`}
    <div class="card">
      <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:2px 18px">
      ${ent.fields.filter(f => f.readable).map(f => {
        const editable = editing && f.writable && f.name !== ent.workflow_field;
        const val = f.name in edit ? edit[f.name] : rec.values[f.name];
        return html`<div style=${'grid-column:span ' + fieldSpan(f)}>
          <label>${flab(f)}${f.computed ? ' (computed)' : ''}</label>
          ${editable
            ? html`<${FieldInput} field=${f} entity=${ent.name} value=${val} onChange=${v => setEdit({ ...edit, [f.name]: v })} />`
            : html`<div style="padding:4px 0 10px;min-height:20px"><${FieldValue} field=${f} value=${rec.values[f.name]} /></div>`}
        </div>`;
      })}
      </div>
      ${editing && html`<div style="margin-top:10px">
        <button class="btn green" onClick=${save}>Save</button>
        <button class="btn" onClick=${cancel}>Cancel</button>
      </div>`}
    </div>
    ${showHist && html`<${Timeline} ent=${ent} id=${id} canRevert=${ent.can_revert} onChanged=${() => { load(); refresh(); }} />`}
    ${meta && html`<${RelatedPanel} ent=${ent} id=${id} meta=${meta} />`}
    <${LinksPanel} ent=${ent} id=${id} />
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

// Dashboards: table-wide aggregate tiles (count/sum/avg/min/max), some
// grouped. Each tile already respects the viewer's row permissions
// server-side. Tile itself is kalita-ui's DashboardTile molecule (imported above).
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
          ${data.tiles.map(t => html`<${DashboardTile} t=${t} />`)}</div>`}
  </div>`;
}

// AgentBoard: the agent workforce, visible to the operator — every task on an
// agent role flowing waiting → working → done. The differentiator no ERP shows:
// agents as audited employees you can watch. Auto-refreshes; a card opens the
// record the agent is working on.
function AgentBoard() {
  const [tasks, setTasks] = useState(null); const [err, setErr] = useState(null);
  const load = () => api('/api/agent-board').then(r => setTasks(r.tasks || [])).catch(setErr);
  useEffect(() => { load(); const t = setInterval(load, 5000); return () => clearInterval(t); }, []);
  if (err) return html`<div class="err">${err.message || 'humans only'}</div>`;
  if (!tasks) return html`<div class="muted">loading…</div>`;
  const cols = [
    { title: 'Waiting for an agent', f: t => t.status === 'open' },
    { title: 'Agent working', f: t => t.status === 'taken' },
    { title: 'Done', f: t => t.status === 'completed' },
  ];
  const failed = tasks.filter(t => t.status === 'failed');
  const card = (t) => html`<div class="kcard" style=${t.entity ? 'cursor:pointer' : 'cursor:default'}
      onClick=${() => t.entity && t.record_id && nav(`/e/${t.entity}/${t.record_id}`)}>
    <div><b>${humanize(t.action || t.kind)}</b>${t.entity ? html` <span class="muted">· ${t.entity}</span>` : ''}</div>
    <div class="muted" style="font-size:12px">role ${t.role}${t.taken_by ? html` · taken by ${t.taken_by}` : ''}</div>
    ${t.note ? html`<div class="muted" style="font-size:11px;margin-top:3px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis" title=${t.note}>${t.note}${t.note_at ? html` <span style="opacity:0.55">${new Date(t.note_at).toLocaleTimeString()}</span>` : ''}</div>` : ''}
  </div>`;
  return html`<div>
    <h2>Agent board <span class="muted" style="font-size:13px">live · ${tasks.length} tasks</span></h2>
    ${tasks.length === 0 && html`<div class="muted">no agent tasks yet — a transition with <code>assignee=agent(Role)</code> queues work here for that agent role.</div>`}
    <div class="cols">${cols.map(c => html`<div class="col"><h4>${c.title} · ${tasks.filter(c.f).length}</h4>
      ${tasks.filter(c.f).map(card)}</div>`)}</div>
    ${failed.length > 0 && html`<div style="margin-top:14px"><h4 style="color:var(--red,#e06)">Failed · ${failed.length}</h4>
      <div class="cols"><div class="col">${failed.map(card)}</div></div></div>`}
  </div>`;
}

// --- shell ----------------------------------------------------------------------

function App() {
  const route = useRoute();
  const [meta, setMeta] = useState(null); const [err, setErr] = useState(null);
  const [inboxCount, setInboxCount] = useState(0);
  // ENH-03: collapsible sidebar, persisted across reloads (pure UI state —
  // no server round-trip, no domain knowledge).
  const [collapsed, setCollapsed] = useState(() => localStorage.kalitaSidebarCollapsed === '1');
  const toggleSidebar = () => setCollapsed(c => { localStorage.kalitaSidebarCollapsed = c ? '0' : '1'; return !c; });
  const refresh = async () => {
    try {
      const [a, p] = await Promise.all([api('/api/approvals'), api('/api/proposals')]);
      setInboxCount((a.approvals?.length || 0) + (p.proposals?.length || 0));
    } catch { /* ignore */ }
  };
  useEffect(() => { api('/api/meta').then(m => { me = { id: m.actor_id, role: m.role }; setMeta(m); refresh(); }).catch(setErr); }, []);

  if (err) return html`<div class="login card"><div class="err">${err.message || 'node unreachable'}</div>
    <button class="btn" onClick=${() => { session.clear(); location.reload(); }}>Sign in again</button></div>`;
  // ENH-03 "states as design": a skeleton instead of literal "connecting…"
  // text, with the same message kept for screen readers via aria-live.
  if (!meta) return html`<div class="login card" aria-busy="true" aria-live="polite">
    <div class="skel" style="height:22px;width:55%;margin-bottom:14px"></div>
    <div class="skel" style="height:14px;width:90%;margin-bottom:8px"></div>
    <div class="skel" style="height:14px;width:75%;margin-bottom:8px"></div>
    <div class="skel" style="height:14px;width:65%"></div>
    <span class="sr-only">Connecting…</span>
  </div>`;

  const parts = route.split('/').filter(Boolean); // e.g. ["e","Card","id"]
  const ent = parts[0] === 'e' || parts[0] === 'board' ? meta.entities.find(x => x.name === parts[1]) : null;
  // ENH-01: the Upload nav item is KnowVault-specific (Source is a pack
  // entity, not core) — gated on the actor actually being able to create a
  // Source, same check EntityList already makes for its own "+ Source"
  // button, so a role without create rights never sees a dead-end page.
  const sourceEnt = meta.entities.find(x => x.name === 'Source');
  const canUpload = !!(sourceEnt && sourceEnt.can_create);
  let view = html`<${Inbox} meta=${meta} refresh=${refresh} />`;
  if (parts[0] === 'search') view = html`<${SearchView} />`;
  else if (parts[0] === 'upload' && canUpload) view = html`<${UploadView} meta=${meta} />`;
  else if (parts[0] === 'dashboards') view = html`<${Dashboards} />`;
  else if (parts[0] === 'agent-board') view = html`<${AgentBoard} />`;
  else if (parts[0] === 'agents') view = html`<${AgentsView} meta=${meta} />`;
  else if (parts[0] === 'e' && ent && parts[2]) view = html`<${RecordView} ent=${ent} id=${parts[2]} refresh=${refresh} meta=${meta} />`;
  else if (parts[0] === 'e' && ent && ent.singleton) view = html`<${SingletonView} ent=${ent} refresh=${refresh} meta=${meta} />`;
  else if (parts[0] === 'e' && ent) view = html`<${EntityList} ent=${ent} />`;
  else if (parts[0] === 'board' && ent) view = html`<${Board} ent=${ent} />`;

  return html`<div class="shell">
    <div class="side ${collapsed ? 'collapsed' : ''}">
      <div class="side-head">
        <h1>${BRAND.name}</h1>
        <button type="button" class="side-toggle" onClick=${toggleSidebar}
          aria-label=${collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          title=${collapsed ? 'Expand sidebar' : 'Collapse sidebar'}>${collapsed ? '»' : '«'}</button>
      </div>
      <div class="who"><span class="lbl">${meta.pack || '(genesis)'} · v${meta.def_version}<br/>${meta.actor_id} — ${meta.role}</span>
        <a href="#" class="lbl" style="display:block" onClick=${e => { e.preventDefault(); session.clear(); location.reload(); }}>sign out</a></div>
      <nav class="nav" aria-label="Main navigation">
        ${meta.search && html`<a href="#/search" class=${route === '/search' ? 'on' : ''} aria-current=${route === '/search' ? 'page' : undefined} title="Search">
          <span aria-hidden="true">🔍</span><span class="navdot">S</span><span class="lbl">Search</span></a>`}
        ${canUpload && html`<a href="#/upload" class=${route === '/upload' ? 'on' : ''} aria-current=${route === '/upload' ? 'page' : undefined} title="Upload">
          <span aria-hidden="true">⬆</span><span class="navdot">U</span><span class="lbl">Upload</span></a>`}
        <a href="#/inbox" class=${route === '/inbox' ? 'on' : ''} aria-current=${route === '/inbox' ? 'page' : undefined} title="Inbox">
          <span aria-hidden="true">📥</span><span class="navdot">I</span><span class="lbl">Inbox</span>${inboxCount > 0 && html`<span class="badge">${inboxCount}</span>`}</a>
        <a href="#/dashboards" class=${route === '/dashboards' ? 'on' : ''} aria-current=${route === '/dashboards' ? 'page' : undefined} title="Dashboards">
          <span aria-hidden="true">📊</span><span class="navdot">D</span><span class="lbl">Dashboards</span></a>
        <a href="#/agent-board" class=${route === '/agent-board' ? 'on' : ''} aria-current=${route === '/agent-board' ? 'page' : undefined} title="Agent board">
          <span aria-hidden="true">🤖</span><span class="navdot">A</span><span class="lbl">Agent board</span></a>
        ${meta.entities.map(e => html`<a href="#/e/${e.name}" class=${parts[1] === e.name ? 'on' : ''} aria-current=${parts[1] === e.name ? 'page' : undefined} title=${elab(e)}>
          <span class="navdot" aria-hidden="true">${elab(e)[0]}</span><span class="lbl">${elab(e)}</span></a>`)}
        <a href="#/agents" class=${route === '/agents' ? 'on' : ''} aria-current=${route === '/agents' ? 'page' : undefined} title="Agents & users"
          style="margin-top:10px;border-top:1px solid var(--line);padding-top:10px">
          <span aria-hidden="true">👤</span><span class="navdot">A</span><span class="lbl">Agents</span></a>
      </nav>
    </div>
    <div class="main">${view}</div>
  </div>`;
}

loadBrand().then(() =>
  render(session.get() ? html`<${App} />` : html`<${Login} />`, document.getElementById('root')));
