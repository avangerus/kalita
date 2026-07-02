package dsl

import (
	"fmt"
	"sort"
	"strings"
)

// MergeModels combines N independently-compiled pack models into the one
// model a single node runs. This is the multi-pack loading step: each input
// model is compiled on its own via Compile (so within-pack duplicates are
// already caught by the existing E009/E014/... checks) — MergeModels adds
// the CROSS-pack check that a solo Compile cannot see: two packs declaring
// the same entity/role/event/workflow/link-name/integration is a naming
// collision, reported loudly with both packs named, never a silent
// last-wins overwrite (docs/TECHDEBT.md "LLD-06 ... cross-pack composition").
//
// It deliberately does NOT attempt cross-pack references (ref[OtherPack.X],
// namespaced lookups, overlays) — that is LLD-19 territory. The contract
// here is narrower: N packs coexist on one node, each pack's own symbols
// stay intact, and a name clash is a compile error, not silent shadowing.
func MergeModels(models []*Model) (*Model, []*Error) {
	if len(models) == 0 {
		return nil, []*Error{{Message: "MergeModels: no pack models given", FixHint: "pass at least one compiled pack model"}}
	}
	if len(models) == 1 {
		return models[0], nil
	}

	errs := &Errors{}
	out := &Model{
		Entities:     map[string]*EntityDecl{},
		Events:       map[string]*EventDecl{},
		Roles:        map[string]*RoleDecl{},
		Perms:        map[string]*PermBlock{},
		Workflows:    map[string]*WorkflowDecl{},
		Integrations: map[string]*IntegrationDecl{},
	}

	labels := packLabels(models)

	// origin records which pack (+ file:line) first declared a merged
	// symbol, so a second declaration's error can name both sides.
	type origin struct {
		pack, file string
		line       int
	}
	entityOrigin := map[string]origin{}
	roleOrigin := map[string]origin{}
	eventOrigin := map[string]origin{}
	workflowOrigin := map[string]origin{}
	integrationOrigin := map[string]origin{}
	linkOrigin := map[string]origin{} // key: entity + "." + link-name

	var productPack, productMCPPack string
	var depends []string
	dependsSeen := map[string]bool{}
	var requires string

	for i, m := range models {
		pack := labels[i]

		// --- manifest: no single Manifest can name every pack on the node;
		// combine into a synthetic display identity (Name = joined packs).
		// Individual packs' identity is NOT lost — it lives in every
		// symbol's File path and in this function's error messages — only
		// the single display Name/Version fields (used for logging and
		// /api/meta) are necessarily a summary once >1 pack is loaded.
		if m.Manifest != nil {
			if requires == "" {
				requires = m.Manifest.Requires
			}
			for _, d := range m.Manifest.Depends {
				if !dependsSeen[d] {
					dependsSeen[d] = true
					depends = append(depends, d)
				}
			}
		}

		// --- entities (core.* / mdg dictionaries are kernel-injected into
		// EVERY pack's own solo compile — expect them in every model and
		// union them; not an authoring collision).
		for _, name := range m.Order {
			e := m.Entities[name]
			if isCoreEntity(name) {
				if _, exists := out.Entities[name]; !exists {
					out.Entities[name] = e
					out.Order = append(out.Order, name)
				}
				continue
			}
			if prev, dup := entityOrigin[name]; dup {
				errs.add(EDupEntity, e.File, e.Line,
					fmt.Sprintf("entity %s is declared in pack %s (%s:%d) and pack %s (%s:%d)",
						name, prev.pack, prev.file, prev.line, pack, e.File, e.Line),
					"entity names must be unique across every pack loaded on this node — rename one")
				continue
			}
			entityOrigin[name] = origin{pack, e.File, e.Line}
			out.Entities[name] = e
			out.Order = append(out.Order, name)
		}

		// --- roles
		for name, r := range m.Roles {
			if prev, dup := roleOrigin[name]; dup {
				errs.add(EDupRole, "", r.Line,
					fmt.Sprintf("role %s is declared in pack %s and pack %s", name, prev.pack, pack),
					"role names must be unique across every pack loaded on this node — rename one")
				continue
			}
			roleOrigin[name] = origin{pack, "", r.Line}
			out.Roles[name] = r
		}

		// --- events (short name)
		for name, ev := range m.Events {
			if prev, dup := eventOrigin[name]; dup {
				errs.add(EDupEvent, ev.File, ev.Line,
					fmt.Sprintf("event %s is declared in pack %s (%s:%d) and pack %s (%s:%d)",
						name, prev.pack, prev.file, prev.line, pack, ev.File, ev.Line),
					"event names must be unique across every pack loaded on this node — rename one")
				continue
			}
			eventOrigin[name] = origin{pack, ev.File, ev.Line}
			out.Events[name] = ev
		}

		// --- workflows (keyed by entity name — a workflow collision here
		// can only happen on a shared core.* entity, since a pack's own
		// entity already collided above; still checked for completeness).
		for name, wf := range m.Workflows {
			if prev, dup := workflowOrigin[name]; dup {
				errs.add(EDupWorkflow, wf.File, wf.Line,
					fmt.Sprintf("workflow on %s is declared in pack %s (%s:%d) and pack %s (%s:%d)",
						name, prev.pack, prev.file, prev.line, pack, wf.File, wf.Line),
					"only one pack may govern a given entity's workflow field")
				continue
			}
			workflowOrigin[name] = origin{pack, wf.File, wf.Line}
			out.Workflows[name] = wf
		}

		// --- integrations (connector name)
		for name, decl := range m.Integrations {
			if prev, dup := integrationOrigin[name]; dup {
				errs.add(EBadIntegration, decl.File, decl.Line,
					fmt.Sprintf("integration %s is declared in pack %s (%s:%d) and pack %s (%s:%d)",
						name, prev.pack, prev.file, prev.line, pack, decl.File, decl.Line),
					"connector names must be unique across every pack loaded on this node — rename one")
				continue
			}
			integrationOrigin[name] = origin{pack, decl.File, decl.Line}
			out.Integrations[name] = decl
		}

		// --- links: same per-entity forward/inverse name-uniqueness rule
		// analyzeLinks enforces within one pack, extended across packs.
		for _, l := range m.Links {
			markLinkName := func(entity, name string) {
				key := entity + "." + name
				if prev, dup := linkOrigin[key]; dup {
					errs.add(EDupLinkName, l.File, l.Line,
						fmt.Sprintf("link name %s on %s is declared in pack %s (%s:%d) and pack %s (%s:%d)",
							name, entity, prev.pack, prev.file, prev.line, pack, l.File, l.Line),
						"forward and inverse link names must be unique per entity across every pack loaded on this node")
					return
				}
				linkOrigin[key] = origin{pack, l.File, l.Line}
			}
			markLinkName(l.From, l.Forward)
			if !(l.From == l.To && l.Forward == l.Inverse) {
				markLinkName(l.To, l.Inverse)
			}
			out.Links = append(out.Links, l)
		}

		// --- perms: role-name collisions are already caught above (Roles
		// merge), so a Perms key clash cannot occur once Roles pass clean —
		// straight union, merging rule sets for a role extended across
		// packs exactly like Compile does for permission blocks within one
		// pack.
		for role, pb := range m.Perms {
			if existing, ok := out.Perms[role]; ok {
				cp := *existing
				cp.Rules = append(append([]PermRule{}, existing.Rules...), pb.Rules...)
				out.Perms[role] = &cp
			} else {
				cp := *pb
				out.Perms[role] = &cp
			}
		}

		// --- product / product.mcp: at most one loaded pack may brand the
		// node or register a tool façade — silently picking "whichever pack
		// happened to compile first" would be exactly the last-wins
		// ambiguity this function exists to reject.
		if m.Product != nil {
			if out.Product != nil {
				errs.add(EBadProduct, m.Product.File, m.Product.Line,
					fmt.Sprintf("product{} is declared in pack %s and again in pack %s", productPack, pack),
					"only one loaded pack may declare a product{} white-label block")
			} else {
				out.Product = m.Product
				productPack = pack
			}
		}
		if m.ProductMCP != nil {
			if out.ProductMCP != nil {
				errs.add(EBadProductMCP, m.ProductMCP.File, m.ProductMCP.Line,
					fmt.Sprintf("product.mcp{} is declared in pack %s and again in pack %s", productMCPPack, pack),
					"only one loaded pack may declare a product.mcp{} tool registry")
			} else {
				out.ProductMCP = m.ProductMCP
				productMCPPack = pack
			}
		}

		out.Automations = append(out.Automations, m.Automations...)
		out.UIs = append(out.UIs, m.UIs...)
		out.Dashboards = append(out.Dashboards, m.Dashboards...)
	}

	sort.Strings(depends)
	out.Manifest = &Manifest{
		Name:     strings.Join(labels, "+"),
		Version:  "",
		Requires: requires,
		Depends:  depends,
	}

	return out, errs.List
}

// isCoreEntity reports whether name is a kernel-injected built-in
// (core.User, core.Calendar, or an mdg[Name] auto-provisioned dictionary) —
// these are re-injected identically into every pack's own solo compile and
// must be unioned across packs, not flagged as an authoring collision.
func isCoreEntity(name string) bool {
	return name == "core.User" || strings.HasPrefix(name, corePrefix)
}

// packLabels returns one display label per model, preferring the pack's own
// manifest name and falling back to a positional label if a model somehow
// has no manifest (e.g. an empty/genesis compile passed in by mistake).
// Two DIFFERENT directories compiled under the SAME pack name still get
// disambiguated labels so error messages stay meaningful.
func packLabels(models []*Model) []string {
	out := make([]string, len(models))
	seen := map[string]int{}
	for i, m := range models {
		name := m.PackName()
		if name == "" {
			name = fmt.Sprintf("pack#%d", i+1)
		}
		if n, dup := seen[name]; dup {
			seen[name] = n + 1
			name = fmt.Sprintf("%s#%d", name, n+1)
		} else {
			seen[name] = 1
		}
		out[i] = name
	}
	return out
}
