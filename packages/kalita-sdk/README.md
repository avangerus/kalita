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
const { answer, sources } = await k.search('Сумма договора с Вектором?');
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

## Hooks

`useMeta()` · `useRecords(entity, opts)` · `useRecord(entity, id)` ·
`useInbox()` · `useSearch()` · `useKalita()` (the raw client).

## Errors

Rejections are `KalitaError` with `.code`, `.rule` (which permission rule
decided), `.fixHint` and `.status` — the same self-correction signal agents get.

## Auth

Bearer tokens issued by the node (`kalita user add`, or invite redemption via
`client.register(invite, id)`). Passkeys land in a later version.
