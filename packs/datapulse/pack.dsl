pack datapulse
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1

# ENH-01: DSL-model-only Sprint 1 demo asset — no `product:` white-label
# block yet (packs/knowvault/pack.dsl's pattern). A `product:` block needs a
# `home` face to point at, and this card explicitly excludes UI-blocks (see
# entities.dsl / workflows.dsl / security.dsl headers) — adding one now would
# either dangle a reference to a screen that does not exist yet or force
# inventing UI out of this card's scope. Add it alongside the first real
# screen (ENH-08 dashboard or later), following packs/contracts' minimal
# style (pack/version/requires/depends only) until then.
