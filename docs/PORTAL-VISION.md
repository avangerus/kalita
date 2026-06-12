# Website + client portal + payments (founder's position, 2026-06-13)

Target combination ("like Bitrix, but through an agent"): **the user speaks via MCP
and gets a website with a client portal and payments.** This is the key
kalita capability for the SMB market.

## What is already expressible today (nothing needs to be built)
- **Client portal = role + row-level permissions.** An external client is an ordinary actor
  with the Customer role and `read Order where customer = $me` permissions; the portal is the
  same generated UI, filtered by permissions. Orders, statuses, inquiries,
  audit log — all free from the pack.
- Order/inquiry workflow, escalations, tasks — standard.

## What is missing (plan, after MVP)
1. **Self-registration of external users** — public registration with
   binding to a record (Customer) and role assignment; currently actors are created by an admin.
2. **Content pages (storefront website)** — DO NOT extend the DSL to include layout
   (the order/freedom boundary): the storefront is custom pages/theme on top of
   the public API (UI-level escape hatch), the portal is generated.
3. **Payments** — `component` integrations (Stripe/YooKassa/crypto) by contract:
   Payment entity in the pack, provider — escape hatch worker; payment events
   in the audit log, disputes/refunds — workflow with HITL.

## The order/freedom boundary (rule)
Portal, orders, statuses, payments, documents — **order** (DSL, guarantees).
Storefront, landing page, design — **freedom** (custom layer on top of the API).
They must not be mixed: free-form layout must not be able to touch data outside of permissions.
