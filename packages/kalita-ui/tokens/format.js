// kalita-ui / tokens/format.js — framework-agnostic value-formatting core.
//
// WHY THIS FILE EXISTS (LLD-05 PR-1, point 2 of the implementing brief):
// kalita-ui's atoms (preact+htm) and @kalita/sdk's organisms
// (packages/kalita-sdk/components.js, React) are two DIFFERENT rendering
// stacks that must not be merged (ADR-003 froze the SDK's React API; the
// embedded admin client is preact+htm per ADR-004 and stays that way). Both
// stacks still need to answer the exact same question — "given this
// MetaField and this value, what should the UI show?" — money grouping,
// percent suffix, datetime truncation, ref-array summaries, etc.
//
// Rather than reimplement that type switch twice (and drift), this module is
// the ONE source of truth: plain data-in/data-out functions, ZERO h()/html()/
// createElement calls. Each rendering stack turns a FormatSpec into its own
// elements (see atoms.js's `renderSpec` for preact+htm, components.js's
// `renderSpec` for React). This is a deliberate, reasoned split — not a
// shortcut — documented here per the implementing brief's instruction.
//
// Pure JS, no dependencies, ESM only (buildless — ADR-004).

/** fmtNumber: thousands-grouped number, or pass through non-numbers as-is. */
export function fmtNumber(n) {
  return typeof n === 'number' ? n.toLocaleString('en-US') : n;
}

/** fmtPlain: the untyped fallback — objects as JSON, everything else as string. */
export function fmtPlain(v) {
  if (v === null || v === undefined) return '';
  if (typeof v === 'object') return JSON.stringify(v);
  return String(v);
}

/** fmtDateTime: RFC3339 -> readable wall-clock "YYYY-MM-DD HH:MM". */
export function fmtDateTime(v) {
  return typeof v === 'string' ? v.slice(0, 16).replace('T', ' ') : fmtPlain(v);
}

/** fmtDate: RFC3339 or date string -> "YYYY-MM-DD". */
export function fmtDate(v) {
  return typeof v === 'string' ? v.slice(0, 10) : fmtPlain(v);
}

/** fmtMoney: scalar number OR {amount, currency} object -> "12,345 USD". */
export function fmtMoney(v) {
  if (v === null || v === undefined || v === '') return '';
  if (typeof v === 'object') return `${fmtNumber(v.amount)} ${v.currency || ''}`.trim();
  return String(fmtNumber(v));
}

/** fmtPercent: 12 -> "12%". */
export function fmtPercent(v) {
  return (v === null || v === undefined || v === '') ? '' : `${v}%`;
}

/** isEmpty: the shared "render a dash instead" predicate. */
export function isEmpty(v) {
  return v === null || v === undefined || v === '' || (Array.isArray(v) && v.length === 0);
}

// FormatSpec — a tiny tagged union describing HOW to render a value, without
// committing to a rendering stack:
//   { kind: 'empty' }
//   { kind: 'text', text }
//   { kind: 'link', href, text }
//   { kind: 'color', value }               // hex string
//   { kind: 'file', file }                 // {name, size, hash, ...} | null
//   { kind: 'files', files }               // array of file objects
//   { kind: 'chips', items }               // array of plain strings
//   { kind: 'ref', ref, id }               // needs async label resolution
//   { kind: 'array_ref', ref, ids }        // needs async label resolution (each)
//
// formatValue: the single type-dispatch every consumer calls instead of
// re-deriving field.type branches. Consumers with an id->label resolver
// (RefLink et al.) handle 'ref'/'array_ref' themselves; everything else is
// ready to print as-is.
export function formatValue(field, value) {
  if (isEmpty(value)) return { kind: 'empty' };
  const type = field && field.type;
  switch (type) {
    case 'money': return { kind: 'text', text: fmtMoney(value) };
    case 'percent': return { kind: 'text', text: fmtPercent(value) };
    case 'decimal': return { kind: 'text', text: fmtNumber(value) };
    case 'datetime': return { kind: 'text', text: fmtDateTime(value) };
    case 'date': return { kind: 'text', text: fmtDate(value) };
    case 'bool': return { kind: 'text', text: value ? 'true' : 'false' };
    case 'color': return { kind: 'color', value };
    case 'url': return { kind: 'link', href: value, text: value };
    case 'file': return { kind: 'file', file: value };
    case 'array_file': return { kind: 'files', files: Array.isArray(value) ? value : [] };
    case 'tags':
    case 'multiselect': return { kind: 'chips', items: Array.isArray(value) ? value : [fmtPlain(value)] };
    case 'ref': return { kind: 'ref', ref: field.ref, id: value };
    case 'array_ref': return { kind: 'array_ref', ref: field.ref, ids: Array.isArray(value) ? value : [] };
    default: return { kind: 'text', text: fmtPlain(value) };
  }
}

// reclabel: a short human label for one record — prefers a name-like field
// (so a picker reads "Rich types demo", not the #hex color that happened to
// sort first), else the first plain string value, else a truncated id.
// Shared so list rows, ref pickers and ref-label resolution agree on the
// same label everywhere (app.js, kalita-ui atoms, kalita-sdk organisms).
const LABEL_KEYS = ['name', 'title', 'subject', 'full_name', 'label', 'number', 'company'];
const UUID_RE = /^[0-9a-f-]{36}$/;
const isPlainStr = (v) => typeof v === 'string' && v !== '' && !UUID_RE.test(v);
export function reclabel(values) {
  values = values || {};
  for (const k of LABEL_KEYS) if (isPlainStr(values[k])) return values[k];
  for (const k in values) if (isPlainStr(values[k])) return values[k];
  return (values.id || '').slice(0, 8) || '—';
}

/** humanize: workflow action identifier -> button label ("close_incident" -> "Close incident"). */
export function humanize(s) {
  return String(s || '').replace(/_/g, ' ').replace(/^./, (c) => c.toUpperCase());
}

/** fieldSpan: how many of a 3-column form grid a field's control should occupy. */
export function fieldSpan(f) {
  if (f.picker) return 2;
  if (['text', 'json', 'array_file', 'array_ref'].includes(f.type)) return 3;
  if (['ref', 'tags', 'multiselect', 'url'].includes(f.type)) return 2;
  return 1;
}
