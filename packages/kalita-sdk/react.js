// @kalita/sdk/react — React hooks over KalitaClient. Pure ESM, peer-depends on
// react. Build product faces (search, portals, branded apps) with these.
//
//   import { KalitaProvider, useRecords, useSearch } from '@kalita/sdk/react'
//   <KalitaProvider token={token}>...</KalitaProvider>
//   const { records, loading } = useRecords('Debtor', { filter: { status: 'Overdue' } })

import { createContext, createElement, useContext, useEffect, useState, useCallback } from 'react';
import { KalitaClient } from './client.js';

const Ctx = createContext(null);

export function KalitaProvider({ baseUrl = '', token, children }) {
  const [client] = useState(() => new KalitaClient({ baseUrl, token }));
  if (token) client.setToken(token);
  return createElement(Ctx.Provider, { value: client }, children);
}

/** The shared client; throws if used outside a provider. */
export function useKalita() {
  const c = useContext(Ctx);
  if (!c) throw new Error('useKalita: wrap your app in <KalitaProvider>');
  return c;
}

function useAsync(fn, deps) {
  const [state, setState] = useState({ data: null, loading: true, error: null });
  const run = useCallback(() => {
    let alive = true;
    setState(s => ({ ...s, loading: true }));
    fn().then(
      data => alive && setState({ data, loading: false, error: null }),
      error => alive && setState({ data: null, loading: false, error }),
    );
    return () => { alive = false; };
  }, deps); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(run, [run]);
  return { ...state, reload: run };
}

export function useMeta() {
  const k = useKalita();
  return useAsync(() => k.meta(), [k]);
}

export function useRecords(entity, opts = {}) {
  const k = useKalita();
  const { data, ...rest } = useAsync(() => k.query(entity, opts), [k, entity, JSON.stringify(opts)]);
  return { records: data?.records || [], ...rest };
}

export function useRecord(entity, id) {
  const k = useKalita();
  const { data, ...rest } = useAsync(() => k.get(entity, id), [k, entity, id]);
  return { record: data, ...rest };
}

export function useInbox() {
  const k = useKalita();
  const { data, ...rest } = useAsync(
    () => Promise.all([k.approvals(), k.proposals(), k.tasks()])
      .then(([a, p, t]) => ({ approvals: a.approvals || [], proposals: p.proposals || [], tasks: t.tasks || [] })),
    [k]);
  return { ...(data || { approvals: [], proposals: [], tasks: [] }), ...rest };
}

/** Imperative search: returns { ask, answer, sources, loading, error }. */
export function useSearch() {
  const k = useKalita();
  const [state, setState] = useState({ answer: null, sources: [], loading: false, error: null });
  const ask = useCallback(async (question) => {
    setState(s => ({ ...s, loading: true, error: null }));
    try {
      const r = await k.search(question);
      setState({ answer: r.answer, sources: r.sources || [], loading: false, error: null });
    } catch (error) {
      setState({ answer: null, sources: [], loading: false, error });
    }
  }, [k]);
  return { ask, ...state };
}
