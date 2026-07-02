// kalita-ui / organisms.js — LLD-05 PK-1 §1 "organisms" layer: the STANDARD
// renderer over kalita-ui/hooks.js's headless data layer, built from atoms/
// molecules + /api/meta. Preact+htm, mirroring the SHAPE (not the exact API
// — different rendering stack) of packages/kalita-sdk/components.js's React
// organisms (KList/KDetail/KBoard/KInbox). For use by web/catalog.html and,
// over time, web/app.js.
//
// Headless/visual split: every organism here is presentation ONLY — all
// data-fetching, loading/error state, and permission derivation (which
// actions can_act) live in hooks.js (useRecordsData/useRecordData/
// useInboxData/entityMeta/actionsFor), which has zero h()/html() calls. A
// consumer who wants a different visual can import hooks.js directly and
// paint their own markup without touching this file — same contract
// kalita-sdk/react.js already gives the React stack.
//
// Closed component registry: every element below is either a plain HTML tag
// or one of kalita-ui's own exported atoms/molecules — nothing here turns a
// meta value or API response into a raw HTML string. Field VALUES are only
// ever placed in htm text-interpolation position (`${value}`, auto-escaped
// by preact/htm — never `dangerouslySetInnerHTML`/`.innerHTML`), and
// field.type only ever SELECTS which registered atom renders (a fixed
// switch in atoms.js's FieldAtom), never contributes markup text itself.
//
// Network capability injection: every organism takes an `api(path, opts)`
// function prop with the SAME signature as web/app.js's `api()` (see
// atoms.js's file header on capability injection) — pass that function
// straight through when wiring from app.js; catalog.html injects a
// fixture-backed stand-in. Organisms do NOT know about auth tokens or
// sessions — that's the injected api()'s job.
import { html } from 'kalita-vendor';
import { humanize, reclabel } from './tokens/format.js';
import { FieldAtom, StatusChip } from './atoms.js';
import { RecordCard, ApprovalCard } from './molecules.js';
import { useRecordsData, useRecordData, useInboxData, entityMeta, actionsFor } from './hooks.js';

// KList: a permission-aware table. Columns come from the entity's
// ui.list_columns or its readable fields (first 6) — identical convention to
// web/app.js's EntityList and kalita-sdk's KList.
export function KList({ api, meta, entity, onRowClick, limit = 25 }) {
  const { records, loading, error } = useRecordsData(api, entity, { limit });
  const em = entityMeta(meta, entity);
  if (!em) return html`<div class="err">unknown entity ${entity}</div>`;
  if (error) return html`<div class="err">${error.message || 'error'}</div>`;
  if (loading) return html`<div class="muted">loading…</div>`;
  const cols = em.ui?.list_columns?.length ? em.ui.list_columns : em.fields.filter((f) => f.readable).slice(0, 6).map((f) => f.name);
  return html`<table><thead><tr>${cols.map((c) => { const f = em.fields.find((x) => x.name === c); return html`<th>${f?.label || c}</th>`; })}</tr></thead>
    <tbody>${records.map((r) => html`<tr onClick=${onRowClick ? () => onRowClick(r) : undefined} style=${onRowClick ? 'cursor:pointer' : ''}>
      ${cols.map((c) => {
        const f = em.fields.find((x) => x.name === c);
        return f ? html`<td><${FieldAtom} field=${f} value=${r.values[c]} /></td>` : html`<td>${r.values[c]}</td>`;
      })}
    </tr>`)}</tbody></table>
    ${records.length === 0 && html`<div class="muted" style="margin-top:8px">no records visible</div>`}`;
}

// KDetail: one record's readable fields + the workflow actions the actor may
// press (actionsFor, the shared permission derivation from hooks.js).
// Read-only (no inline edit) — a detail PANE, not the full edit form
// (web/app.js's RecordView layers editing + history + related on top).
export function KDetail({ api, meta, entity, id, onAct, onNavigate }) {
  const { record, loading, error } = useRecordData(api, entity, id);
  const em = entityMeta(meta, entity);
  if (!em) return html`<div class="err">unknown entity ${entity}</div>`;
  if (error) return html`<div class="err">${error.message || 'error'}</div>`;
  if (loading || !record) return html`<div class="muted">loading…</div>`;
  const state = em.workflow_field ? record.values[em.workflow_field] : null;
  const actions = actionsFor(em, record);
  return html`<div>
    <h3>${em.label || em.name} <span class="muted">${reclabel(record.values)}</span> ${state && html`<${StatusChip} value=${state} />`}</h3>
    ${actions.length > 0 && html`<div style="margin-bottom:10px;display:flex;gap:8px;flex-wrap:wrap">
      ${actions.map((a) => html`<button class="btn ${a.requires_approval ? '' : 'green'}"
        onClick=${onAct ? () => onAct(a.action) : undefined}>${a.label || humanize(a.action)}${a.requires_approval ? ' ✍' : ''}</button>`)}
    </div>`}
    <div class="card"><div style="display:grid;grid-template-columns:repeat(3,1fr);gap:2px 18px">
      ${em.fields.filter((f) => f.readable).map((f) => html`<div>
        <label>${f.label || f.name}</label>
        <div style="padding:4px 0 10px"><${FieldAtom} field=${f} value=${record.values[f.name]}
          onNavigate=${onNavigate} fetcher=${(e, rid) => api(`/api/records/${e}/${rid}`)} /></div>
      </div>`)}
    </div></div>
  </div>`;
}

// KBoard: a kanban grouped by the entity's board-by enum field, one
// RecordCard per record — mirrors web/app.js's Board and kalita-sdk's
// KBoard, but cards render through the shared RecordCard molecule.
export function KBoard({ api, meta, entity, onCardClick }) {
  const { records, loading } = useRecordsData(api, entity, { limit: 500 });
  const em = entityMeta(meta, entity);
  if (!em || !em.ui?.board_by) return html`<div class="muted">no board configured for ${entity}</div>`;
  if (loading) return html`<div class="muted">loading…</div>`;
  const field = em.fields.find((f) => f.name === em.ui.board_by);
  return html`<div class="cols">${(field?.values || []).map((v) => html`<div class="col">
    <h4>${v} · ${records.filter((r) => r.values[em.ui.board_by] === v).length}</h4>
    ${records.filter((r) => r.values[em.ui.board_by] === v).map((r) => html`<${RecordCard} ent=${em} record=${r}
      onClick=${onCardClick ? () => onCardClick(r) : undefined} />`)}
  </div>`)}</div>`;
}

// KInbox: the human work surface — pending signatures, proposals, role
// tasks. Signature cards use the ApprovalCard molecule (expand-to-preview
// included); proposals/tasks stay compact rows (they have no meta-driven
// field set to expand into).
export function KInbox({ api, meta, onDecide, onDecideProposal, onOpenRecord }) {
  const { approvals, proposals, tasks, loading, error, reload } = useInboxData(api);
  if (error) return html`<div class="err">${error.message || 'error'}</div>`;
  if (loading) return html`<div class="muted">loading…</div>`;
  const entOf = (a) => entityMeta(meta, a.entity) || { name: a.entity, fields: [] };
  return html`<div>
    <h3>Signatures requested · ${approvals.length}</h3>
    ${approvals.map((a) => html`<${ApprovalCard} a=${a} ent=${entOf(a)}
      onDecide=${(id, grant) => onDecide(id, grant).then(reload)}
      onOpenRecord=${onOpenRecord}
      fetchRecord=${(e, id) => api(`/api/records/${e}/${id}`)} />`)}
    <h3>Definition changes · ${proposals.length}</h3>
    ${proposals.map((p) => html`<div class="card">
      <b>${p.description || 'proposal'}</b> <span class="muted">by ${p.author.id}</span>
      <pre>${(p.plan || []).join('\n')}</pre>
      <button class="btn green" onClick=${() => onDecideProposal(p.id, true).then(reload)}>Sign & apply</button>
      <button class="btn red" onClick=${() => onDecideProposal(p.id, false).then(reload)}>Reject</button>
    </div>`)}
    <h3>My role's tasks · ${tasks.length}</h3>
    ${tasks.map((t) => html`<div class="card">
      <${StatusChip} value=${t.kind} /> ${t.action || ''}
      <div class="muted">${t.args || ''}</div></div>`)}
  </div>`;
}
