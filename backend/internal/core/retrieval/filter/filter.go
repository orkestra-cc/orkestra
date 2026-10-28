// Package filter owns consumer filters for the retrieval module: validation
// against a corpus's declared filter fields (Validate, PR 3) and compilation to
// the engine's *engine.Filter (Compile, PR 5). Both live here so a filter is
// never compiled without having been validated.
package filter

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Limits on a filter tree.
const (
	MaxDepth       = 4
	MaxNodes       = 64
	MaxValues      = 100
	MaxStringBytes = 1024
)

// maxExactFloatInt is 2^53, the largest magnitude at which a float64 holds
// every integer exactly.
const maxExactFloatInt = 1 << 53

// allowedOps lists, per field type, the operators a comparison may use.
var allowedOps = map[iface.RetrievalFieldType][]iface.RetrievalOp{
	iface.RetrievalFieldKeyword:  {iface.RetrievalOpEq, iface.RetrievalOpIn, iface.RetrievalOpExists},
	iface.RetrievalFieldKeywords: {iface.RetrievalOpEq, iface.RetrievalOpIn, iface.RetrievalOpExists},
	iface.RetrievalFieldInteger: {iface.RetrievalOpEq, iface.RetrievalOpIn, iface.RetrievalOpLt,
		iface.RetrievalOpLte, iface.RetrievalOpGt, iface.RetrievalOpGte, iface.RetrievalOpExists},
	iface.RetrievalFieldFloat: {iface.RetrievalOpLt, iface.RetrievalOpLte, iface.RetrievalOpGt,
		iface.RetrievalOpGte, iface.RetrievalOpExists},
	iface.RetrievalFieldBool: {iface.RetrievalOpEq, iface.RetrievalOpExists},
	iface.RetrievalFieldDatetime: {iface.RetrievalOpLt, iface.RetrievalOpLte, iface.RetrievalOpGt,
		iface.RetrievalOpGte, iface.RetrievalOpExists},
}

// Validate checks f against the corpus schema; nil f is valid. Every error wraps
// iface.ErrRetrievalInvalidFilter and names the node path, e.g. `and[1].or[0]`.
func Validate(f *iface.RetrievalFilter, schema []iface.RetrievalFilterField) error {
	if f == nil {
		return nil
	}
	types := make(map[string]iface.RetrievalFieldType, len(schema))
	for _, fld := range schema {
		types[fld.Name] = fld.Type
	}
	w := walker{types: types}
	return w.node(f, "", 1)
}

type walker struct {
	types map[string]iface.RetrievalFieldType
	nodes int
}

func fail(path, format string, args ...any) error {
	if path == "" {
		path = "(root)"
	}
	return fmt.Errorf("%w: %s: %s", iface.ErrRetrievalInvalidFilter, path, fmt.Sprintf(format, args...))
}

func join(path, seg string) string {
	if path == "" {
		return seg
	}
	return path + "." + seg
}

func (w *walker) node(f *iface.RetrievalFilter, path string, depth int) error {
	if depth > MaxDepth {
		return fail(path, "depth exceeds %d", MaxDepth)
	}
	w.nodes++
	if w.nodes > MaxNodes {
		return fail(path, "more than %d nodes", MaxNodes)
	}

	kinds := 0
	if len(f.And) > 0 {
		kinds++
	}
	if len(f.Or) > 0 {
		kinds++
	}
	if f.Not != nil {
		kinds++
	}
	if f.Field != "" {
		kinds++
	}
	if kinds != 1 {
		return fail(path, "a node must set exactly one of and, or, not, or a comparison")
	}

	if f.Field == "" {
		if f.Op != "" || f.Value != nil || len(f.Values) > 0 {
			return fail(path, "a logical node must not set op, value or values")
		}
		switch {
		case len(f.And) > 0:
			return w.children(f.And, "and", path, depth)
		case len(f.Or) > 0:
			return w.children(f.Or, "or", path, depth)
		default:
			return w.node(f.Not, join(path, "not"), depth+1)
		}
	}
	return w.comparison(f, path)
}

func (w *walker) children(list []iface.RetrievalFilter, name, path string, depth int) error {
	for i := range list {
		if err := w.node(&list[i], join(path, fmt.Sprintf("%s[%d]", name, i)), depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (w *walker) comparison(f *iface.RetrievalFilter, path string) error {
	typ, ok := w.types[f.Field]
	if !ok {
		return fail(path, "field %q is not declared", f.Field)
	}
	allowed := false
	for _, op := range allowedOps[typ] {
		if op == f.Op {
			allowed = true
			break
		}
	}
	if !allowed {
		return fail(path, "op %q is not allowed on %s field %q", f.Op, typ, f.Field)
	}

	switch f.Op {
	case iface.RetrievalOpExists:
		if f.Value != nil || len(f.Values) > 0 {
			return fail(path, "op \"exists\" takes no value or values")
		}
	case iface.RetrievalOpIn:
		if f.Value != nil {
			return fail(path, "op \"in\" takes values, not value")
		}
		if len(f.Values) < 1 || len(f.Values) > MaxValues {
			return fail(path, "op \"in\" needs 1 to %d values, got %d", MaxValues, len(f.Values))
		}
		for i, v := range f.Values {
			if err := checkValue(typ, v); err != nil {
				return fail(path, "values[%d]: %s", i, err)
			}
		}
	default: // eq and range operators
		if len(f.Values) > 0 {
			return fail(path, "op %q takes value, not values", f.Op)
		}
		if f.Value == nil {
			return fail(path, "op %q needs a value", f.Op)
		}
		if err := checkValue(typ, f.Value); err != nil {
			return fail(path, "value: %s", err)
		}
	}
	return nil
}

type valueError string

func (e valueError) Error() string { return string(e) }

func checkValue(typ iface.RetrievalFieldType, v any) error {
	switch typ {
	case iface.RetrievalFieldKeyword, iface.RetrievalFieldKeywords:
		s, ok := v.(string)
		if !ok {
			return valueError("must be a string")
		}
		if len(s) < 1 || len(s) > MaxStringBytes {
			return valueError(fmt.Sprintf("string must be 1 to %d bytes", MaxStringBytes))
		}
	case iface.RetrievalFieldInteger:
		if !isInteger(v) {
			return valueError("must be an integer")
		}
	case iface.RetrievalFieldFloat:
		f, ok := asFloat(v)
		if !ok {
			return valueError("must be a number")
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return valueError("number must be finite")
		}
	case iface.RetrievalFieldBool:
		if _, ok := v.(bool); !ok {
			return valueError("must be a bool")
		}
	case iface.RetrievalFieldDatetime:
		switch t := v.(type) {
		case time.Time:
		case string:
			if _, err := time.Parse(time.RFC3339Nano, t); err != nil {
				return valueError("must be an RFC 3339 datetime")
			}
		default:
			return valueError("must be an RFC 3339 datetime string or time.Time")
		}
	default:
		return valueError(fmt.Sprintf("unsupported field type %q", typ))
	}
	return nil
}

func isInteger(v any) bool {
	switch n := v.(type) {
	case int, int8, int16, int32, int64, uint8, uint16, uint32:
		return true
	case uint:
		return uint64(n) <= math.MaxInt64
	case uint64:
		return n <= math.MaxInt64
	case float64:
		return n == math.Trunc(n) && math.Abs(n) <= maxExactFloatInt
	case json.Number:
		_, err := n.Int64()
		return err == nil
	}
	return false
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return 0, true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case json.Number:
		f, err := strconv.ParseFloat(string(n), 64)
		if err != nil && !math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	}
	return 0, false
}
