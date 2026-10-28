package filter

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

var testSchema = []iface.RetrievalFilterField{
	{Name: "status", Type: iface.RetrievalFieldKeyword},
	{Name: "tags", Type: iface.RetrievalFieldKeywords},
	{Name: "score", Type: iface.RetrievalFieldInteger},
	{Name: "ratio", Type: iface.RetrievalFieldFloat},
	{Name: "active", Type: iface.RetrievalFieldBool},
	{Name: "created", Type: iface.RetrievalFieldDatetime},
}

func cmp() iface.RetrievalFilter {
	return iface.RetrievalFilter{Field: "status", Op: iface.RetrievalOpEq, Value: "open"}
}

func nested(depth int) iface.RetrievalFilter {
	f := cmp()
	for i := 0; i < depth; i++ {
		f = iface.RetrievalFilter{And: []iface.RetrievalFilter{f}}
	}
	return f
}

func manyComparisons(n int) iface.RetrievalFilter {
	f := iface.RetrievalFilter{}
	for i := 0; i < n; i++ {
		f.And = append(f.And, cmp())
	}
	return f
}

func strs(n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = "v"
	}
	return out
}

func ptr(f iface.RetrievalFilter) *iface.RetrievalFilter { return &f }

func TestValidate(t *testing.T) {
	bad := iface.RetrievalFilter{Field: "owner", Op: iface.RetrievalOpEq, Value: "x"}
	tests := []struct {
		name string
		f    *iface.RetrievalFilter
		want string // "" = valid
	}{
		{"nil", nil, ""},
		{"eq keyword", ptr(cmp()), ""},
		{"undeclared", &bad, `field "owner" is not declared`},
		{"reserved", &iface.RetrievalFilter{Field: "_rt.tenantId", Op: "eq", Value: "t"}, "is not declared"},
		{"namespaced", &iface.RetrievalFilter{Field: "f.status", Op: "eq", Value: "open"}, "is not declared"},
		{"tenantId", &iface.RetrievalFilter{Field: "tenantId", Op: "eq", Value: "t"}, "is not declared"},
		{"depth 4", ptr(nested(3)), ""},
		{"depth 5", ptr(nested(4)), "depth"},
		{"not depth 5", ptr(iface.RetrievalFilter{Not: ptr(iface.RetrievalFilter{Not: ptr(iface.RetrievalFilter{Not: ptr(iface.RetrievalFilter{Not: ptr(cmp())})})})}), "depth"},
		{"64 nodes", ptr(manyComparisons(63)), ""},
		{"65 nodes", ptr(manyComparisons(64)), "nodes"},
		{"in 100", &iface.RetrievalFilter{Field: "status", Op: "in", Values: strs(100)}, ""},
		{"in 101", &iface.RetrievalFilter{Field: "status", Op: "in", Values: strs(101)}, "values"},
		{"in empty", &iface.RetrievalFilter{Field: "status", Op: "in"}, "values"},
		{"in with value", &iface.RetrievalFilter{Field: "status", Op: "in", Value: "a", Values: strs(1)}, "value"},
		{"eq with values", &iface.RetrievalFilter{Field: "status", Op: "eq", Value: "a", Values: strs(1)}, "values"},
		{"eq without value", &iface.RetrievalFilter{Field: "status", Op: "eq"}, "value"},
		{"keyword[] eq", &iface.RetrievalFilter{Field: "tags", Op: "eq", Value: "a"}, ""},
		{"eq float", &iface.RetrievalFilter{Field: "ratio", Op: "eq", Value: 0.5}, `op "eq" is not allowed on float field "ratio"`},
		{"range keyword", &iface.RetrievalFilter{Field: "status", Op: "gt", Value: "a"}, "not allowed"},
		{"unknown op", &iface.RetrievalFilter{Field: "status", Op: "like", Value: "a"}, "op"},
		{"integer 1.5", &iface.RetrievalFilter{Field: "score", Op: "gte", Value: 1.5}, "integer"},
		{"integer 3.0", &iface.RetrievalFilter{Field: "score", Op: "gte", Value: float64(3)}, ""},
		{"integer json.Number", &iface.RetrievalFilter{Field: "score", Op: "gte", Value: json.Number("3")}, ""},
		{"integer json.Number frac", &iface.RetrievalFilter{Field: "score", Op: "gte", Value: json.Number("3.5")}, "integer"},
		{"integer int32", &iface.RetrievalFilter{Field: "score", Op: "eq", Value: int32(3)}, ""},
		{"integer too big float", &iface.RetrievalFilter{Field: "score", Op: "eq", Value: float64(1 << 60)}, "integer"},
		{"integer string", &iface.RetrievalFilter{Field: "score", Op: "eq", Value: "3"}, "integer"},
		// engine.Range is float64: a range bound must be an exact float64 (P2, 4401533393).
		{"integer range 2^53", &iface.RetrievalFilter{Field: "score", Op: "lte", Value: int64(1 << 53)}, ""},
		{"integer range -2^53", &iface.RetrievalFilter{Field: "score", Op: "gt", Value: int64(-1 << 53)}, ""},
		{"integer range 2^53+1", &iface.RetrievalFilter{Field: "score", Op: "gt", Value: int64(1<<53 + 1)}, "2^53"},
		{"integer range -(2^53+1)", &iface.RetrievalFilter{Field: "score", Op: "lt", Value: int64(-1<<53 - 1)}, "2^53"},
		{"integer range uint64 2^53+1", &iface.RetrievalFilter{Field: "score", Op: "gte", Value: uint64(1<<53 + 1)}, "2^53"},
		{"integer range json.Number 2^53", &iface.RetrievalFilter{Field: "score", Op: "lt", Value: json.Number("9007199254740992")}, ""},
		{"integer range json.Number 2^53+1", &iface.RetrievalFilter{Field: "score", Op: "lt", Value: json.Number("9007199254740993")}, "2^53"},
		{"integer eq 2^53+1", &iface.RetrievalFilter{Field: "score", Op: "eq", Value: int64(1<<53 + 1)}, ""},
		{"integer in 2^53+1", &iface.RetrievalFilter{Field: "score", Op: "in", Values: []any{json.Number("9007199254740993")}}, ""},
		{"float int", &iface.RetrievalFilter{Field: "ratio", Op: "lt", Value: 3}, ""},
		{"float json.Number", &iface.RetrievalFilter{Field: "ratio", Op: "lt", Value: json.Number("0.5")}, ""},
		{"NaN", &iface.RetrievalFilter{Field: "ratio", Op: "lt", Value: math.NaN()}, "finite"},
		{"Inf", &iface.RetrievalFilter{Field: "ratio", Op: "lt", Value: math.Inf(1)}, "finite"},
		{"datetime", &iface.RetrievalFilter{Field: "created", Op: "gt", Value: "2026-10-26T10:00:00+02:00"}, ""},
		{"datetime time.Time", &iface.RetrievalFilter{Field: "created", Op: "gt", Value: time.Now()}, ""},
		{"datetime bad", &iface.RetrievalFilter{Field: "created", Op: "gt", Value: "yesterday"}, "RFC 3339"},
		{"bool ok", &iface.RetrievalFilter{Field: "active", Op: "eq", Value: true}, ""},
		{"bool string", &iface.RetrievalFilter{Field: "active", Op: "eq", Value: "true"}, "bool"},
		{"exists ok", &iface.RetrievalFilter{Field: "ratio", Op: "exists"}, ""},
		{"exists with value", &iface.RetrievalFilter{Field: "ratio", Op: "exists", Value: 1}, "exists"},
		{"two kinds", &iface.RetrievalFilter{And: []iface.RetrievalFilter{cmp()}, Field: "status"}, "exactly one"},
		{"and+or", &iface.RetrievalFilter{And: []iface.RetrievalFilter{cmp()}, Or: []iface.RetrievalFilter{cmp()}}, "exactly one"},
		{"logical with op", &iface.RetrievalFilter{And: []iface.RetrievalFilter{cmp()}, Op: "eq"}, "must not set"},
		{"empty node", &iface.RetrievalFilter{}, "exactly one"},
		{"empty and", &iface.RetrievalFilter{And: []iface.RetrievalFilter{}}, "exactly one"},
		{"not ok", &iface.RetrievalFilter{Not: ptr(cmp())}, ""},
		{"long keyword", &iface.RetrievalFilter{Field: "status", Op: "eq", Value: strings.Repeat("a", 1025)}, "1024"},
		{"empty keyword", &iface.RetrievalFilter{Field: "status", Op: "eq", Value: ""}, "1024"},
		{"path", &iface.RetrievalFilter{And: []iface.RetrievalFilter{cmp(), {Or: []iface.RetrievalFilter{bad}}}}, "and[1].or[0]"},
		{"path not", &iface.RetrievalFilter{Not: &bad}, "not"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.f, testSchema)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.want)
			}
			if !errors.Is(err, iface.ErrRetrievalInvalidFilter) {
				t.Fatalf("error does not wrap ErrRetrievalInvalidFilter: %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}
