// @kalita/sdk/components — notation-driven React components. Each one reads
// /api/meta and renders itself (columns, types, permissions, actions, view
// config); the host site supplies the look via className and render-props.
// Drop <KList entity="Deal"/> into your own design and get a permission-aware
// table with zero logic — "the theme paints, the kernel + notation fill in".
//
// Unstyled by default: pass classNames or a `render` prop to match your design.

import { createElement as h, Fragment } from 'react';
import { useMeta, useRecords, useRecord, useInbox } from './react.js';
// Shared value-formatting core (LLD-05 PR-1, point 2 of the implementing
// brief): kalita-sdk is React, kalita-ui (packages/kalita-ui) is preact+htm —
// two DIFFERENT rendering stacks that stay separate (ADR-003 froze THIS
// package's public API: KList/KDetail/KBoard/KInbox signatures below are
// unchanged). Both stacks still need the identical answer to "how should
// this MetaField's value look" (money grouping, percent, datetime, ...), so
// packages/kalita-ui/tokens/format.js is the ONE source of truth: plain
// data-in/data-out functions, no h()/html()/createElement calls, safe to
// import from either stack. See that file's header for the full rationale,
// and packages/kalita-ui/atoms.js's `renderSpec` for the preact-side twin of
// the `renderSpec` below.
//
// Import path is relative, not a package specifier: this repo has no build
// step (ADR-004) and both packages sit side by side under packages/, so any
// bundler or Node's ESM loader resolves this natively. A future npm publish
// of @kalita/sdk as a standalone build artifact would need `@kalita/ui` as a
// real dependency (or this one file copied/inlined at publish time) — not
// needed today since neither package goes through a publish/bundle step.
import { formatValue, fmtPlain } from '../kalita-ui/tokens/format.js';

// entityMeta picks one entity's notation out of the meta payload.
function entityMeta(meta, entity) {
  return meta?.entities?.find((e) => e.name === entity) || null;
}

// renderSpec: turn a tokens/format.js FormatSpec into a React child — the
// React-stack twin of atoms.js's `renderSpec` (same FormatSpec shape,
// different element factory). 'ref'/'array_ref' are not resolved to a label
// here (that needs an id->record fetch; KList/KDetail/KBoard render a table
// of raw values today, same as before this integration — see TECHDEBT.md
// for the follow-up to give the SDK's organisms ref-label resolution).
function renderSpec(spec) {
  switch (spec.kind) {
    case 'empty': return '';
    case 'text': return spec.text;
    case 'link': return h('a', { href: spec.href, target: '_blank', rel: 'noreferrer noopener' }, spec.text);
    case 'color': return h(Fragment, null,
      h('span', { style: { display: 'inline-block', width: 10, height: 10, marginRight: 6, background: spec.value, borderRadius: 2 } }),
      spec.value);
    case 'file': return spec.file ? `📄 ${spec.file.name || 'file'}` : '';
    case 'files': return spec.files.map((f) => `📄 ${f?.name || 'file'}`).join(', ');
    case 'chips': return spec.items.join(', ');
    case 'ref': return fmtPlain(spec.id);
    case 'array_ref': return `${spec.ids.length} linked`;
    default: return '';
  }
}

// fieldFmt: format one column's value using its meta field descriptor, via
// the shared formatValue() core, when the descriptor is known; falls back to
// the old untyped `fmt()` otherwise (unlabeled columns, e.g. KBoard's
// title column is just "the first list column", not guaranteed to resolve).
function fieldFmt(em, name, value) {
  const f = em?.fields?.find((x) => x.name === name);
  if (!f) return fmt(value);
  return renderSpec(formatValue(f, value));
}

// KList: a permission-aware table. Columns come from the entity's ui.list or
// its readable fields. classes lets the host theme every part; render(row)
// overrides row rendering entirely.
export function KList({ entity, classes = {}, onRowClick, render }) {
  const { data: meta } = useMeta();
  const { records, loading, error } = useRecords(entity);
  const em = entityMeta(meta, entity);
  if (error) return h('div', { className: classes.error }, error.message || 'error');
  if (!meta || loading) return h('div', { className: classes.loading }, '…');
  if (!em) return h('div', { className: classes.error }, `unknown entity ${entity}`);

  const cols = (em.ui?.list_columns?.length
    ? em.ui.list_columns
    : em.fields.filter((f) => f.readable).slice(0, 6).map((f) => f.name));

  if (render) return h(Fragment, null, records.map((r) => render(r)));

  return h('table', { className: classes.table },
    h('thead', { className: classes.thead },
      h('tr', null, cols.map((c) => h('th', { key: c, className: classes.th }, c)))),
    h('tbody', { className: classes.tbody },
      records.map((r) => h('tr', {
        key: r.id, className: classes.tr,
        onClick: onRowClick ? () => onRowClick(r) : undefined,
      }, cols.map((c) => h('td', { key: c, className: classes.td }, fieldFmt(em, c, r.values[c])))))));
}

// KDetail: one record's fields, grouped, with the workflow actions the actor
// may press (notation decides which buttons exist).
export function KDetail({ entity, id, classes = {}, onAct }) {
  const { data: meta } = useMeta();
  const { record, loading, error } = useRecord(entity, id);
  const em = entityMeta(meta, entity);
  if (error) return h('div', { className: classes.error }, error.message);
  if (!meta || loading || !em) return h('div', { className: classes.loading }, '…');

  const state = em.workflow_field ? record?.values?.[em.workflow_field] : null;
  const actions = (em.actions || []).filter((a) => a.can_act && (a.from === state || a.from === 'any'));

  return h('div', { className: classes.detail },
    h('div', { className: classes.fields },
      em.fields.filter((f) => f.readable).map((f) =>
        h('div', { key: f.name, className: classes.field },
          h('label', { className: classes.label }, f.name),
          h('span', { className: classes.value }, fieldFmt(em, f.name, record?.values?.[f.name]))))),
    actions.length > 0 && h('div', { className: classes.actions },
      actions.map((a) => h('button', {
        key: a.action, className: classes.button,
        onClick: onAct ? () => onAct(a.action) : undefined,
      }, a.action + (a.requires_approval ? ' ✍' : '')))));
}

// KBoard: a kanban grouped by the entity's board-by enum field.
export function KBoard({ entity, classes = {}, onCardClick }) {
  const { data: meta } = useMeta();
  const { records, loading } = useRecords(entity);
  const em = entityMeta(meta, entity);
  if (!meta || loading || !em || !em.ui?.board_by) return h('div', { className: classes.loading }, '…');
  const field = em.fields.find((f) => f.name === em.ui.board_by);
  const title = em.ui.list_columns?.[0] || em.fields[0]?.name;
  return h('div', { className: classes.board },
    (field?.values || []).map((v) =>
      h('div', { key: v, className: classes.column },
        h('div', { className: classes.columnTitle }, `${v} · ${records.filter((r) => r.values[em.ui.board_by] === v).length}`),
        records.filter((r) => r.values[em.ui.board_by] === v).map((r) =>
          h('div', {
            key: r.id, className: classes.card,
            onClick: onCardClick ? () => onCardClick(r) : undefined,
          }, fieldFmt(em, title, r.values[title]))))));
}

// KInbox: the human work surface — pending signatures, proposals, role tasks.
export function KInbox({ classes = {} }) {
  const { approvals, proposals, tasks, loading } = useInbox();
  if (loading) return h('div', { className: classes.loading }, '…');
  const section = (label, items, renderItem) => h('div', { className: classes.section },
    h('h3', { className: classes.sectionTitle }, `${label} · ${items.length}`),
    items.map(renderItem));
  return h('div', { className: classes.inbox },
    section('Signatures', approvals, (a) =>
      h('div', { key: a.id, className: classes.item }, `${a.action}: ${a.entity} ${a.from}→${a.to}`)),
    section('Definition changes', proposals, (p) =>
      h('div', { key: p.id, className: classes.item }, p.description || 'proposal')),
    section('Tasks', tasks, (t) =>
      h('div', { key: t.id, className: classes.item }, `${t.kind} ${t.action || ''}`)));
}

function fmt(v) {
  if (v === null || v === undefined) return '';
  if (Array.isArray(v)) return v.join(', ');
  if (typeof v === 'object') return v.name || JSON.stringify(v);
  return String(v);
}
