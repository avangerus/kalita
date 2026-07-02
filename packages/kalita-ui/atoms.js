// kalita-ui / atoms.js — the bottom layer of the Product Kit design system
// (LLD-05 PK-1 §1). Preact+htm, matching web/app.js's rendering stack
// (ADR-004: the embedded admin renderer is preact+htm, not React — do not
// confuse these with packages/kalita-sdk's React organisms, a different
// stack consuming the SAME packages/kalita-ui/tokens/format.js core).
//
// Every atom takes a field-descriptor-shaped prop (+ value [+ onChange]) and
// nothing pack-specific: zero entity names, zero domain knowledge. An atom
// renders EDIT mode when `onChange` is supplied, READ mode otherwise — this
// keeps FieldInput/FieldValue in web/app.js as thin dispatchers (per the LLD:
// "те остаются ДИСПЕТЧЕРОМ ... логика типов не дублируется") without forcing
// two separate atoms per type for the common case.
//
// Network capability injection: some atoms need I/O (RefLink resolves a ref
// id to a label; FileAttachment uploads a file). Atoms never import app.js's
// authenticated `api()` — that would reintroduce domain/session coupling.
// Instead callers pass a `fetcher`/`uploader` function prop; atoms default to
// a plain unauthenticated `fetch` so they still work standalone (catalog.html
// injects a fixture-backed fetcher instead). This is the deliberate boundary:
// atoms stay presentational + capability-injected, never session-aware.
//
// Vendor import: preact+htm comes through the bare specifier "kalita-vendor",
// resolved by an <script type="importmap"> in the consuming HTML page (see
// web/index.html and web/catalog.html) to vendor/preact-standalone.module.js
// relative to THAT page. This indirection is what lets the exact same source
// file work when opened straight off disk (file://), served from disk in dev
// (`kalita serve --ui-dir ./web`), and served from the embedded single-binary
// build — three different URL/filesystem topologies that a plain relative
// import could not satisfy at once (packages/kalita-ui sits outside web/, so
// a relative path back into web/vendor resolves differently under each
// topology; a bare specifier resolved per-page does not). See cmd/kalita/main.go
// for the matching `/packages/` static mount added for the disk-serving case.
import { html, useState, useEffect } from 'kalita-vendor';
import { formatValue, isEmpty, reclabel as reclabelImpl } from './tokens/format.js';

// --- shared bits -------------------------------------------------------------

const Dash = () => html`<span class="muted">—</span>`;

// renderSpec: turn a tokens/format.js FormatSpec into preact+htm output. The
// 'ref'/'array_ref' kinds are NOT handled here (they need async resolution —
// use RefLink for those); every other kind is ready to print.
export function renderSpec(spec) {
  switch (spec.kind) {
    case 'empty': return html`<${Dash} />`;
    case 'text': return spec.text === '' ? html`<${Dash} />` : html`${spec.text}`;
    case 'link': return html`<a href=${spec.href} target="_blank" rel="noreferrer noopener">${spec.text}</a>`;
    case 'color': return html`<span style="display:inline-flex;align-items:center;gap:6px">
      <span style="width:13px;height:13px;border-radius:var(--radius-sm,3px);border:1px solid var(--line);background:${spec.value}"></span>${spec.value}</span>`;
    case 'file': return spec.file ? html`<span>📄 ${spec.file.name || 'file'}</span>` : html`<${Dash} />`;
    case 'files': return spec.files.length
      ? html`${spec.files.map((f) => html`<${Badge}>📄 ${f?.name || 'file'}</${Badge}>`)}`
      : html`<${Dash} />`;
    case 'chips': return spec.items.length ? html`${spec.items.map((t) => html`<${Badge}>${t}</${Badge}>`)}` : html`<${Dash} />`;
    default: return html`<${Dash} />`;
  }
}

// --- Badge / StatusChip -------------------------------------------------------

// Badge: the generic small pill — chips, counts, file names. tone picks a
// background from the token palette; 'default' is the neutral pill used
// throughout the admin client today.
const TONE_BG = {
  default: '#223041',
  ok: 'var(--ok-soft, #15301f)',
  warn: 'var(--warn-soft, #2a2013)',
  bad: 'var(--bad-soft, #33201c)',
  dim: 'transparent',
};
export function Badge({ children, tone = 'default', style = '' }) {
  return html`<span class="pill" style=${`margin:0 4px 2px 0;display:inline-block;background:${TONE_BG[tone] || TONE_BG.default};${style}`}>${children}</span>`;
}

// StatusChip: a semantic pill for workflow state / actor status / anything
// with an implicit tone. Pass `tone` explicitly when known (ok/warn/bad); a
// bare `value` with no tone renders as the neutral default pill (matches the
// plain `<span class="pill">` used for workflow state today).
export function StatusChip({ value, tone = 'default' }) {
  if (isEmpty(value)) return html`<${Dash} />`;
  return html`<${Badge} tone=${tone}>${value}</${Badge}>`;
}

// --- Bool ----------------------------------------------------------------------

// Bool: tri-state (true / false / unset). Edit mode is a <select> (matches
// the rest of this admin UI's "select, don't type" convention); read mode
// prints the formatted text.
export function Bool({ field, value, onChange }) {
  if (!onChange) {
    const spec = formatValue(field, value);
    return renderSpec(spec);
  }
  return html`<select value=${String(value ?? '')} onChange=${(e) => onChange(e.target.value === '' ? null : e.target.value === 'true')}>
    <option value="">—</option><option value="true">true</option><option value="false">false</option>
  </select>`;
}

// --- Select / EnumSelect / MultiSelect (Tags) -----------------------------------

// Select: generic single-choice dropdown. `options` is an array of plain
// strings or {value,label} pairs; defaults to field.values (enum fields).
export function Select({ field, value, onChange, options, placeholder = '—' }) {
  const opts = (options || field?.values || []).map((o) => (typeof o === 'object' ? o : { value: o, label: o }));
  if (!onChange) {
    return isEmpty(value) ? html`<${Dash} />` : html`${value}`;
  }
  return html`<select value=${value || ''} onChange=${(e) => onChange(e.target.value)}>
    <option value="">${placeholder}</option>${opts.map((o) => html`<option value=${o.value}>${o.label}</option>`)}
  </select>`;
}

// EnumSelect: Select bound to field.values — the common case for field.type === 'enum'.
export function EnumSelect(props) {
  return Select(props);
}

// MultiSelect (aka Tags): add/remove chips. `options` (multiselect: a closed
// list from field.values) restricts input to a dropdown of remaining
// choices; without `options` (tags: free text) a text box + Enter adds a new
// chip. `suggestions` (optional) offers previously-used free values — the
// atom does NOT fetch these itself (see file header: no network in atoms);
// the caller fetches and passes them in.
export function MultiSelect({ field, value, onChange, options, suggestions }) {
  const list = Array.isArray(value) ? value : [];
  const [draft, setDraft] = useState('');
  if (!onChange) {
    return list.length ? html`${list.map((t) => html`<${Badge}>${t}</${Badge}>`)}` : html`<${Dash} />`;
  }
  const opts = (options || field?.values || suggestions || []).filter((o) => !list.includes(o));
  const add = (t) => { t = (t || '').trim(); if (t && !list.includes(t)) onChange([...list, t]); setDraft(''); };
  return html`<div style="margin:3px 0 10px">
    <div>${list.map((t) => html`<${Badge}>${t}
      <span style="cursor:pointer;color:var(--dim)" onClick=${() => onChange(list.filter((x) => x !== t))}> ✕</span></${Badge}>`)}</div>
    <div style="display:flex;gap:8px">
      ${opts.length > 0 && html`<select style="margin:0;max-width:220px" value="" onChange=${(e) => e.target.value && add(e.target.value)}>
        <option value="">choose existing…</option>${opts.map((o) => html`<option value=${o}>${o}</option>`)}</select>`}
      ${!(options || field?.values) && html`<input style="margin:0" placeholder="or add new, Enter" value=${draft}
        onInput=${(e) => setDraft(e.target.value)} onKeyDown=${(e) => e.key === 'Enter' && (e.preventDefault(), add(draft))} />`}
    </div>
  </div>`;
}

// --- Money / DateTime -----------------------------------------------------------

// Money: the money/percent/decimal numeric family. Edit renders a plain
// number input (the currency of a {amount,currency} money value is NOT
// editable here — packs that need currency selection compose their own
// molecule); read renders the type-formatted text (grouped, %, currency).
export function Money({ field, value, onChange }) {
  if (!onChange) return renderSpec(formatValue(field, value));
  const amount = value && typeof value === 'object' ? value.amount : value;
  const setAmount = (n) => {
    if (value && typeof value === 'object') onChange({ ...value, amount: n });
    else onChange(n);
  };
  return html`<input type="number" value=${amount ?? ''} onInput=${(e) => setAmount(e.target.value === '' ? null : Number(e.target.value))} />`;
}

// DateTime: date/datetime family. Edit uses the native picker and stores
// RFC3339 (engine format); read renders the truncated wall-clock string.
export function DateTime({ field, value, onChange }) {
  if (!onChange) return renderSpec(formatValue(field, value));
  if (field?.type === 'date') {
    return html`<input type="date" value=${value || ''} onInput=${(e) => onChange(e.target.value)} />`;
  }
  const local = value ? String(value).slice(0, 16) : '';
  return html`<input type="datetime-local" value=${local}
    onInput=${(e) => { const v = e.target.value; onChange(v ? (v.length === 16 ? v + ':00Z' : v + 'Z') : ''); }} />`;
}

// --- ColorPicker -----------------------------------------------------------------

export function ColorPicker({ value, onChange }) {
  if (!onChange) return renderSpec(formatValue({ type: 'color' }, value));
  return html`<input type="color" value=${value || '#000000'} onInput=${(e) => onChange(e.target.value)}
    style="width:56px;height:34px;padding:2px;cursor:pointer" />`;
}

// --- Text (long text / json) ------------------------------------------------------

export function Text({ field, value, onChange }) {
  if (!onChange) return renderSpec(formatValue(field, value));
  const isJson = field?.type === 'json';
  return html`<textarea rows=${isJson ? 4 : 3}
    value=${typeof value === 'object' && value !== null ? JSON.stringify(value, null, 2) : (value || '')}
    onInput=${(e) => onChange(e.target.value)} />`;
}

// --- Input (generic scalar: string/int/float/email/url/phone) ------------------

// Input: the catch-all scalar atom — string, int, float, email, url, phone.
// Edit mode picks the right native <input type>; read mode formats via the
// shared core (url becomes a clickable link).
export function Input({ field, value, onChange }) {
  if (!onChange) return renderSpec(formatValue(field, value));
  const type = field?.type;
  if (type === 'email' || type === 'url' || type === 'phone') {
    return html`<input type=${type === 'phone' ? 'tel' : type} value=${value || ''} onInput=${(e) => onChange(e.target.value)} />`;
  }
  const numeric = ['int', 'float'].includes(type);
  return html`<input type=${numeric ? 'number' : 'text'} value=${value ?? ''}
    onInput=${(e) => onChange(numeric ? Number(e.target.value) : e.target.value)} />`;
}

// --- FileAttachment (file / array_file) -------------------------------------------

// FileAttachment: read-only display always works with no network. Edit mode
// (drag/drop or click-to-choose) needs an `uploader` — an injected
// `(File) => Promise<{name,size,hash,...}>` — because uploading is
// inherently an authenticated network call the atom must not own itself.
// Without `uploader`, edit mode degrades to read-only display (fail safe,
// not fail silent-and-broken).
export function FileAttachment({ field, value, onChange, uploader, multi }) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState(null);
  if (multi) {
    const list = Array.isArray(value) ? value : [];
    if (!onChange || !uploader) return renderSpec(formatValue({ type: 'array_file' }, list));
    return html`<div style="margin:3px 0 10px">
      ${list.map((f, i) => html`<${Badge}>📄 ${f?.name || 'file'}
        <span style="cursor:pointer;color:var(--dim)" onClick=${() => onChange(list.filter((_, j) => j !== i))}> ✕</span></${Badge}>`)}
      <${FileAttachment} field=${{ type: 'file' }} value=${null} uploader=${uploader}
        onChange=${(f) => f && onChange([...list, f])} />
    </div>`;
  }
  if (!onChange || !uploader) return renderSpec(formatValue({ type: 'file' }, value));
  const upload = async (file) => {
    if (!file) return;
    setBusy(true); setErr(null);
    try { onChange(await uploader(file)); } catch (e) { setErr(e?.message || 'upload failed'); }
    setBusy(false);
  };
  const inputId = 'kui-file-' + (value?.hash || Math.random().toString(36).slice(2));
  return html`<div
      onDragOver=${(e) => e.preventDefault()}
      onDrop=${(e) => { e.preventDefault(); upload(e.dataTransfer.files[0]); }}
      onClick=${() => document.getElementById(inputId)?.click()}
      style="border:1px dashed var(--line);border-radius:var(--radius,6px);padding:12px;text-align:center;margin:3px 0 10px;cursor:pointer">
    ${busy ? html`<span class="muted">loading…</span>`
      : value?.name ? html`<span>📄 ${value.name} <span class="muted">(${Math.round((value.size || 0) / 1024)} KB)</span></span>`
      : html`<span class="muted">drop a file here or click to choose</span>`}
    <input id=${inputId} type="file" style="display:none" onChange=${(e) => upload(e.target.files[0])} />
    ${err && html`<div class="err">${err}</div>`}
  </div>`;
}

// --- RefLink -----------------------------------------------------------------------

// Module-level label cache — same intent as web/app.js's former `_refCache`:
// one fetch per (entity,id) even across a whole table column of repeats.
const _refCache = new Map();

const defaultFetcher = (entity, id) => fetch(`/api/records/${entity}/${id}`).then((r) => r.json());

function useRefLabel(entity, id, fetcher) {
  const key = entity + '/' + id;
  const [text, setText] = useState(() => (id ? (_refCache.get(key) ?? null) : null));
  useEffect(() => {
    if (!id) { setText(null); return; }
    if (_refCache.has(key)) { setText(_refCache.get(key)); return; }
    (fetcher || defaultFetcher)(entity, id)
      .then((r) => { const l = reclabelFallback(r); _refCache.set(key, l); setText(l); })
      .catch(() => { const l = String(id).slice(0, 8); _refCache.set(key, l); setText(l); });
  }, [entity, id]);
  return text;
}

// reclabelFallback: /api/records/<e>/<id> returns {id, values, ...}; unwrap
// to the shared reclabel(values) so callers of useRefLabel don't need to
// know the response shape.
function reclabelFallback(rec) {
  return reclabelImpl(rec?.values || {});
}

// RefLink: resolve a ref id (or, with `multi`, an array of ids for
// array_ref) to its target record's label, and render it — a plain span, or
// a clickable link when `onNavigate(entity, id)` is supplied. This is the
// ONE place that knows how to turn a ref into a human label; RefInput's
// SEARCH/PICK UI (web/app.js) is a different, stateful concern and stays
// there (an atom that both fetches-as-you-type AND opens a lookup modal is
// no longer "domain-free presentational" — it is app shell UX).
export function RefLink({ field, value, onNavigate, fetcher, multi }) {
  if (multi) {
    const ids = Array.isArray(value) ? value : [];
    if (!ids.length) return html`<${Dash} />`;
    return html`${ids.map((id) => html`<${Badge}><${RefLinkOne} field=${field} value=${id} onNavigate=${onNavigate} fetcher=${fetcher} /></${Badge}>`)}`;
  }
  return html`<${RefLinkOne} field=${field} value=${value} onNavigate=${onNavigate} fetcher=${fetcher} />`;
}

function RefLinkOne({ field, value, onNavigate, fetcher }) {
  const text = useRefLabel(field.ref, value, fetcher);
  if (!value) return html`<${Dash} />`;
  if (!text) return html`<span class="muted">…</span>`;
  if (onNavigate) return html`<a onClick=${() => onNavigate(field.ref, value)}>${text}</a>`;
  return html`${text}`;
}

// --- FieldAtom (the canonical field-type dispatcher) --------------------------------

// FieldAtom: given ANY MetaField, render the right atom in EDIT mode
// (onChange supplied) or READ mode (omitted) — the single "type switch"
// every consumer shares (molecules.FieldRow, web/app.js's FieldValue, and
// most of web/app.js's FieldInput). This IS the "one source of render-type
// logic" the LLD requires; do not re-derive this switch anywhere else.
//
// Three field types are DELIBERATELY absent from this switch even though
// meta describes them: 'ref' edit mode (needs a live search+lookup-modal
// picker), 'array_ref' edit mode (same, plural), and field.picker (a
// capability browser — tree/list UI driven by a connector response). Those
// need stateful, multi-step interactive UI beyond "value + onChange" and
// have no atom home; web/app.js's RefInput/ArrayRefInput/CapabilityPicker
// remain the app-shell-local components for them (see FieldInput there).
// FieldAtom DOES handle 'ref'/'array_ref' in READ mode, via RefLink.
export function FieldAtom({ field, value, onChange, onNavigate, fetcher, uploader, suggestions }) {
  const type = field?.type;
  if (type === 'ref') return html`<${RefLink} field=${field} value=${value} onNavigate=${onNavigate} fetcher=${fetcher} />`;
  if (type === 'array_ref') return html`<${RefLink} field=${field} value=${value} multi onNavigate=${onNavigate} fetcher=${fetcher} />`;
  if (type === 'file') return html`<${FileAttachment} field=${field} value=${value} onChange=${onChange} uploader=${uploader} />`;
  if (type === 'array_file') return html`<${FileAttachment} field=${field} value=${value} onChange=${onChange} uploader=${uploader} multi />`;
  if (type === 'tags') return html`<${MultiSelect} field=${field} value=${value} onChange=${onChange} suggestions=${suggestions} />`;
  if (type === 'multiselect') return html`<${MultiSelect} field=${field} value=${value} onChange=${onChange} />`;
  if (type === 'enum') return html`<${EnumSelect} field=${field} value=${value} onChange=${onChange} />`;
  if (type === 'bool') return html`<${Bool} field=${field} value=${value} onChange=${onChange} />`;
  if (type === 'color') return html`<${ColorPicker} value=${value} onChange=${onChange} />`;
  if (type === 'text' || type === 'json') return html`<${Text} field=${field} value=${value} onChange=${onChange} />`;
  if (type === 'money' || type === 'percent' || type === 'decimal') return html`<${Money} field=${field} value=${value} onChange=${onChange} />`;
  if (type === 'datetime' || type === 'date') return html`<${DateTime} field=${field} value=${value} onChange=${onChange} />`;
  return html`<${Input} field=${field} value=${value} onChange=${onChange} />`;
}
