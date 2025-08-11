package api

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// ==== Типы сортировки и параметров листинга ====

type SortKey struct {
	Field string
	Desc  bool
}

type ListParams struct {
	Limit   int
	Offset  int
	Sort    []SortKey
	Filters map[string][]string
	Q       string
	Nulls   string // "last" (default) | "first"
}

// ==== Парсинг query-параметров ====

func parseListParams(q url.Values) ListParams {
	// limit
	limit := 50
	lv := q.Get("_limit")
	if lv == "" {
		lv = q.Get("limit")
	}
	if lv != "" {
		if n, err := strconv.Atoi(lv); err == nil && n >= 0 && n <= 1000 {
			limit = n
		}
	}

	// offset
	offset := 0
	ov := q.Get("_offset")
	if ov == "" {
		ov = q.Get("offset")
	}
	if ov != "" {
		if n, err := strconv.Atoi(ov); err == nil && n >= 0 {
			offset = n
		}
	}

	// sort
	var sortKeys []SortKey
	sv := strings.TrimSpace(q.Get("_sort"))
	if sv == "" {
		sv = strings.TrimSpace(q.Get("sort"))
	}
	if sv != "" {
		parts := strings.Split(sv, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			desc := false
			if strings.HasPrefix(p, "-") {
				desc = true
				p = strings.TrimPrefix(p, "-")
			} else if strings.HasPrefix(p, "+") {
				p = strings.TrimPrefix(p, "+")
			}
			if p != "" {
				sortKeys = append(sortKeys, SortKey{Field: p, Desc: desc})
			}
		}
	}

	// nulls
	nulls := strings.ToLower(strings.TrimSpace(q.Get("nulls")))
	if nulls != "first" && nulls != "last" {
		nulls = "last"
	}

	// фильтры (исключаем служебные ключи)
	filters := make(map[string][]string)
	for key, vals := range q {
		switch key {
		case "q", "offset", "limit", "sort", "order",
			"_offset", "_limit", "_sort", "_order",
			"nulls":
			continue
		}
		clean := make([]string, 0, len(vals))
		for _, v := range vals {
			if strings.TrimSpace(v) != "" {
				clean = append(clean, v)
			}
		}
		if len(clean) > 0 {
			filters[key] = clean
		}
	}

	return ListParams{
		Limit:   limit,
		Offset:  offset,
		Sort:    sortKeys,
		Filters: filters,
		Q:       strings.TrimSpace(q.Get("q")),
		Nulls:   nulls,
	}
}

// ==== Утилита ====

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ==== Сортировка с политикой nulls ====

func isNull(v any, ok bool) bool { return !ok || v == nil }

// сравнение двух записей по одному ключу с учётом nullsPolicy и направления
func cmpByKey(a, b *Record, key string, nullsPolicy string, desc bool) int {
	va, oka := a.Data[key]
	vb, okb := b.Data[key]

	na := isNull(va, oka)
	nb := isNull(vb, okb)

	// nulls first/last
	if na && nb {
		return 0
	}
	if na != nb {
		if nullsPolicy == "last" {
			if na {
				return +1 // a=null → в конец при asc
			}
			return -1
		}
		// nulls=first
		if na {
			return -1
		}
		return +1
	}

	// оба не null — сравним строково (как и было)
	sa := toString(va)
	sb := toString(vb)
	rel := 0
	if sa < sb {
		rel = -1
	} else if sa > sb {
		rel = +1
	}
	if desc {
		rel = -rel
	}
	return rel
}

// мультисортировка с учётом nullsPolicy
func sortRecordsMultiNulls(records []*Record, keys []SortKey, nullsPolicy string) {
	if len(keys) == 0 {
		return
	}
	type kspec struct {
		name string
		desc bool
	}
	specs := make([]kspec, 0, len(keys))
	for _, k := range keys {
		if k.Field == "" {
			continue
		}
		specs = append(specs, kspec{name: k.Field, desc: k.Desc})
	}

	sort.SliceStable(records, func(i, j int) bool {
		for _, s := range specs {
			if c := cmpByKey(records[i], records[j], s.name, nullsPolicy, s.desc); c != 0 {
				return c < 0
			}
		}
		return false
	})
}
