package dsl

import "testing"

// ParseAggregateBody is the ONE parser for an aggregate call's where-clause,
// shared by semantic.go (compile-time validation) and
// internal/engine/computed.go's evalAggregate (runtime evaluation) — see
// aggregate.go. This test pins down its verdict on both the supported form
// and the forms that must fail, independent of full pack compilation.
func TestParseAggregateBody(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantEntity string
		wantField  string
		wantRef    string
		wantOK     bool
	}{
		{"count, no field", "Finding where contract = $self", "Finding", "", "contract", true},
		{"sum, dotted field", "OrderLine . line_total where order = $self", "OrderLine", "line_total", "order", true},
		{"trailing close paren tolerated", "Finding where contract = $self)", "Finding", "", "contract", true},
		{"missing where", "Finding", "", "", "", false},
		{"compound and", "Finding where contract = $self and status = Open", "Finding", "", "", false},
		{"reversed operands", "Finding where $self = contract", "Finding", "", "", false},
		{"no equality at all", "Finding where contract", "Finding", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entity, field, ref, ok := ParseAggregateBody(tc.body)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (entity=%q field=%q ref=%q)", ok, tc.wantOK, entity, field, ref)
			}
			if !ok {
				return // the fields returned alongside a false ok carry no contract
			}
			if entity != tc.wantEntity || field != tc.wantField || ref != tc.wantRef {
				t.Fatalf("got (%q,%q,%q), want (%q,%q,%q)", entity, field, ref, tc.wantEntity, tc.wantField, tc.wantRef)
			}
		})
	}
}

// findAggregateCalls must locate an aggregate call embedded inside a larger
// arithmetic expression (e.g. `annual_quota - sum(...)`, packs/hr/hr.dsl's
// remaining_days) — not just a bare "count(...)" computed field.
func TestFindAggregateCalls(t *testing.T) {
	expr := "annual_quota - sum ( LeaveRequest . days where employee = $self )"
	bodies := findAggregateCalls(expr)
	if len(bodies) != 1 {
		t.Fatalf("want 1 aggregate call, got %d: %v", len(bodies), bodies)
	}
	if _, _, _, ok := ParseAggregateBody(bodies[0]); !ok {
		t.Fatalf("expected the extracted body to parse as the supported form: %q", bodies[0])
	}
}
