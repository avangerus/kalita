pack knowvault
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1

# White-label identity (DSL-SPEC-v0 §1.1, LLD-05 §3, PK-4): kalita itself is
# never shown to a KnowVault customer — the engine is invisible by design
# (packs/knowvault/README.md §0). Node --brand* flags default from this block;
# the frontend reads it via /api/meta to theme itself, no rebuild required.
product:
    name "KnowVault"
    color accent "#4da3ff"
    home SearchFace
    auth password
