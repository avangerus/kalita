// @kalita/sdk/components — notation-driven React components. Each one reads
// /api/meta and renders itself (columns, types, permissions, actions, view
// config); the host site supplies the look via className and render-props.
// Drop <KList entity="Deal"/> into your own design and get a permission-aware
// table with zero logic — "the theme paints, the kernel + notation fill in".
//
// Unstyled by default: pass classNames or a `render` prop to match your design.

import { createElement as h, Fragment } from 'react';
import { useMeta, useRecords, useRecord, useInbox } from './react.js';

// entityMeta picks one entity's notation out of the meta payload.
function entityMeta(meta, entity) {
  return meta?.entities?.find((e) => e.name === entity) || null;
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
      }, cols.map((c) => h('td', { key: c, className: classes.td }, fmt(r.values[c])))))));
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
          h('span', { className: classes.value }, fmt(record?.values?.[f.name]))))),
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
          }, fmt(r.values[title]))))));
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
