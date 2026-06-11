// @kalita/sdk — framework-agnostic client over the Kalita HTTP contract.
// Pure ESM, no build step: import directly in a browser, or `npm install`
// into a React/Vite/Next project. The HTTP API is the product; this is sugar.
//
//   import { KalitaClient } from '@kalita/sdk'
//   const k = new KalitaClient({ baseUrl: '', token })
//   const { records } = await k.query('Debtor', { filter: { status: 'Overdue' } })
//   await k.act('Debtor', id, 'send_claim', { type: 'human', id: 'me' })

export class KalitaError extends Error {
  constructor(payload, status) {
    super(payload?.message || `HTTP ${status}`);
    this.code = payload?.code;
    this.rule = payload?.rule;       // PERMISSION_DENIED: which rule decided
    this.fixHint = payload?.fix_hint; // the self-correction hint
    this.status = status;
  }
}

export class KalitaClient {
  /** @param {{baseUrl?: string, token?: string, fetch?: typeof fetch}} opts */
  constructor({ baseUrl = '', token = '', fetch: f } = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.token = token;
    this._fetch = f || globalThis.fetch.bind(globalThis);
  }

  setToken(token) { this.token = token; }

  async _req(method, path, body) {
    const headers = { 'Content-Type': 'application/json' };
    if (this.token) headers.Authorization = `Bearer ${this.token}`;
    const resp = await this._fetch(this.baseUrl + path, {
      method, headers, body: body == null ? undefined : JSON.stringify(body),
    });
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) throw new KalitaError(data, resp.status);
    return data;
  }

  // --- discovery ---------------------------------------------------------------
  /** Per-actor metadata: entities, fields, the buttons you may press, capabilities. */
  meta() { return this._req('GET', '/api/meta'); }
  system() { return this._req('GET', '/api/system'); }

  // --- data --------------------------------------------------------------------
  query(entity, { filter, limit, offset } = {}) {
    const p = new URLSearchParams();
    if (limit) p.set('limit', limit);
    if (offset) p.set('offset', offset);
    for (const [k, v] of Object.entries(filter || {})) p.set(k, v);
    const qs = p.toString();
    return this._req('GET', `/api/records/${entity}${qs ? '?' + qs : ''}`);
  }
  get(entity, id) { return this._req('GET', `/api/records/${entity}/${id}`); }
  create(entity, values, basis, idempotencyKey) {
    return this._req('POST', `/api/records/${entity}`, { values, basis, idempotency_key: idempotencyKey });
  }
  update(entity, id, values, basis, idempotencyKey) {
    return this._req('PATCH', `/api/records/${entity}/${id}`, { values, basis, idempotency_key: idempotencyKey });
  }
  act(entity, id, action, basis, idempotencyKey) {
    return this._req('POST', `/api/records/${entity}/${id}/act`, { action, basis, idempotency_key: idempotencyKey });
  }
  journal(entity, id) { return this._req('GET', `/api/records/${entity}/${id}/journal`); }

  // --- files -------------------------------------------------------------------
  /** Upload a File/Blob, returns a FileRef { hash, name, size, mime } to put
   *  into a `file` field. */
  async uploadFile(file) {
    const form = new FormData();
    form.append('file', file);
    const headers = {};
    if (this.token) headers.Authorization = `Bearer ${this.token}`;
    const resp = await this._fetch(this.baseUrl + '/api/files', { method: 'POST', headers, body: form });
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) throw new KalitaError(data, resp.status);
    return data;
  }
  /** URL to download a stored file (permission-gated server-side). */
  fileUrl(hash) { return `${this.baseUrl}/api/files/${hash}`; }

  // --- inbox / human work ------------------------------------------------------
  approvals() { return this._req('GET', '/api/approvals'); }
  decideApproval(id, grant, basis, signature) {
    return this._req('POST', `/api/approvals/${id}/decide`, { grant, basis, signature });
  }
  proposals() { return this._req('GET', '/api/proposals'); }
  decideProposal(id, grant, basis, signature) {
    return this._req('POST', `/api/proposals/${id}/decide`, { grant, basis, signature });
  }
  tasks(status = 'open') { return this._req('GET', `/api/tasks?status=${status}`); }

  // --- search (when the node has a RAG backend) --------------------------------
  search(question) { return this._req('POST', '/api/search', { question }); }

  // --- onboarding --------------------------------------------------------------
  /** Public: redeem an invite, become an actor; returns { token, role }. */
  register(invite, id) { return this._req('POST', '/api/register', { invite, id }); }
  createInvite(role, { entity, recordId, bindField } = {}) {
    return this._req('POST', '/api/invites', { role, entity, record_id: recordId, bind_field: bindField });
  }

  // --- admin -------------------------------------------------------------------
  actors() { return this._req('GET', '/api/actors'); }
  disableActor(id) { return this._req('POST', `/api/actors/${id}/disable`, {}); }
}
