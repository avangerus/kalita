// web/catalog-fixtures.js — static fixture data for web/catalog.html, the
// buildless component storybook (LLD-05 PK-1 §1). No backend required: this
// module stands in for /api/meta and /api/records/* with a realistic
// MetaEntity shape (see internal/engine/meta.go) covering every field type
// atoms.js/FieldAtom must render, plus a tiny in-memory "database" and a
// mock `api(path, opts)` function with the SAME signature web/app.js's
// api() has — every kalita-ui component that needs network capability
// (RefLink's fetcher, organisms' `api` prop, SearchFace) is wired to THIS
// mock here, never a real fetch(), so catalog.html opens with zero backend.

export const accountEntity = {
  name: 'Account', label: 'Account',
  fields: [
    { name: 'name', label: 'Name', type: 'string', readable: true, writable: true, required: true },
    { name: 'industry', label: 'Industry', type: 'string', readable: true, writable: true },
  ],
  can_create: true, can_read: true, can_update: true, can_revert: false,
  actions: [], links: [], children: [], ui: { list_columns: ['name', 'industry'] },
};

export const dealEntity = {
  name: 'Deal', label: 'Deal',
  fields: [
    { name: 'name', label: 'Name', type: 'string', readable: true, writable: true, required: true },
    { name: 'notes', label: 'Notes', type: 'text', readable: true, writable: true },
    { name: 'spec', label: 'Spec (JSON)', type: 'json', readable: true, writable: true },
    { name: 'seats', label: 'Seats', type: 'int', readable: true, writable: true },
    { name: 'discount_ratio', label: 'Discount ratio', type: 'float', readable: true, writable: true },
    { name: 'amount', label: 'Amount', type: 'money', readable: true, writable: true },
    { name: 'list_price', label: 'List price (scalar)', type: 'money', readable: true, writable: true },
    { name: 'margin', label: 'Margin', type: 'percent', readable: true, writable: true },
    { name: 'weight_kg', label: 'Weight', type: 'decimal', readable: true, writable: true },
    { name: 'close_date', label: 'Close date', type: 'date', readable: true, writable: true },
    { name: 'last_touch', label: 'Last touch', type: 'datetime', readable: true, writable: true },
    { name: 'contact_email', label: 'Contact email', type: 'email', readable: true, writable: true },
    { name: 'site', label: 'Site', type: 'url', readable: true, writable: true },
    { name: 'phone', label: 'Phone', type: 'phone', readable: true, writable: true },
    { name: 'brand_color', label: 'Brand color', type: 'color', readable: true, writable: true },
    { name: 'is_strategic', label: 'Strategic?', type: 'bool', readable: true, writable: true },
    { name: 'stage', label: 'Stage', type: 'enum', values: ['New', 'Qualified', 'Won', 'Lost'], readable: true, writable: true },
    { name: 'channels', label: 'Channels', type: 'multiselect', values: ['Web', 'Referral', 'Outbound', 'Partner'], readable: true, writable: true },
    { name: 'tags', label: 'Tags', type: 'tags', readable: true, writable: true },
    { name: 'account', label: 'Account', type: 'ref', ref: 'Account', readable: true, writable: true },
    { name: 'related_accounts', label: 'Related accounts', type: 'array_ref', ref: 'Account', readable: true, writable: true },
    { name: 'contract', label: 'Contract', type: 'file', readable: true, writable: true },
    { name: 'attachments', label: 'Attachments', type: 'array_file', readable: true, writable: true },
  ],
  can_create: true, can_read: true, can_update: true, can_revert: true,
  workflow_field: 'stage',
  actions: [
    { action: 'qualify', label: 'Qualify', from: 'New', to: 'Qualified', requires_approval: false, can_act: true },
    { action: 'win', label: 'Mark won', from: 'Qualified', to: 'Won', requires_approval: true, can_act: true },
    { action: 'lose', label: 'Mark lost', from: 'any', to: 'Lost', requires_approval: false, can_act: true },
  ],
  links: [{ name: 'relates_to', target: 'Deal' }],
  children: [],
  ui: { list_columns: ['name', 'stage', 'amount', 'close_date'], board_by: 'stage' },
};

export const meta = {
  pack: 'catalog-fixtures', def_version: 1, actor_id: 'demo', role: 'Sales',
  entities: [dealEntity, accountEntity],
  roles: [{ name: 'Sales', is_agent: false }],
};

const db = {
  Account: [
    { id: 'acc-0000000000000000000001', values: { name: 'Northwind Traders', industry: 'Retail' } },
    { id: 'acc-0000000000000000000002', values: { name: 'Globex', industry: 'Manufacturing' } },
  ],
  Deal: [
    {
      id: 'deal-000000000000000000001',
      values: {
        name: 'Rich types demo', notes: 'Multi-line\nwork product text.', spec: { tier: 'gold', seats: 12 },
        seats: 12, discount_ratio: 0.15, amount: { amount: 128000, currency: 'USD' }, list_price: 150000,
        margin: 22, weight_kg: 1234.5, close_date: '2026-08-01', last_touch: '2026-07-01T14:32:00Z',
        contact_email: 'buyer@northwind.example', site: 'https://northwind.example', phone: '+1 555 0100',
        brand_color: '#4da3ff', is_strategic: true, stage: 'Qualified', channels: ['Web', 'Partner'],
        tags: ['enterprise', 'renewal'], account: 'acc-0000000000000000000001',
        related_accounts: ['acc-0000000000000000000001', 'acc-0000000000000000000002'],
        contract: { name: 'msa.pdf', size: 48213 }, attachments: [{ name: 'quote.pdf', size: 9210 }],
      },
    },
    {
      id: 'deal-000000000000000000002',
      values: { name: 'Empty-ish deal', stage: 'New', amount: 5000, account: 'acc-0000000000000000000002' },
    },
  ],
};

/** mockApi: same (path, opts) signature as web/app.js's api(). */
export function mockApi(path, opts = {}) {
  const [route, qs] = path.split('?');
  const parts = route.split('/').filter(Boolean); // ["api","records","Deal","id"]
  const wait = (v) => new Promise((res) => setTimeout(() => res(v), 120));

  if (parts[0] === 'api' && parts[1] === 'meta') return wait(meta);
  if (parts[0] === 'api' && parts[1] === 'records' && parts[2]) {
    const rows = db[parts[2]] || [];
    if (parts[3]) {
      const rec = rows.find((r) => r.id === parts[3]);
      return rec ? wait(rec) : Promise.reject({ message: 'not found' });
    }
    return wait({ records: rows });
  }
  if (parts[0] === 'api' && parts[1] === 'query' && parts[2]) {
    const body = opts.body ? JSON.parse(opts.body) : {};
    const term = (body.search || '').toLowerCase();
    const rows = (db[parts[2]] || []).filter((r) => !term || JSON.stringify(r.values).toLowerCase().includes(term));
    return wait({ records: rows });
  }
  if (parts[0] === 'api' && parts[1] === 'approvals') return wait({ approvals: fixtureApprovals });
  if (parts[0] === 'api' && parts[1] === 'proposals') return wait({ proposals: [] });
  if (parts[0] === 'api' && parts[1] === 'tasks') return wait({ tasks: [] });
  if (parts[0] === 'api' && parts[1] === 'search') {
    return wait({ answer: 'Fixture answer: catalog.html never calls a real search worker — this text stands in for one.', sources: ['handbook.pdf', 'policy.md'] });
  }
  return wait({});
}

export const fixtureApprovals = [
  {
    id: 'appr-1', entity: 'Deal', record_id: 'deal-000000000000000000001',
    action: 'win', from: 'Qualified', to: 'Won',
    requested_by: { id: 'sales-agent', type: 'agent' },
  },
];

// A fixture "clause risk" entity + record for RiskCardFace's showcase — kept
// separate from Deal so the face's example is not mistaken for domain wiring.
export const clauseEntity = {
  name: 'ClauseFinding', label: 'Clause finding',
  fields: [
    { name: 'title', label: 'Clause', type: 'string', readable: true, writable: true },
    { name: 'risk', label: 'Risk level', type: 'enum', values: ['Low', 'Medium', 'High'], readable: true, writable: true },
    { name: 'text', label: 'Clause text', type: 'text', readable: true, writable: true },
    { name: 'section', label: 'Section', type: 'string', readable: true, writable: true },
    { name: 'reviewer', label: 'Reviewer', type: 'string', readable: true, writable: true },
  ],
  can_create: true, can_read: true, can_update: true, can_revert: false,
  actions: [], links: [], children: [], ui: {},
};

export const clauseRecord = {
  id: 'clause-1',
  values: {
    title: 'Limitation of liability', risk: 'High', section: '§9.2', reviewer: 'legal-agent',
    text: 'Neither party shall be liable for indirect or consequential damages, except in cases of gross negligence...',
  },
};
