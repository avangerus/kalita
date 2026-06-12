# ADR-003: UI — Embedded Admin Client + Separate SDK over a Frozen Contract

**Status:** Accepted (2026-06-14). Response to the founder's question: "don't
embed JS inside — build an SDK on React."

**Context:** the universal client today is a single app.js (preact+htm), embedded
in the binary via go:embed. This works well for the admin UI, but is not suitable
for product-facing screens (KnowVault search, portals, branded storefronts) nor
for partners and pack authors who need `npm install` rather than "paste code into
our file."

**Decision — three parts:**

1. **HTTP API — the sole product contract and freeze point.** Everything
   ("what to show" — /api/meta; data — /api/records; actions — act/approve;
   search — /api/search). Any UI, regardless of kind, is merely a consumer.
   The contract is versioned and will not break without a major version bump.

2. **The embedded admin client remains.** The generated admin UI (lists, board,
   Inbox, Agents) — embedded, no build step, version matches the core, on-prem
   without node. This is "order." Pulling a bundler in here goes against the
   out-of-the-box principle.

3. **SDK as a separate package `packages/kalita-sdk` — pure ESM, no build step.**
   - `client.js` — framework-agnostic API wrapper (auth token, records, act,
     approve, search, meta, invites). Works in any JS environment.
   - `react.js` — hooks (useMeta, useRecords, useRecord, useInbox, useSearch)
     on top of client.js.
   - ESM modules are imported directly (our "no build" philosophy is preserved)
     AND published to npm as `@kalita/sdk` for full React projects.
   - The embedded client is eventually refactored on top of the same client.js —
     a single source of truth for all API calls.

**Who builds what:**
- admin / service screens → embedded client (us);
- product-facing screens (search, portal, storefront), partner-branded
  applications → SDK (us and partners).

**Alternatives considered:** (1) everything in React with a build step — rejected:
kills "out-of-the-box without node" for the on-prem admin UI; (2) keep inline
only — rejected: dead end for partners and product faces (founder's question);
(3) GraphQL layer — rejected: REST+meta is already self-describing; a second
contract = a second source of truth.

**Expected outcome (falsifiable):** a third-party developer builds a working
screen (login + record list + action) with `@kalita/sdk` in a Vite project in
< 30 lines, without reading the core source code. Fails — the SDK is not thin
or complete enough.

**Consequences:** the API becomes a public commitment (document it and do not
break it); npm publishing adds a release step for the SDK (but NOT for the core);
KnowVault search and portals migrate to the SDK as they are built.

## Addendum (2026-06-14): Three SDK Levels, the Last Being Primary

The final role of the SDK (founder's formulation): "if you want a site — you
plug in an SDK component into your design and get a convenient tool for working
with the kalita API and UI notations." In other words, the SDK is the way to
apply ANY design on top of kalita without rewriting the logic. Three levels:

1. **client.js** — raw API (records/act/search/...). Full control.
2. **react.js hooks** — useRecords/useSearch/useInbox: data into your JSX.
3. **Notation-driven components** — `<KList entity>`, `<KDetail>`, `<KBoard>`,
   `<KForm>`: the component ITSELF reads /api/meta (columns, types, permissions,
   buttons, view config) and renders; design is provided via render-props/slots
   and className, so the site keeps its own look and feel. A designer drops
   `<KList entity="Deal"/>` into their grid — gets a deals table with permissions
   and actions, zero lines of logic. This is "WordPress theme": the theme paints,
   the core + notation fill in.

Principle: notation-driven components are thin wrappers over hooks, presentation
is swappable (unstyled by default), zero domain logic inside. The closed set
matches the view types (T15): list/board/detail/form/report/calendar/custom.
