// web/catalog.js — the storybook bootstrap for web/catalog.html.
// External (not inline) on purpose: the node serves this page under a strict
// CSP (script-src is `self` + one hashed import map, NOT `unsafe-inline` —
// internal/api/secure.go), so an inline <script type="module"> would be
// blocked when the catalog is served through the node. As an external
// same-origin module it loads under `self`, and opening catalog.html straight
// off disk (file://, no CSP) works too. Its imports resolve identically in
// both modes (see packages/kalita-ui/atoms.js header).

import { render, html, useState, useEffect } from 'kalita-vendor';
import * as atoms from '../packages/kalita-ui/atoms.js';
import * as molecules from '../packages/kalita-ui/molecules.js';
import * as organisms from '../packages/kalita-ui/organisms.js';
import * as faces from '../packages/kalita-ui/faces.js';
import { meta, dealEntity, clauseEntity, clauseRecord, mockApi, fixtureApprovals } from './catalog-fixtures.js';

const { Input, Select, EnumSelect, MultiSelect, Money, Badge, StatusChip, DateTime, Bool, ColorPicker, Text, FileAttachment, RefLink, FieldAtom } = atoms;
const { FieldRow, RecordCard, ApprovalCard, TimelineItem, DashboardTile } = molecules;
const { KList, KDetail, KBoard, KInbox } = organisms;
const { SearchFace, RiskCardFace, ChatFace } = faces;

const fakeUploader = (file) => new Promise((res) => setTimeout(() => res({ name: file.name, size: file.size }), 300));
const neverResolves = () => new Promise(() => {}); // for the "loading" RefLink example

// Section/Example: pure layout helpers, not part of the design system itself.
const Section = ({ title, children }) => html`<section><h2>${title}</h2>${children}</section>`;
const Sub = ({ title, children }) => html`<div><h3>${title}</h3>${children}</div>`;
const Example = ({ caption, children }) => html`<div class="ex"><div class="cap">${caption}</div>${children}</div>`;

// --- Field playground: one generic knob-driven example that exercises
// FieldAtom (and therefore every named atom) across ALL meta field types —
// the acceptance checklist item "each atom renders all its meta-types".
const FIELD_TYPES = ['string', 'text', 'json', 'int', 'float', 'money', 'percent', 'decimal',
  'date', 'datetime', 'email', 'url', 'phone', 'color', 'bool', 'enum', 'multiselect', 'tags',
  'ref', 'array_ref', 'file', 'array_file'];
const SAMPLE_VALUE = {
  string: '"Hello"', text: '"Multi-line\\ntext"', json: '{"tier":"gold"}', int: '42', float: '3.14',
  money: '{"amount":128000,"currency":"USD"}', percent: '22', decimal: '1234.5',
  date: '"2026-08-01"', datetime: '"2026-07-01T14:32:00Z"', email: '"buyer@example.com"',
  url: '"https://example.com"', phone: '"+1 555 0100"', color: '"#4da3ff"', bool: 'true',
  enum: '"B"', multiselect: '["A","C"]', tags: '["enterprise","renewal"]',
  ref: '"acc-0000000000000000000001"', array_ref: '["acc-0000000000000000000001"]',
  file: '{"name":"msa.pdf","size":48213}', array_file: '[{"name":"quote.pdf","size":9210}]',
};
function FieldPlayground() {
  const [type, setType] = useState('money');
  const [raw, setRaw] = useState(SAMPLE_VALUE.money);
  let value; try { value = JSON.parse(raw); } catch { value = raw; }
  const [editVal, setEditVal] = useState(value);
  const field = { name: 'demo_field', label: 'Demo field', type,
    values: (type === 'enum' || type === 'multiselect') ? ['A', 'B', 'C'] : undefined,
    ref: (type === 'ref' || type === 'array_ref') ? 'Account' : undefined };
  const setType2 = (t) => { setType(t); setRaw(SAMPLE_VALUE[t] ?? '""'); try { setEditVal(JSON.parse(SAMPLE_VALUE[t] ?? '""')); } catch { setEditVal(''); } };
  return html`<div class="card">
    <div class="knobs">
      <div><label>field.type (knob)</label><select value=${type} onInput=${(e) => setType2(e.target.value)}>
        ${FIELD_TYPES.map((t) => html`<option value=${t}>${t}</option>`)}</select></div>
      <div style="flex:1;min-width:260px"><label>value — JSON (knob)</label>
        <input value=${raw} onInput=${(e) => { setRaw(e.target.value); try { setEditVal(JSON.parse(e.target.value)); } catch {} }} /></div>
    </div>
    <div style="display:grid;grid-template-columns:1fr 1fr;gap:16px">
      <div><div class="muted" style="margin-bottom:4px">READ — FieldAtom, no onChange</div>
        <${FieldAtom} field=${field} value=${value} fetcher=${(e, id) => mockApi(`/api/records/${e}/${id}`)} /></div>
      <div><div class="muted" style="margin-bottom:4px">EDIT — FieldAtom, onChange</div>
        <${FieldAtom} field=${field} value=${editVal} onChange=${setEditVal} uploader=${fakeUploader} suggestions=${['existing-tag-a', 'existing-tag-b']} />
        <div class="muted" style="margin-top:6px">current edit value: <code>${JSON.stringify(editVal)}</code></div></div>
    </div>
  </div>`;
}

function Catalog() {
  const [boolVal, setBoolVal] = useState(true);
  const [dtVal, setDtVal] = useState('2026-07-01T14:32:00Z');
  const [rec, setRec] = useState(null);
  useEffect(() => { mockApi('/api/records/Deal/deal-000000000000000000001').then(setRec); }, []);

  return html`<div class="wrap">
    <h1>Kalita UI — component catalog</h1>
    <p class="lede">Buildless storybook for <code>packages/kalita-ui</code> (LLD-05 PK-1). Every example below
      is driven by the SAME field/meta shape web/app.js and web/catalog.html both consume — no backend running.</p>

    ${Section({ title: 'Tokens', children: html`<div class="grid">
      ${['--bg', '--panel', '--line', '--fg', '--dim', '--acc', '--ok', '--warn', '--bad'].map((v) => html`
        <div class="ex"><div class="cap">${v}</div>
          <div style="height:28px;border-radius:4px;border:1px solid var(--line);background:var(${v})"></div></div>`)}
    </div>` })}

    ${Section({ title: 'Atoms — field playground (every meta type)', children: html`<${FieldPlayground} />` })}

    ${Section({ title: 'Atoms — named examples', children: html`
      ${Sub({ title: 'Money — scalar number vs {amount,currency}', children: html`<div class="grid">
        ${Example({ caption: 'read, scalar', children: html`<${Money} field=${{ type: 'money' }} value=${150000} />` })}
        ${Example({ caption: 'read, object', children: html`<${Money} field=${{ type: 'money' }} value=${{ amount: 128000, currency: 'USD' }} />` })}
        ${Example({ caption: 'read, empty', children: html`<${Money} field=${{ type: 'money' }} value=${null} />` })}
        ${Example({ caption: 'edit', children: html`<${Money} field=${{ type: 'money' }} value=${128000} onChange=${() => {}} />` })}
      </div>` })}
      ${Sub({ title: 'Bool — true / false / unset (knob)', children: html`<div class="grid">
        ${Example({ caption: 'read', children: html`<${Bool} field=${{ type: 'bool' }} value=${boolVal} />` })}
        ${Example({ caption: 'edit (knob toggles read example above)', children: html`<${Bool} field=${{ type: 'bool' }} value=${boolVal} onChange=${setBoolVal} />` })}
        ${Example({ caption: 'unset', children: html`<${Bool} field=${{ type: 'bool' }} value=${null} />` })}
      </div>` })}
      ${Sub({ title: 'DateTime — value vs empty (knob)', children: html`<div class="grid">
        ${Example({ caption: 'read', children: html`<${DateTime} field=${{ type: 'datetime' }} value=${dtVal} />` })}
        ${Example({ caption: 'edit', children: html`<${DateTime} field=${{ type: 'datetime' }} value=${dtVal} onChange=${setDtVal} />` })}
        ${Example({ caption: 'read, empty', children: html`<${DateTime} field=${{ type: 'datetime' }} value=${null} />` })}
        ${Example({ caption: 'edit, date-only', children: html`<${DateTime} field=${{ type: 'date' }} value="2026-08-01" onChange=${() => {}} />` })}
      </div>` })}
      ${Sub({ title: 'RefLink — resolved / loading / empty', children: html`<div class="grid">
        ${Example({ caption: 'resolved (mock fetcher)', children: html`<${RefLink} field=${{ ref: 'Account' }} value="acc-0000000000000000000001" fetcher=${(e, id) => mockApi(`/api/records/${e}/${id}`)} />` })}
        ${Example({ caption: 'loading (fetcher never resolves)', children: html`<${RefLink} field=${{ ref: 'Account' }} value="acc-loading" fetcher=${neverResolves} />` })}
        ${Example({ caption: 'empty', children: html`<${RefLink} field=${{ ref: 'Account' }} value=${null} />` })}
        ${Example({ caption: 'multi (array_ref)', children: html`<${RefLink} field=${{ ref: 'Account' }} multi
          value=${['acc-0000000000000000000001', 'acc-0000000000000000000002']} fetcher=${(e, id) => mockApi(`/api/records/${e}/${id}`)} />` })}
      </div>` })}
      ${Sub({ title: 'Badge / StatusChip', children: html`<div class="grid">
        ${Example({ caption: 'Badge, default', children: html`<${Badge}>enterprise</${Badge}>` })}
        ${Example({ caption: 'StatusChip, ok', children: html`<${StatusChip} value="Won" tone="ok" />` })}
        ${Example({ caption: 'StatusChip, warn', children: html`<${StatusChip} value="Qualified" tone="warn" />` })}
        ${Example({ caption: 'StatusChip, bad', children: html`<${StatusChip} value="Lost" tone="bad" />` })}
      </div>` })}
      ${Sub({ title: 'MultiSelect — closed list vs free tags', children: html`<div class="grid">
        ${Example({ caption: 'edit, multiselect (closed)', children: html`<${MultiSelect} field=${{ type: 'multiselect', values: ['Web', 'Referral', 'Outbound'] }} value=${['Web']} onChange=${() => {}} />` })}
        ${Example({ caption: 'edit, tags (free + suggestions)', children: html`<${MultiSelect} field=${{ type: 'tags' }} value=${['enterprise']} suggestions=${['renewal', 'churn-risk']} onChange=${() => {}} />` })}
        ${Example({ caption: 'read', children: html`<${MultiSelect} field=${{ type: 'tags' }} value=${['enterprise', 'renewal']} />` })}
      </div>` })}
      ${Sub({ title: 'Input / Select / ColorPicker / Text', children: html`<div class="grid">
        ${Example({ caption: 'Input, email edit', children: html`<${Input} field=${{ type: 'email' }} value="buyer@example.com" onChange=${() => {}} />` })}
        ${Example({ caption: 'Input, url read', children: html`<${Input} field=${{ type: 'url' }} value="https://example.com" />` })}
        ${Example({ caption: 'EnumSelect edit', children: html`<${EnumSelect} field=${{ values: ['New', 'Qualified', 'Won'] }} value="Qualified" onChange=${() => {}} />` })}
        ${Example({ caption: 'Select, custom options', children: html`<${Select} options=${['low', 'medium', 'high']} value="medium" onChange=${() => {}} />` })}
        ${Example({ caption: 'ColorPicker edit', children: html`<${ColorPicker} value="#4da3ff" onChange=${() => {}} />` })}
        ${Example({ caption: 'Text, json edit', children: html`<${Text} field=${{ type: 'json' }} value=${{ tier: 'gold' }} onChange=${() => {}} />` })}
        ${Example({ caption: 'FileAttachment edit (fake uploader)', children: html`<${FileAttachment} field=${{ type: 'file' }} value=${null} onChange=${() => {}} uploader=${fakeUploader} />` })}
      </div>` })}
    ` })}

    ${Section({ title: 'Molecules', children: html`
      ${Sub({ title: 'FieldRow', children: html`<div class="card" style="max-width:320px">
        <${FieldRow} field=${{ name: 'amount', label: 'Amount', type: 'money' }} value=${{ amount: 128000, currency: 'USD' }} /></div>` })}
      ${Sub({ title: 'RecordCard', children: rec ? html`<div style="max-width:320px"><${RecordCard} ent=${dealEntity} record=${rec} /></div>` : html`<div class="muted">loading fixture…</div>` })}
      ${Sub({ title: 'ApprovalCard', children: html`<${ApprovalCard} a=${fixtureApprovals[0]} ent=${dealEntity}
        onDecide=${() => {}} fetchRecord=${(e, id) => mockApi(`/api/records/${e}/${id}`)} />` })}
      ${Sub({ title: 'TimelineItem', children: html`<div class="card">
        <${TimelineItem} e=${{ seq: 1, kind: 'record.created', ts: '2026-07-01T10:00:00Z', actor: { id: 'sales-agent', type: 'agent' } }} canRevert=${false} lastMutSeq=${2} />
        <${TimelineItem} e=${{ seq: 2, kind: 'record.updated', ts: '2026-07-01T11:00:00Z', actor: { id: 'alice', type: 'human' }, payload: { changes: [{ field: 'stage', old: 'New', new: 'Qualified' }] } }} canRevert=${true} lastMutSeq=${2} onRewind=${() => {}} />
      </div>` })}
      ${Sub({ title: 'DashboardTile — number vs grouped', children: html`<div style="display:flex;gap:12px;flex-wrap:wrap">
        <${DashboardTile} t=${{ label: 'Open deals', value: 42 }} />
        <${DashboardTile} t=${{ label: 'By stage', groups: [{ key: 'New', value: 10 }, { key: 'Qualified', value: 6 }, { key: 'Won', value: 3 }] }} />
      </div>` })}
    ` })}

    ${Section({ title: 'Organisms (mock api, no backend)', children: html`
      ${Sub({ title: 'KList', children: html`<${KList} api=${mockApi} meta=${meta} entity="Deal" />` })}
      ${Sub({ title: 'KDetail', children: html`<${KDetail} api=${mockApi} meta=${meta} entity="Deal" id="deal-000000000000000000001" />` })}
      ${Sub({ title: 'KBoard', children: html`<${KBoard} api=${mockApi} meta=${meta} entity="Deal" />` })}
      ${Sub({ title: 'KInbox', children: html`<${KInbox} api=${mockApi} meta=${meta}
        onDecide=${() => Promise.resolve()} onDecideProposal=${() => Promise.resolve()} />` })}
    ` })}

    ${Section({ title: 'Faces', children: html`
      ${Sub({ title: 'SearchFace', children: html`<${SearchFace} api=${mockApi} scopeEntity="Account" />` })}
      ${Sub({ title: 'RiskCardFace', children: html`<${RiskCardFace} ent=${clauseEntity} record=${clauseRecord}
        riskField="risk" clauseField="text" riskTone=${(v) => ({ Low: 'ok', Medium: 'warn', High: 'bad' }[v] || 'default')} />` })}
      ${Sub({ title: 'ChatFace (LLD-06 stub)', children: html`<${ChatFace} />` })}
    ` })}
  </div>`;
}

render(html`<${Catalog} />`, document.getElementById('root'));