package engine

import (
	"context"
	"fmt"
	"sort"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Dashboard evaluation (DSL `dashboard` block). A tile is an aggregate over a
// whole table — count/sum/avg/min/max — optionally filtered by `where` and
// broken down by `group by`. Unlike a computed field (which rolls up rows tied
// to one parent record), a tile spans the table.
//
// Safety: aggregates respect row-level ABAC. Each row is counted only if the
// actor may read it, so a manager with unconditional read sees totals over
// everything while a customer scoped by `where customer = $me` sees totals over
// only their own rows. There is no separate "see all totals" grant to leak.

type GroupValue struct {
	Key   string  `json:"key"`
	Value float64 `json:"value"`
}

type TileResult struct {
	Label  string       `json:"label"`
	Func   string       `json:"func"`
	Entity string       `json:"entity"`
	Field  string       `json:"field,omitempty"`
	Value  float64      `json:"value"`            // total when not grouped
	Groups []GroupValue `json:"groups,omitempty"` // breakdown when `group by`
}

type DashboardResult struct {
	Name  string       `json:"name"`
	Title string       `json:"title,omitempty"`
	Tiles []TileResult `json:"tiles"`
}

// Dashboards lists the names/titles of dashboards in the loaded model.
func (e *Engine) Dashboards() []map[string]string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := []map[string]string{}
	for _, d := range e.model.Dashboards {
		out = append(out, map[string]string{"name": d.Name, "title": d.Title})
	}
	return out
}

// Dashboard computes one dashboard by name for the given actor.
func (e *Engine) Dashboard(ctx context.Context, actor eventstore.Actor, name string) (*DashboardResult, *Err) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, d := range e.model.Dashboards {
		if d.Name != name {
			continue
		}
		res := &DashboardResult{Name: d.Name, Title: d.Title}
		for _, tile := range d.Tiles {
			res.Tiles = append(res.Tiles, e.computeTile(tile, actor))
		}
		return res, nil
	}
	return nil, &Err{Code: CodeNotFound, Message: "no dashboard " + name,
		Rule: "dashboards are declared with a `dashboard` block in a pack"}
}

func (e *Engine) computeTile(tile dsl.DashboardTile, actor eventstore.Actor) TileResult {
	tr := TileResult{Label: tile.Label, Func: tile.Func, Entity: tile.Entity, Field: tile.Field}
	decl, ok := e.model.Entities[tile.Entity]
	if !ok {
		return tr
	}

	// accumulators, keyed by group ("" = the single ungrouped bucket)
	type acc struct {
		count int
		total float64
		first bool
		extom float64 // min/max running value
	}
	buckets := map[string]*acc{}
	order := []string{}
	get := func(key string) *acc {
		a := buckets[key]
		if a == nil {
			a = &acc{first: true}
			buckets[key] = a
			order = append(order, key)
		}
		return a
	}

	for _, id := range sortedIDs(e.records[tile.Entity]) {
		rec := e.records[tile.Entity][id]
		// row-level ABAC: count only rows this actor may read
		if d := e.can(actor, "read", tile.Entity, "", rec.Values); !d.allowed {
			continue
		}
		full := e.withComputed(decl, rec.ID, rec.Values)
		if tile.Where != "" && !evalWhere(tile.Where, e.ctxFor(rec.ID, actor, full)) {
			continue
		}
		key := ""
		if tile.GroupBy != "" {
			key = groupKey(full[tile.GroupBy])
		}
		a := get(key)
		a.count++
		if tile.Func != "count" {
			n, ok := toFloat(full[tile.Field])
			if !ok {
				continue
			}
			switch tile.Func {
			case "sum", "avg":
				a.total += n
			case "min":
				if a.first || n < a.extom {
					a.extom = n
				}
			case "max":
				if a.first || n > a.extom {
					a.extom = n
				}
			}
			a.first = false
		}
	}

	value := func(a *acc) float64 {
		switch tile.Func {
		case "count":
			return float64(a.count)
		case "sum":
			return a.total
		case "avg":
			if a.count == 0 {
				return 0
			}
			return a.total / float64(a.count)
		case "min", "max":
			return a.extom
		}
		return 0
	}

	if tile.GroupBy == "" {
		if a := buckets[""]; a != nil {
			tr.Value = value(a)
		}
		return tr
	}
	sort.Strings(order)
	for _, key := range order {
		tr.Groups = append(tr.Groups, GroupValue{Key: key, Value: value(buckets[key])})
	}
	return tr
}

// groupKey renders a field value as a stable string bucket key.
func groupKey(v any) string {
	switch x := v.(type) {
	case nil:
		return "(none)"
	case string:
		if x == "" {
			return "(none)"
		}
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", x)
	}
}
