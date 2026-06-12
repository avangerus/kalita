# ADR-004: Three Frontend Layers — Notation, Renderer, Product (the WordPress Model)

**Status:** Accepted (2026-06-14). Formalises the founder's formulation: "the
backend has notations, the frontend assembles an interface from them, on top sits
a site — like in WordPress." Refines ADR-003 and resolves the pain of "React
inside the binary is a headache."

## Three Layers

1. **Backend — source of truth and NOTATION.** `/api/meta` dictates what exists:
   entities, fields, types, actions available to the actor (buttons), filters,
   boards, node capabilities (search, etc.). The backend does not serve the
   frontend — it defines it. Zero presentational decisions in the core.

2. **Frontend — a pure RENDERER of the notation.** Contains no domain logic
   (has no knowledge of receivables/knowvault), takes meta and renders. Any
   renderer is interchangeable: the embedded preact client, an app built with
   @kalita/sdk, a third-party frontend — all consume the same notation.

3. **Product/site — on top of the renderer (like WordPress).**
   WP core = data + API ≈ our backend + meta. WP themes = renderer ≈ our
   SDK/client. A specific site (store, KnowVault, portal with payments) = theme
   + content. The end user sees the site with no awareness of themes or the core
   (T12).

## Packaging Consequence: Frontend Decoupled from the Binary

The headache = when the frontend is baked into the binary and every edit requires
a Go rebuild (and triggers "serves stale JS" bugs). Solution:

- **dev/custom:** `serve --ui-dir ./web` — static files from disk. Edit a file
  → F5, no Go rebuild. Put your own site/theme here.
- **out-of-the-box:** the embedded renderer stays behind the build tag `embedui`
  — `go build -tags embedui` produces a single self-contained binary for on-prem.
- **default** (no tag and no --ui-dir): the node serves only the API — a clean
  backend; the frontend connects externally (SDK).

preact+htm in the embedded renderer is NOT a build (a single .js file, placed
as-is); "React headache" referred to bundlers (webpack/vite + embedding a
finished bundle), which the project consciously avoids.

## Rule

Any presentation belongs to layer 2/3, never the core. A new renderer gets no
access beyond the API. A "theme" (site) does not touch data beyond meta
permissions (the order/freedom boundary from PORTAL-VISION).

## Alternatives
(1) React + bundler in the binary — rejected (rebuild headache, against the
out-of-the-box principle).
(2) Embedded frontend only — rejected (dead end for themes/partners).
(3) Frontend always external — rejected as the default: breaks "single docker
run" for the on-prem admin UI; therefore embed remains available as a build-tag
option.
