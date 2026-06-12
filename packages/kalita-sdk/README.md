# @kalita/sdk

Build product faces over a Kalita node. The HTTP API is the real contract;
this is a thin, framework-agnostic wrapper plus React hooks. Pure ESM — import
directly in a browser, or `npm install` into Vite/Next.

The embedded admin UI that ships in the node binary covers tables, boards and
the approval inbox. Reach for the SDK when you build something the generated UI
should not be: a branded product screen (KnowVault search), a customer portal,
a public storefront over your own data.

## Vanilla

```js
import { KalitaClient } from '@kalita/sdk';

const k = new KalitaClient({ baseUrl: '', token });
const { records } = await k.query('Debtor', { filter: { status: 'Overdue' } });
await k.act('Debtor', records[0].id, 'send_claim', { type: 'human', id: 'me' });
const { answer, sources } = await k.search('What is the contract amount with Vector?');
```

## React — a working screen in under 30 lines

```jsx
import { KalitaProvider, useSearch } from '@kalita/sdk/react';
import { useState } from 'react';

function Search() {
  const { ask, answer, sources, loading } = useSearch();
  const [q, setQ] = useState('');
  return (
    <div>
      <input value={q} onChange={e => setQ(e.target.value)} />
      <button onClick={() => ask(q)} disabled={loading}>Ask</button>
      {answer && <p>{answer}</p>}
      {sources.map(s => <span key={s}>{s}</span>)}
    </div>
  );
}

export default function App({ token }) {
  return (
    <KalitaProvider token={token}>
      <Search />
    </KalitaProvider>
  );
}
```

## Notation-driven components — drop into your own design

The fastest path: components that read `/api/meta` and render themselves —
columns, types, permissions, workflow buttons, view config all come from the
backend notation. You supply the look via `classes` and render-props; the
component carries zero business logic. "The theme paints, the kernel + notation
fill in."

```jsx
import { KalitaProvider } from '@kalita/sdk/react';
import { KList, KDetail, KBoard, KInbox } from '@kalita/sdk/components';

<KalitaProvider token={token}>
  {/* a permission-aware Deals table, styled with YOUR classes */}
  <KList entity="Deal" classes={{ table: 'my-table', th: 'my-th' }}
         onRowClick={(r) => open(r.id)} />
  <KBoard entity="Deal" classes={{ board: 'flex gap-4' }} />
  <KDetail entity="Deal" id={id} onAct={(action) => act(action)} />
</KalitaProvider>
```

Unstyled by default — pass `classes` to match your design, or `render` to take
over rows entirely. The component set mirrors the kernel's view types:
KList · KBoard · KDetail · KForm · KInbox (report/calendar coming).

## Hooks

`useMeta()` · `useRecords(entity, opts)` · `useRecord(entity, id)` ·
`useInbox()` · `useSearch()` · `useKalita()` (the raw client).

## Errors

Rejections are `KalitaError` with `.code`, `.rule` (which permission rule
decided), `.fixHint` and `.status` — the same self-correction signal agents get.

## Auth

Bearer tokens issued by the node (`kalita user add`, or invite redemption via
`client.register(invite, id)`). Passkeys land in a later version.
