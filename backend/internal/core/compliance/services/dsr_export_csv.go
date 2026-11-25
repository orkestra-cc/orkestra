package services

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/orkestra/backend/internal/shared/csvsafe"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// FlattenBundleCSV writes the bundle as CSV rows `producer,path,value`: one row
// per scalar, the metadata section first, then the producers by key. Inside a
// producer, map keys are walked in sorted order and arrays by index, so the
// output is deterministic. A path is dot-joined keys with `[i]` for array
// positions; empty maps and arrays get one row holding `{}` / `[]`; null is an
// empty value. Every cell goes through csvsafe, because the file is opened in
// a spreadsheet.
func FlattenBundleCSV(w io.Writer, bundle iface.PersonalDataBundle) error {
	// A JSON round trip turns structs and times into the generic shapes the
	// walk handles; UseNumber keeps integers exact.
	raw, err := json.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("flatten bundle: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic map[string]any
	if err := dec.Decode(&generic); err != nil {
		return fmt.Errorf("flatten bundle: %w", err)
	}

	keys := make([]string, 0, len(generic))
	for k := range generic {
		if k != bundleMetadataKey {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if _, ok := generic[bundleMetadataKey]; ok {
		keys = append([]string{bundleMetadataKey}, keys...)
	}

	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"producer", "path", "value"}); err != nil {
		return err
	}
	for _, k := range keys {
		var rows [][2]string
		flattenValue("", generic[k], &rows)
		for _, r := range rows {
			if err := cw.Write(csvsafe.Row([]string{k, r[0], r[1]})); err != nil {
				return err
			}
		}
	}
	cw.Flush()
	return cw.Error()
}

func flattenValue(path string, v any, rows *[][2]string) {
	switch t := v.(type) {
	case nil:
		*rows = append(*rows, [2]string{path, ""})
	case map[string]any:
		if len(t) == 0 {
			*rows = append(*rows, [2]string{path, "{}"})
			return
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := k
			if path != "" {
				p = path + "." + k
			}
			flattenValue(p, t[k], rows)
		}
	case []any:
		if len(t) == 0 {
			*rows = append(*rows, [2]string{path, "[]"})
			return
		}
		for i, e := range t {
			flattenValue(path+"["+strconv.Itoa(i)+"]", e, rows)
		}
	case json.Number:
		*rows = append(*rows, [2]string{path, t.String()})
	case string:
		*rows = append(*rows, [2]string{path, t})
	case bool:
		*rows = append(*rows, [2]string{path, strconv.FormatBool(t)})
	default:
		*rows = append(*rows, [2]string{path, fmt.Sprint(t)})
	}
}
