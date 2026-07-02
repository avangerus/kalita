// kalita-ui / molecules.js — LLD-05 PK-1 §1 "molecules" layer: small
// compositions of atoms, still zero domain knowledge (no entity names, no
// pack-specific fields). Preact+htm, same stack as atoms.js/web/app.js.
//
// FieldRow is the SINGLE canonical "meta field -> control" dispatcher
// (exported as FieldAtom from atoms.js and wrapped here with label/grid
// span). web/app.js's FieldInput/FieldValue delegate to FieldAtom directly
// for every type except the few genuinely-interactive search/browse pickers
// that have no atom equivalent (ref search+lookup, capability picker) — see
// the comment on FieldAtom in atoms.js for why those stay app.js-local.
import { html, useState, useEffect } from 'kalita-vendor';
import { formatValue, reclabel, humanize, fieldSpan, fmtPlain } from './tokens/format.js';
import { Badge, StatusChip, FieldAtom, renderSpec } from './atoms.js';

// --- FieldRow ------------------------------------------------------------------

// FieldRow: label + required/computed marker + the right atom, sized to
// occupy `span` (default: fieldSpan(field)) columns of a form/detail grid.
// Capability props (onNavigate/fetcher/uploader/suggestions) are forwarded
// to FieldAtom untouched — see atoms.js for what each does.
export function FieldRow({ field, value, onChange, onNavigate, fetcher, uploader, suggestions, span }) {
  const cols = span || fieldSpan(field);
  return html`<div style=${'grid-column:span ' + cols}>
    <label>${field.label || field.name}${field.required ? ' *' : ''}${field.computed ? ' (computed)' : ''}</label>
    ${onChange
      ? html`<${FieldAtom} field=${field} value=${value} onChange=${onChange}
          onNavigate=${onNavigate} fetcher=${fetcher} uploader=${uploader} suggestions=${suggestions} />`
      : html`<div style="padding:4px 0 10px;min-height:20px">
          <${FieldAtom} field=${field} value=${value} onNavigate=${onNavigate} fetcher=${fetcher} /></div>`}
  </div>`;
}

// --- RecordCard ------------------------------------------------------------------

// RecordCard: a compact record summary — reclabel() as the title, a handful
// of readable fields underneath. Used by list/board views (RecordCard is
// what a KBoard/kanban card or a search-result row renders).
export function RecordCard({ ent, record, columns, onClick }) {
  const cols = columns || (ent.ui?.list_columns?.length
    ? ent.ui.list_columns
    : ent.fields.filter((f) => f.readable).slice(0, 4).map((f) => f.name));
  return html`<div class="kcard" style=${onClick ? 'cursor:pointer' : ''} onClick=${onClick}>
    <div style="font-weight:600;margin-bottom:4px">${reclabel(record.values)}</div>
    <div style="display:flex;flex-wrap:wrap;gap:2px 14px;font-size:12px">
      ${cols.map((c) => {
        const f = ent.fields.find((x) => x.name === c);
        if (!f) return null;
        return html`<div><span class="muted">${f.label || f.name}:</span> ${renderSpec(formatValue(f, record.values[c]))}</div>`;
      })}
    </div>
  </div>`;
}

// --- ApprovalCard ------------------------------------------------------------------

// ApprovalCard: a signature request with "what am I signing?" — the record's
// text work-product in full plus a compact field summary, so a human signs
// informed, not blind. Generalized off web/app.js's original (which
// hardcoded `api`/`nav`): network and navigation are injected.
//   fetchRecord(entity, id) -> Promise<{values}>       required to expand
//   onOpenRecord(entity, id)                            optional, "open record" link
//   renderRelated({ent, id}) -> VNode | null             optional render-prop,
//     e.g. web/app.js's RelatedPanel (children/findings under the record) —
//     kept as a render-prop instead of a hard import so this molecule stays
//     free of any specific "related panel" implementation (ADR-003 slot style).
export function ApprovalCard({ a, ent, onDecide, fetchRecord, onOpenRecord, renderRelated }) {
  const [open, setOpen] = useState(false);
  const [rec, setRec] = useState(null);
  useEffect(() => {
    if (open && !rec && fetchRecord) fetchRecord(a.entity, a.record_id).then(setRec).catch(() => {});
  }, [open, a.entity, a.record_id]);
  const fields = (ent.fields || []).filter((f) => f.readable !== false);
  const textFields = fields.filter((f) => f.type === 'text' && rec?.values?.[f.name]);
  const scalarFields = fields.filter((f) => f.type !== 'text' && rec?.values?.[f.name] != null && rec.values[f.name] !== '');
  return html`<div class="card">
    <b>${humanize(a.action)}</b>: ${ent.label || ent.name} <${Badge}>${a.from} → ${a.to}</${Badge}>
    <div class="muted">requested by ${a.requested_by.id} (${a.requested_by.type})
      ${onOpenRecord && html` · <a onClick=${() => onOpenRecord(a.entity, a.record_id)}>open record</a>`}</div>
    <div style="margin-top:8px;display:flex;gap:8px;align-items:center;flex-wrap:wrap">
      <button class="btn green" onClick=${() => onDecide(a.id, true)}>Approve ✍</button>
      <button class="btn red" onClick=${() => onDecide(a.id, false)}>Reject</button>
      <button class="btn ${open ? 'on' : ''}" onClick=${() => setOpen(!open)}>${open ? 'hide' : '👁 what am I signing?'}</button>
    </div>
    ${open && html`<div style="margin-top:10px;border-top:1px solid var(--line);padding-top:10px">
      ${!rec ? html`<div class="muted">loading preview…</div>` : html`
        ${textFields.map((f) => html`<div style="margin-bottom:8px">
          <div class="muted" style="font-size:12px">${f.label || f.name} — the work to sign off:</div>
          <div style="white-space:pre-wrap;font-size:13px;max-height:260px;overflow:auto;background:var(--input-bg,#161b22);padding:8px;border-radius:var(--radius,6px)">${rec.values[f.name]}</div></div>`)}
        <div style="display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:2px 18px">
          ${scalarFields.map((f) => html`<div style="font-size:13px;padding:2px 0">
            <span class="muted">${f.label || f.name}:</span> ${renderSpec(formatValue(f, rec.values[f.name]))}</div>`)}
        </div>
        ${renderRelated ? renderRelated({ ent, id: a.record_id }) : null}`}
    </div>`}
  </div>`;
}

// --- TimelineItem ------------------------------------------------------------------

// kindLabel/changeLines/isMutation: journal-event -> human-readable summary.
// Generalized off web/app.js's Timeline (was inline there); pure functions,
// exported so any face can build its own timeline layout around them.
const KIND_LABEL = {
  'record.created': 'created', 'record.updated': 'edited', 'record.action': 'transition',
  'comment.posted': 'commented', 'task.created': 'task queued', 'task.taken': 'task taken',
  'task.completed': 'task done', 'approval.requested': 'sent for signature',
  'approval.granted': 'signed', 'approval.rejected': 'rejected',
};
export function kindLabel(k) { return KIND_LABEL[k] || k; }

export function isMutation(k) {
  return k === 'record.created' || k === 'record.updated' || k === 'record.action';
}

export function changeLines(e) {
  const p = e.payload || {};
  if (e.kind === 'record.created') return ['created'];
  if (e.kind === 'record.updated') return (p.changes || []).map((c) => `${c.field}: ${fmtPlain(c.old) || '∅'} → ${fmtPlain(c.new) || '∅'}`);
  if (e.kind === 'record.action') return [`${p.from || '∅'} → ${p.to || '∅'}${p.action ? ` (${p.action})` : ''}`];
  return [];
}

// TimelineItem: one journal-event row — who, what, when, and (for a
// supervisor) a "rewind to here" button. `lastMutSeq` is the seq of the most
// recent mutation (rewind is only offered for earlier ones, matching the
// original app.js Timeline behavior exactly).
export function TimelineItem({ e, canRevert, busy, onRewind, lastMutSeq }) {
  const lines = changeLines(e);
  const undone = e.basis && (e.basis.type === 'revert' || e.basis.type === 'rewind');
  return html`<div style="padding:6px 0;border-bottom:1px solid var(--line);display:flex;gap:10px;align-items:flex-start">
    <span class="muted" style="font-size:11px;width:118px;flex:none">${(e.ts || '').slice(0, 16).replace('T', ' ')}</span>
    <div style="flex:1">
      <${StatusChip} value=${kindLabel(e.kind)} tone=${undone ? 'bad' : 'default'} />
      <b> ${e.actor.id}</b> <span class="muted">(${e.actor.type})</span>
      ${undone && html`<span class="muted"> · ${e.basis.type} of #${e.basis.id}</span>`}
      ${lines.map((l) => html`<div style="font-size:13px;margin-top:2px">${l}</div>`)}
    </div>
    ${canRevert && isMutation(e.kind) && e.seq < lastMutSeq && html`
      <button class="btn" style="flex:none;font-size:12px;padding:3px 8px" disabled=${busy}
        onClick=${() => onRewind(e.seq)} title="restore the record to its state at this point">⤺ rewind here</button>`}
  </div>`;
}

// --- DashboardTile ------------------------------------------------------------------

// DashboardTile: table-wide aggregate — a big number, or a grouped bar list.
// Generalized off web/app.js's Tile (identical behavior, moved here).
export function DashboardTile({ t }) {
  const grouped = t.groups && t.groups.length;
  const max = grouped ? Math.max(...t.groups.map((g) => g.value), 1) : 0;
  return html`<div class="card" style=${grouped ? 'flex:1 1 320px' : 'flex:0 0 200px'}>
    <div class="muted" style="font-size:12px;margin-bottom:6px">${t.label}</div>
    ${grouped
      ? html`<div>${t.groups.map((g) => html`<div style="display:flex;align-items:center;gap:8px;margin:4px 0">
          <span style="width:96px;font-size:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${g.key}</span>
          <span style="flex:1;background:#1f2630;border-radius:var(--radius-sm,4px);height:14px;position:relative">
            <span style="position:absolute;inset:0 auto 0 0;width:${Math.round(g.value / max * 100)}%;background:var(--acc);border-radius:var(--radius-sm,4px)"></span></span>
          <b style="width:32px;text-align:right">${g.value}</b></div>`)}</div>`
      : html`<div style="font-size:30px;font-weight:600">${t.value}</div>`}
  </div>`;
}
