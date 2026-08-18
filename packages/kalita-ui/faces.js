// kalita-ui / faces.js — LLD-05 PK-1 §1 "faces" layer: a face is a
// composition of organisms + molecules under ONE product screen, assembled
// from the library — not written from scratch, and not carrying a single
// line of domain JS (no hardcoded entity or field names; anything
// pack-specific arrives as a prop from the host page, which reads it out of
// /api/meta same as RecordCard does).
import { html, useState, useEffect } from 'kalita-vendor';
import { formatValue, reclabel } from './tokens/format.js';
import { StatusChip, renderSpec } from './atoms.js';

// --- SearchFace ----------------------------------------------------------------

// SearchFace: search box + answer + sources — the KnowVault product face.
// Generalized off web/app.js's SearchView: the "scope" dropdown (originally
// hardcoded to the Workspace entity) is now driven by an optional
// `scopeEntity` prop + injected `api`; omit it and the scope selector simply
// does not render (search face with no scoping concept at all).
export function SearchFace({ api, scopeEntity, scopeParam = 'workspace', placeholder = 'Ask anything about your documents…' }) {
  const [q, setQ] = useState('');
  const [busy, setBusy] = useState(false);
  const [res, setRes] = useState(null);
  const [err, setErr] = useState(null);
  const [scopes, setScopes] = useState([]);
  const [scope, setScope] = useState('');
  useEffect(() => {
    if (!scopeEntity) return;
    api(`/api/records/${scopeEntity}?limit=200`).then((r) => {
      const rows = r.records || [];
      setScopes(rows);
      if (rows.length > 0) setScope(rows[rows.length - 1].id);
    }).catch(() => {});
  }, [scopeEntity]);
  const ask = async () => {
    if (!q.trim()) return;
    setBusy(true); setErr(null); setRes(null);
    try {
      const body = { question: q };
      if (scope) body[scopeParam] = scope;
      setRes(await api('/api/search', { method: 'POST', body: JSON.stringify(body) }));
    } catch (e) { setErr(e); }
    setBusy(false);
  };
  return html`<div>
    <div style="display:flex;gap:8px;align-items:flex-start;max-width:720px">
      ${scopes.length > 0 && html`<select style="margin:0;max-width:200px" value=${scope} onChange=${(e) => setScope(e.target.value)}>
        <option value="">All</option>${scopes.map((s) => html`<option value=${s.id}>${reclabel(s.values)}</option>`)}</select>`}
      <input style="margin:0" placeholder=${placeholder} value=${q} onInput=${(e) => setQ(e.target.value)}
        onKeyDown=${(e) => e.key === 'Enter' && ask()} />
      <button class="btn green" onClick=${ask} disabled=${busy}>${busy ? '…' : 'Ask'}</button>
    </div>
    ${err && html`<div class="err">${err.message || JSON.stringify(err)}</div>`}
    ${res && html`<div class="card" style="max-width:720px;margin-top:14px">
      <div style="white-space:pre-wrap;line-height:1.55">${res.answer}</div>
      ${res.sources?.length > 0 && html`<div class="muted" style="margin-top:12px">
        Sources: ${res.sources.map((s) => html`<span class="pill" style="margin-right:6px">${s}</span>`)}</div>`}
    </div>`}
    ${busy && html`<div class="muted" style="margin-top:12px">searching and composing an answer…</div>`}
  </div>`;
}

// --- RiskCardFace ----------------------------------------------------------------

// RiskCardFace: a clause-risk card — for the Legal product, but the
// component itself knows nothing about "Legal": which field holds the risk
// level and which holds the clause text are told to it explicitly
// (`riskField`/`clauseField`, field NAMES), not guessed from field-name
// substrings (that would be Legal-pack knowledge wearing a "heuristic"
// costume). Omit them and the face falls back to RecordCard's own
// convention — first enum field, first text field — same "first N readable
// fields" spirit as RecordCard.
//
// `riskTone(value) -> 'ok'|'warn'|'bad'|'default'` is likewise injected —
// the face does not hardcode which risk VALUES ("High"/"Critical"/...) mean
// danger; a pack's vocabulary is its own.  `renderFindings({ent, id})` is a
// render-prop for a findings/children list (e.g. web/app.js's RelatedPanel),
// kept as a slot rather than an import so this face stays library-composed.
export function RiskCardFace({ ent, record, riskField, clauseField, riskTone, renderFindings, onOpenRecord }) {
  const rf = ent.fields.find((f) => f.name === riskField) || ent.fields.find((f) => f.type === 'enum');
  const cf = ent.fields.find((f) => f.name === clauseField) || ent.fields.find((f) => f.type === 'text');
  const others = ent.fields.filter((f) => f.readable && f !== rf && f !== cf);
  const tone = rf && riskTone ? riskTone(record.values[rf.name]) : 'default';
  return html`<div class="card">
    <div style="display:flex;justify-content:space-between;align-items:flex-start;gap:8px">
      <b onClick=${onOpenRecord ? () => onOpenRecord(ent.name, record.id) : undefined}
         style=${onOpenRecord ? 'cursor:pointer' : ''}>${reclabel(record.values)}</b>
      ${rf && html`<${StatusChip} value=${record.values[rf.name]} tone=${tone} />`}
    </div>
    ${cf && record.values[cf.name] && html`<div style="white-space:pre-wrap;margin-top:8px;font-size:13px;background:var(--input-bg,#161b22);padding:8px;border-radius:var(--radius,6px)">${record.values[cf.name]}</div>`}
    ${others.length > 0 && html`<div style="display:grid;grid-template-columns:repeat(2,1fr);gap:2px 18px;margin-top:8px">
      ${others.slice(0, 6).map((f) => html`<div style="font-size:13px"><span class="muted">${f.label || f.name}:</span> ${renderSpec(formatValue(f, record.values[f.name]))}</div>`)}
    </div>`}
    ${renderFindings ? html`<div style="margin-top:10px">${renderFindings({ ent, id: record.id })}</div>` : null}
  </div>`;
}

// --- ChatFace ----------------------------------------------------------------

// ChatFace: KChat, the operator chat (LLD-06 §2 PK-3) — a Message stream +
// input box talking to workers/chat_runner's `/chat/turn`, and NOTHING else.
// Like every other face here, it carries zero domain knowledge: chat_runner
// already resolved everything product-specific (which tool, which pack) on
// the worker side, under the SAME bearer this component's `chatApi` call
// carries — ChatFace itself never sees or handles a token, it only calls the
// injected capability function, exactly like SearchFace calls `api` (see
// this file's header: no hardcoded pack/entity names, ever).
//
// `chatApi` defaults to `api` (same capability-injection convention as
// every other face) — pass a distinct one only when chat_runner is served
// from a different origin than the node's own /api (deployment routing,
// not this component's concern). `pending_approval` on a turn's response
// (workers/chat_runner/chat_runner.py `handle_turn`'s third return value)
// is a STRUCTURED signal, not something this component greps out of the
// reply text — LLD-06 §2: "renders pending-approval when a chat action
// hits HITL."
//
// Message history is kept CLIENT-SIDE for the running conversation (each
// turn appends the user's line + the assistant's reply locally) — the
// governed source of truth is the assistant.Message records chat_runner
// itself writes on the node; this component does not re-fetch them per
// keystroke, it only needs to render what just happened in THIS window.
export function ChatFace({ api, chatApi, sessionId: initialSessionId = null, title = 'Chat',
  placeholder = 'Ask your system, or say what you need done…' }) {
  const send = chatApi || api;
  const [sessionId, setSessionId] = useState(initialSessionId);
  const [messages, setMessages] = useState([]);
  const [input, setInput] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState(null);

  const submit = async () => {
    const text = input.trim();
    if (!text || busy) return;
    setInput('');
    setErr(null);
    setMessages((m) => [...m, { role: 'user', body: text }]);
    setBusy(true);
    try {
      const res = await send('/chat/turn', { method: 'POST', body: JSON.stringify({ session_id: sessionId, message: text }) });
      setSessionId(res.session_id);
      setMessages((m) => [...m, { role: 'assistant', body: res.reply, pendingApproval: !!res.pending_approval }]);
    } catch (e) {
      setErr(e);
    }
    setBusy(false);
  };

  return html`<div class="card" style="max-width:720px;min-height:320px;display:flex;flex-direction:column">
    <b style="margin-bottom:8px">${title}</b>
    <div style="flex:1;overflow-y:auto;display:flex;flex-direction:column;gap:8px;min-height:220px;max-height:420px">
      ${messages.length === 0 && html`<div class="muted">Ask your system, or say what you need done.</div>`}
      ${messages.map((m, i) => html`<div key=${i} style=${'align-self:' + (m.role === 'user' ? 'flex-end' : 'flex-start') + ';max-width:85%'}>
        ${m.pendingApproval && html`<div style="margin-bottom:4px"><${StatusChip} value="pending approval" tone="warn" /></div>`}
        <div class="card" style=${'padding:8px 12px;white-space:pre-wrap;' + (m.role === 'user' ? 'background:var(--accent-bg,#1f6feb22)' : '')}>${m.body}</div>
      </div>`)}
      ${busy && html`<div class="muted">…</div>`}
    </div>
    ${err && html`<div class="err">${err.message || JSON.stringify(err)}</div>`}
    <div style="display:flex;gap:8px;margin-top:10px">
      <input style="margin:0;flex:1" placeholder=${placeholder} value=${input}
        onInput=${(e) => setInput(e.target.value)} onKeyDown=${(e) => e.key === 'Enter' && submit()} />
      <button class="btn green" onClick=${submit} disabled=${busy}>${busy ? '…' : 'Send'}</button>
    </div>
  </div>`;
}
