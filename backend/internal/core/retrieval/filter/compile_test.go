package filter

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

var compileSchema = []iface.RetrievalFilterField{
	{Name: "team", Type: iface.RetrievalFieldKeyword},
	{Name: "tags", Type: iface.RetrievalFieldKeywords},
	{Name: "seniority", Type: iface.RetrievalFieldInteger},
	{Name: "score", Type: iface.RetrievalFieldFloat},
	{Name: "active", Type: iface.RetrievalFieldBool},
	{Name: "since", Type: iface.RetrievalFieldDatetime},
}

func f64(v float64) *float64 { return &v }

func leaf(field string, op iface.RetrievalOp, v any) iface.RetrievalFilter {
	return iface.RetrievalFilter{Field: field, Op: op, Value: v}
}

func leafIn(field string, vs ...any) iface.RetrievalFilter {
	return iface.RetrievalFilter{Field: field, Op: iface.RetrievalOpIn, Values: vs}
}

func TestCompile_NilFilterIsNil(t *testing.T) {
	got, err := Compile(nil, compileSchema)
	if got != nil || err != nil {
		t.Fatalf("got %v, %v; want nil, nil", got, err)
	}
}

func TestCompile_Comparisons(t *testing.T) {
	ms := float64(time.Date(2026, 1, 2, 1, 4, 5, 0, time.UTC).UnixMilli())
	tests := []struct {
		name string
		in   iface.RetrievalFilter
		want engine.Filter
	}{
		{"keyword eq", leaf("team", "eq", "sales"), engine.Filter{Field: "team", Match: []any{"sales"}}},
		{"keyword in", leafIn("team", "a", "b"), engine.Filter{Field: "team", Match: []any{"a", "b"}}},
		{"keywords eq", leaf("tags", "eq", "vip"), engine.Filter{Field: "tags", Match: []any{"vip"}}},
		{"integer eq float64", leaf("seniority", "eq", 3.0), engine.Filter{Field: "seniority", Match: []any{int64(3)}}},
		{"integer eq json.Number", leaf("seniority", "eq", json.Number("7")), engine.Filter{Field: "seniority", Match: []any{int64(7)}}},
		{"integer eq uint64", leaf("seniority", "eq", uint64(9)), engine.Filter{Field: "seniority", Match: []any{int64(9)}}},
		{"integer in", leafIn("seniority", 1, int32(2), 3.0), engine.Filter{Field: "seniority", Match: []any{int64(1), int64(2), int64(3)}}},
		{"integer gte", leaf("seniority", "gte", 2), engine.Filter{Field: "seniority", Range: &engine.Range{Gte: f64(2)}}},
		{"integer gt uint64", leaf("seniority", "gt", uint64(5)), engine.Filter{Field: "seniority", Range: &engine.Range{Gt: f64(5)}}},
		{"float lt", leaf("score", "lt", 1.5), engine.Filter{Field: "score", Range: &engine.Range{Lt: f64(1.5)}}},
		{"float lte int", leaf("score", "lte", 2), engine.Filter{Field: "score", Range: &engine.Range{Lte: f64(2)}}},
		{"float gt json.Number", leaf("score", "gt", json.Number("0.25")), engine.Filter{Field: "score", Range: &engine.Range{Gt: f64(0.25)}}},
		{"bool eq", leaf("active", "eq", true), engine.Filter{Field: "active", Match: []any{true}}},
		{"datetime gt offset", leaf("since", "gt", "2026-01-02T03:04:05+02:00"), engine.Filter{Field: "since", Range: &engine.Range{Gt: f64(ms)}}},
		{"datetime lte fraction", leaf("since", "lte", "2026-01-02T01:04:05.5Z"), engine.Filter{Field: "since", Range: &engine.Range{Lte: f64(ms + 500)}}},
		{"datetime time.Time", leaf("since", "gte", time.Date(2026, 1, 2, 1, 4, 5, 0, time.UTC)), engine.Filter{Field: "since", Range: &engine.Range{Gte: f64(ms)}}},
		{"exists", iface.RetrievalFilter{Field: "team", Op: "exists"}, engine.Filter{Field: "team", Exists: true}},
		{"integer lte 2^53", leaf("seniority", "lte", int64(1<<53)), engine.Filter{Field: "seniority", Range: &engine.Range{Lte: f64(1 << 53)}}},
		{"integer gte -2^53 json.Number", leaf("seniority", "gte", json.Number("-9007199254740992")), engine.Filter{Field: "seniority", Range: &engine.Range{Gte: f64(-1 << 53)}}},
		{"integer eq 2^53+1 stays exact", leaf("seniority", "eq", int64(1<<53+1)), engine.Filter{Field: "seniority", Match: []any{int64(1<<53 + 1)}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in
			got, err := Compile(&in, compileSchema)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, &tc.want) {
				t.Fatalf("got %+v, want %+v", got, &tc.want)
			}
		})
	}
}

func TestCompile_Groups(t *testing.T) {
	a, b, c := leaf("team", "eq", "a"), leaf("team", "eq", "b"), leaf("tags", "eq", "c")
	in := iface.RetrievalFilter{And: []iface.RetrievalFilter{
		{Or: []iface.RetrievalFilter{a, b}},
		{Not: &c},
	}}
	A := engine.Filter{Field: "team", Match: []any{"a"}}
	B := engine.Filter{Field: "team", Match: []any{"b"}}
	C := engine.Filter{Field: "tags", Match: []any{"c"}}
	want := &engine.Filter{Must: []engine.Filter{{Should: []engine.Filter{A, B}}, {MustNot: []engine.Filter{C}}}}
	got, err := Compile(&in, compileSchema)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestCompile_Rejects(t *testing.T) {
	deep := nested(4)
	many := manyComparisons(64)
	tooMany := iface.RetrievalFilter{Field: "team", Op: "in", Values: strs(101)}
	both := iface.RetrievalFilter{Field: "team", Op: "eq", Value: "x", And: []iface.RetrievalFilter{leaf("team", "eq", "y")}}
	tests := []struct {
		name string
		in   iface.RetrievalFilter
	}{
		{"undeclared", leaf("salary", "eq", "x")},
		{"reserved", leaf("_rt.tenantId", "eq", "t")},
		{"keyword lt", leaf("team", "lt", "x")},
		{"bool gt", leaf("active", "gt", true)},
		{"keyword eq number", leaf("team", "eq", 3)},
		{"integer non-integral", leaf("seniority", "eq", 3.5)},
		{"integer out of range", leaf("seniority", "eq", 1e19)},
		{"float NaN", leaf("score", "lt", math.NaN())},
		{"float Inf", leaf("score", "lt", math.Inf(1))},
		{"float eq", leaf("score", "eq", 2.5)},
		{"datetime eq", leaf("since", "eq", "2026-01-02T01:04:05Z")},
		{"in empty", iface.RetrievalFilter{Field: "team", Op: "in"}},
		{"in with Value", leaf("team", "in", "a")},
		{"bad datetime", leaf("since", "gt", "yesterday")},
		{"and plus field", both},
		{"depth 5", deep},
		{"65 nodes", many},
		{"101 values", tooMany},
		{"exists string", leaf("team", "exists", "yes")},
		{"exists false", leaf("team", "exists", false)},
		{"exists true", leaf("team", "exists", true)},
		{"integer gt 2^53+1", leaf("seniority", "gt", int64(1<<53+1))},
		{"integer lt -(2^53+1)", leaf("seniority", "lt", int64(-1<<53-1))},
		{"integer gte uint64 max", leaf("seniority", "gte", uint64(math.MaxInt64))},
		{"integer lte json.Number 2^53+1", leaf("seniority", "lte", json.Number("9007199254740993"))},
		{"integer gt json.Number -(2^53+1)", leaf("seniority", "gt", json.Number("-9007199254740993"))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in
			got, err := Compile(&in, compileSchema)
			if !errors.Is(err, iface.ErrRetrievalInvalidFilter) {
				t.Fatalf("err = %v, want ErrRetrievalInvalidFilter", err)
			}
			if got != nil {
				t.Fatalf("got %+v, want nil", got)
			}
		})
	}
}

func TestCompile_FieldNamesAreBare(t *testing.T) {
	absent := leaf("team", "exists", nil)
	in := iface.RetrievalFilter{And: []iface.RetrievalFilter{
		leaf("team", "eq", "a"),
		{Or: []iface.RetrievalFilter{leaf("seniority", "gte", 2), {Not: &absent}}},
		leaf("since", "gt", "2026-01-02T01:04:05Z"),
	}}
	got, err := Compile(&in, compileSchema)
	if err != nil {
		t.Fatal(err)
	}
	var walk func(f engine.Filter)
	walk = func(f engine.Filter) {
		if strings.Contains(f.Field, ".") {
			t.Errorf("field %q is namespaced", f.Field)
		}
		for _, list := range [][]engine.Filter{f.Must, f.Should, f.MustNot} {
			for _, c := range list {
				walk(c)
			}
		}
	}
	walk(*got)
}
