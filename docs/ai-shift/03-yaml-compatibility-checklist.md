# YAML / DSL compatibility checklist for AI-oriented evolution

Use this checklist before introducing any AI-, policy-, or execution-related change.

## 1. Parser and grammar compatibility
- [ ] Do existing `.dsl` files parse without modification?
- [ ] Are `module`, `entity`, field, and `constraints` blocks unchanged in meaning?
- [ ] Are field option tokens still treated compatibly with the current free-form `Options` model?
- [ ] Have we avoided introducing keywords that collide with existing option names or values?
- [ ] If new annotations are added, are they strictly optional and ignored safely by older behavior?

## 2. Entity and field semantics
- [ ] Do current field types keep the same meaning: `string`, `int`, `float`, `money`, `bool`, `date`, `datetime`, `enum`, `ref`, `array`?
- [ ] Do inline enums still validate exactly as before?
- [ ] Do `ref[...]` and `array[ref[...]]` resolve targets the same way as before?
- [ ] Do `required`, `unique`, `default`, `readonly`, `catalog`, and `on_delete` keep their current semantics?
- [ ] Are existing free-form validation-like options still accepted, even if not fully enforced everywhere?

## 3. YAML catalog compatibility
- [ ] Does existing enum YAML still load with the current `name` + `items` structure?
- [ ] Are `code` and `name` still sufficient for catalog items?
- [ ] Are optional item metadata fields still additive rather than required?
- [ ] Is catalog lookup behavior preserved for current field references such as `catalog=ProjectStatus`?
- [ ] If policy vocabularies are added in YAML, are they stored separately from existing business catalogs or clearly additive?

## 4. Validation compatibility
- [ ] Do create/update/patch requests still pass or fail the same way for existing payloads?
- [ ] Are type coercion and normalization behaviors unchanged for current clients?
- [ ] Are unique and composite-unique checks unchanged?
- [ ] Are reference existence checks unchanged?
- [ ] Are new AI/policy checks additive, configurable, and non-blocking by default for legacy schemas?

## 5. API compatibility
- [ ] Do existing CRUD, bulk, file, meta, and reload routes remain available?
- [ ] Are existing response shapes preserved for legacy routes?
- [ ] Are new fields in Meta API and write responses purely additive?
- [ ] Can older clients ignore new metadata without breaking?
- [ ] Are AI-specific APIs introduced as new endpoints instead of replacing current CRUD?

## 6. Runtime compatibility
- [ ] Does schema reload keep its current atomic replacement behavior?
- [ ] Do optimistic locking and ETag semantics remain unchanged?
- [ ] Do delete/restrict/set-null behaviors remain unchanged for existing entities?
- [ ] If execution logging or reviews are added, do they avoid changing the success contract of current operations?
- [ ] If persistence strategy changes later, is API-visible behavior regression-tested first?

## 7. Migration governance checklist
- [ ] Is every AI-oriented rule disabled or pass-through by default for pre-existing schemas?
- [ ] Is every blocking policy tied to explicit opt-in configuration or annotations?
- [ ] Is there a schema-diff or compatibility report before rollout?
- [ ] Are `dsl/` and `reference/enums/` examples tested as compatibility fixtures?
- [ ] Are Python API tests or equivalent regression tests run before enabling stricter behavior?

## 8. Safe defaults for the next steps
- [ ] Prefer additive metadata over grammar changes.
- [ ] Prefer sidecar policy YAML/config over immediate DSL rewrites.
- [ ] Prefer warnings and review requirements over hard rejection for legacy schemas.
- [ ] Prefer new endpoints for intent/review/simulation over repurposing CRUD semantics.
- [ ] Prefer describing current reality precisely before standardizing new abstractions.
