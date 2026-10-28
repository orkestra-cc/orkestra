package filter

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Compile validates f against schema (Validate first) and returns the engine
// form. A nil f returns (nil, nil). Every error wraps
// iface.ErrRetrievalInvalidFilter. engine.Filter.Field holds the bare declared
// name; the driver adds the "f." namespace, so a compiled filter can never
// address "_rt".
//
// Datetimes compile to Unix milliseconds UTC as float64, the representation
// engine.Range and the indexed Point.Fields share. Absence is written as a
// not node around an exists comparison (exists itself takes no value).
func Compile(f *iface.RetrievalFilter, schema []iface.RetrievalFilterField) (*engine.Filter, error) {
	if f == nil {
		return nil, nil
	}
	if err := Validate(f, schema); err != nil {
		return nil, err
	}
	types := make(map[string]iface.RetrievalFieldType, len(schema))
	for _, fld := range schema {
		types[fld.Name] = fld.Type
	}
	out, err := compileNode(f, types)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func compileNode(f *iface.RetrievalFilter, types map[string]iface.RetrievalFieldType) (engine.Filter, error) {
	switch {
	case len(f.And) > 0:
		kids, err := compileList(f.And, types)
		return engine.Filter{Must: kids}, err
	case len(f.Or) > 0:
		kids, err := compileList(f.Or, types)
		return engine.Filter{Should: kids}, err
	case f.Not != nil:
		kid, err := compileNode(f.Not, types)
		return engine.Filter{MustNot: []engine.Filter{kid}}, err
	}
	return compileLeaf(f, types[f.Field])
}

func compileList(list []iface.RetrievalFilter, types map[string]iface.RetrievalFieldType) ([]engine.Filter, error) {
	out := make([]engine.Filter, 0, len(list))
	for i := range list {
		c, err := compileNode(&list[i], types)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func compileLeaf(f *iface.RetrievalFilter, typ iface.RetrievalFieldType) (engine.Filter, error) {
	out := engine.Filter{Field: f.Field}
	switch f.Op {
	case iface.RetrievalOpExists:
		out.Exists = true
	case iface.RetrievalOpEq, iface.RetrievalOpIn:
		vals := f.Values
		if f.Op == iface.RetrievalOpEq {
			vals = []any{f.Value}
		}
		out.Match = make([]any, 0, len(vals))
		for _, v := range vals {
			m, err := matchValue(typ, v)
			if err != nil {
				return engine.Filter{}, badValue(f.Field, err)
			}
			out.Match = append(out.Match, m)
		}
	default:
		n, err := rangeValue(typ, f.Value)
		if err != nil {
			return engine.Filter{}, badValue(f.Field, err)
		}
		r := &engine.Range{}
		switch f.Op {
		case iface.RetrievalOpLt:
			r.Lt = &n
		case iface.RetrievalOpLte:
			r.Lte = &n
		case iface.RetrievalOpGt:
			r.Gt = &n
		case iface.RetrievalOpGte:
			r.Gte = &n
		default:
			return engine.Filter{}, badValue(f.Field, fmt.Errorf("unsupported op %q", f.Op))
		}
		out.Range = r
	}
	return out, nil
}

func badValue(field string, err error) error {
	return fmt.Errorf("%w: field %q: %s", iface.ErrRetrievalInvalidFilter, field, err)
}

// matchValue converts an eq/in operand to the form the engine matches on.
func matchValue(typ iface.RetrievalFieldType, v any) (any, error) {
	switch typ {
	case iface.RetrievalFieldKeyword, iface.RetrievalFieldKeywords:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("must be a string")
		}
		return s, nil
	case iface.RetrievalFieldBool:
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("must be a bool")
		}
		return b, nil
	case iface.RetrievalFieldInteger:
		return toInt64(v)
	}
	return nil, fmt.Errorf("eq/in is not supported on %s", typ)
}

// toInt64 converts every integer representation Validate accepts.
func toInt64(v any) (int64, error) {
	switch n := v.(type) {
	case int:
		return int64(n), nil
	case int8:
		return int64(n), nil
	case int16:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case int64:
		return n, nil
	case uint8:
		return int64(n), nil
	case uint16:
		return int64(n), nil
	case uint32:
		return int64(n), nil
	case uint:
		if uint64(n) > math.MaxInt64 {
			return 0, fmt.Errorf("integer out of range")
		}
		return int64(n), nil
	case uint64:
		if n > math.MaxInt64 {
			return 0, fmt.Errorf("integer out of range")
		}
		return int64(n), nil
	case float64:
		if n != math.Trunc(n) || math.Abs(n) > maxExactFloatInt {
			return 0, fmt.Errorf("must be an integer")
		}
		return int64(n), nil
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, fmt.Errorf("must be an integer")
		}
		return i, nil
	}
	return 0, fmt.Errorf("must be an integer")
}

// rangeValue converts a comparison operand to the float64 engine.Range holds:
// the number itself for integer and float fields, Unix milliseconds for
// datetimes. An integer bound beyond ±2^53 is refused rather than rounded.
func rangeValue(typ iface.RetrievalFieldType, v any) (float64, error) {
	var out float64
	switch typ {
	case iface.RetrievalFieldDatetime:
		var t time.Time
		switch d := v.(type) {
		case time.Time:
			t = d
		case string:
			p, err := time.Parse(time.RFC3339Nano, d)
			if err != nil {
				return 0, fmt.Errorf("must be an RFC 3339 datetime")
			}
			t = p
		default:
			return 0, fmt.Errorf("must be an RFC 3339 datetime")
		}
		return float64(t.UnixMilli()), nil
	case iface.RetrievalFieldInteger:
		if !isExactRangeInteger(v) {
			return 0, fmt.Errorf("an integer range bound must be within ±2^53")
		}
		i, _ := toInt64(v)
		return float64(i), nil
	case iface.RetrievalFieldFloat:
		n, err := numberToFloat(v)
		if err != nil {
			return 0, err
		}
		out = n
	default:
		return 0, fmt.Errorf("range is not supported on %s", typ)
	}
	if math.IsNaN(out) || math.IsInf(out, 0) {
		return 0, fmt.Errorf("number must be finite")
	}
	return out, nil
}

// numberToFloat reads the real value of any numeric kind Validate accepts.
func numberToFloat(v any) (float64, error) {
	switch n := v.(type) {
	case int:
		return float64(n), nil
	case int8:
		return float64(n), nil
	case int16:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case uint:
		return float64(n), nil
	case uint8:
		return float64(n), nil
	case uint16:
		return float64(n), nil
	case uint32:
		return float64(n), nil
	case uint64:
		return float64(n), nil
	case float32:
		return float64(n), nil
	case float64:
		return n, nil
	case json.Number:
		f, err := strconv.ParseFloat(string(n), 64)
		if err != nil {
			return 0, fmt.Errorf("must be a number")
		}
		return f, nil
	}
	return 0, fmt.Errorf("must be a number")
}
