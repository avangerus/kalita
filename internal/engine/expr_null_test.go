package engine

import "testing"

func TestNullPresence(t *testing.T) {
	cases := []struct {
		expr string
		vals map[string]any
		want bool
	}{
		// filled field
		{"certificate != null", map[string]any{"certificate": "blob:abc"}, true},
		{"certificate = null", map[string]any{"certificate": "blob:abc"}, false},
		// absent field
		{"certificate != null", map[string]any{}, false},
		{"certificate = null", map[string]any{}, true},
		// explicit nil value counts as null
		{"certificate = null", map[string]any{"certificate": nil}, true},
		// combined with other conditions
		{"status = Active and manager != null", map[string]any{"status": "Active", "manager": "u1"}, true},
		{"status = Active and manager != null", map[string]any{"status": "Active"}, false},
	}
	for _, tc := range cases {
		got := evalWhere(tc.expr, evalCtx{values: tc.vals})
		if got != tc.want {
			t.Errorf("evalWhere(%q, %v) = %v, want %v", tc.expr, tc.vals, got, tc.want)
		}
	}
}
