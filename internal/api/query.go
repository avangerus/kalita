package api

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// ---- СТАРЫЙ парсер (если где-то ещё используется) ----

type Query struct {
	Limit  int
	Offset int
	Sort   []SortKey         // e.g. ["-created_at", "name"]
	Q      string            // простой полнотекстовый по строковым полям
	Eq     map[string]string // точные фильтры: ?email=...&role=...
}

type SortKey struct {
	Field string
	Desc  bool
}

func parseQuery(v url.Values) Query {
	q := Query{
		Limit:  50,
		Offset: 0,
		Eq:     map[string]string{},
	}
	if l := v.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 1000 {
			q.Limit = n
		}
	}
	if o := v.Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			q.Offset = n
		}
	}
	if s := v.Get("sort"); s != "" {
		parts := strings.Split(s, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			desc := strings.HasPrefix(p, "-")
			field := strings.TrimPrefix(p, "-")
			q.Sort = append(q.Sort, SortKey{Field: field, Desc: desc})
		}
	}
	q.Q = v.Get("q")

	// остальные параметры считаем равенствами (кроме служебных)
	for key := range v {
		if key == "limit" || key == "offset" || key == "sort" || key == "q" {
			continue
		}
		q.Eq[key] = v.Get(key)
	}
	return q
}

func applySort(records []map[string]interface{}, sortKeys []SortKey) {
	if len(sortKeys) == 0 {
		return
	}
	sort.SliceStable(records, func(i, j int) bool {
		a := records[i]
		b := records[j]
		for _, k := range sortKeys {
			av, bv := a[k.Field], b[k.Field]
			// сравнение как строк (достаточно на старте)
			as := toString(av)
			bs := toString(bv)
			if as == bs {
				continue
			}
			if k.Desc {
				return as > bs
			}
			return as < bs
		}
		return false
	})
}

// ЕДИНСТВЕННАЯ версия toString (убрали дубликаты)
func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		// безопасное строковое представление без переводов строк
		return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(fmt.Sprintf("%v", v), "\n", " "), "\t", " "))
	}
}

// ---- НОВЫЙ парсер параметров списка ----

type ListParams struct {
	Limit   int
	Offset  int
	Sort    []SortKey // мульти-сорт
	Filters map[string][]string
	Q       string
}

func parseListParams(q url.Values) ListParams {
	limit := 50
	offset := 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 1000 {
			limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	// мульти-сорт: sort=-updated_at,name
	var sortKeys []SortKey
	if s := strings.TrimSpace(q.Get("sort")); s != "" {
		parts := strings.Split(s, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			desc := strings.HasPrefix(p, "-")
			field := strings.TrimPrefix(p, "-")
			sortKeys = append(sortKeys, SortKey{Field: field, Desc: desc})
		}
	}

	// фильтры: все ключи кроме служебных
	filters := make(map[string][]string)
	for k, vals := range q {
		if k == "limit" || k == "offset" || k == "sort" || k == "q" {
			continue
		}
		dst := make([]string, 0, len(vals))
		for _, v := range vals {
			v = strings.TrimSpace(v)
			if v != "" {
				dst = append(dst, v)
			}
		}
		if len(dst) > 0 {
			filters[k] = dst
		}
	}

	return ListParams{
		Limit:   limit,
		Offset:  offset,
		Sort:    sortKeys,
		Filters: filters,
		Q:       strings.TrimSpace(q.Get("q")),
	}
}

func sortRecordsMulti(records []*Record, keys []SortKey) {
	if len(keys) == 0 {
		return
	}
	less := func(a, b any) int {
		as := toString(a)
		bs := toString(b)
		if as < bs {
			return -1
		}
		if as > bs {
			return 1
		}
		return 0
	}
	sort.Slice(records, func(i, j int) bool {
		for _, k := range keys {
			cmp := less(records[i].Data[k.Field], records[j].Data[k.Field])
			if cmp == 0 {
				continue
			}
			if k.Desc {
				return cmp > 0
			}
			return cmp < 0
		}
		return false
	})
}
