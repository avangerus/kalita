// kalita-ui / hooks.js — the HEADLESS layer under organisms.js: data +
// loading/error state, zero presentation (no h()/html() calls anywhere in
// this file). Mirrors packages/kalita-sdk/react.js's hook shape (useMeta/
// useRecords/useRecord/useInbox) but for the preact stack, so a consumer can
// pull ONLY the data/permission logic and paint their own visual — exactly
// what organisms.js itself does (it is the "standard renderer" over these
// hooks, not the only possible one). Added per the LLD-05 coordinator note
// on separating headless data/behavior from presentation in the preact
// stack the same way react.js already does for the React stack.
//
// Every hook takes the injected `api(path, opts)` function (same signature
// as web/app.js's `api()` — see atoms.js's file header on capability
// injection) as its first argument; hooks never assume a session/token.
import { useState, useEffect, useCallback } from 'kalita-vendor';

function useAsync(fn, deps) {
  const [state, setState] = useState({ data: null, loading: true, error: null });
  const run = useCallback(() => {
    let alive = true;
    setState((s) => ({ ...s, loading: true }));
    fn().then(
      (data) => alive && setState({ data, loading: false, error: null }),
      (error) => alive && setState({ data: null, loading: false, error }),
    );
    return () => { alive = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  useEffect(run, [run]);
  return { ...state, reload: run };
}

/** useMetaData: /api/meta — entities, fields, permissions, actions for this actor. */
export function useMetaData(api) {
  return useAsync(() => api('/api/meta'), [api]);
}

/** useRecordsData: a page of records for one entity. */
export function useRecordsData(api, entity, opts = {}) {
  const { data, ...rest } = useAsync(() => api(`/api/records/${entity}?limit=${opts.limit || 25}&offset=${opts.offset || 0}`), [api, entity, opts.limit, opts.offset]);
  return { records: data?.records || [], ...rest };
}

/** useRecordData: one record by id. */
export function useRecordData(api, entity, id) {
  const { data, ...rest } = useAsync(() => api(`/api/records/${entity}/${id}`), [api, entity, id]);
  return { record: data, ...rest };
}

/** useInboxData: approvals + proposals + open tasks — the human work surface. */
export function useInboxData(api) {
  const { data, ...rest } = useAsync(
    () => Promise.all([api('/api/approvals'), api('/api/proposals'), api('/api/tasks?status=open')])
      .then(([a, p, t]) => ({ approvals: a.approvals || [], proposals: p.proposals || [], tasks: t.tasks || [] })),
    [api],
  );
  return { ...(data || { approvals: [], proposals: [], tasks: [] }), ...rest };
}

// entityMeta: pick one entity's notation out of a meta payload — pure data
// lookup, no rendering; organisms.js and any custom visual built on these
// hooks share this instead of re-deriving the `.find(...)`.
export function entityMeta(meta, entity) {
  return (meta?.entities || []).find((e) => e.name === entity) || null;
}

// actionsFor: the workflow actions this actor may press on a record right
// now — from(state)/from('any') + can_act, the same permission logic
// KDetail/RecordView apply, factored out so a headless consumer gets it too.
export function actionsFor(em, record) {
  if (!em) return [];
  const state = em.workflow_field ? record?.values?.[em.workflow_field] : null;
  return (em.actions || []).filter((a) => a.can_act && (a.from === state || a.from === 'any'));
}
