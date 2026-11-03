package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/voska/close/internal/errfmt"
)

func Lookup(v map[string]any, field string) (any, bool) {
	if x, ok := v[field]; ok {
		return x, true
	}
	parts := strings.SplitN(field, ".", 2)
	if len(parts) == 2 {
		if child, ok := v[parts[0]].(map[string]any); ok {
			return Lookup(child, parts[1])
		}
	}
	return nil, false
}

func Project(v map[string]any, fields []string) map[string]any {
	if len(fields) == 0 {
		return v
	}
	project := func(row map[string]any) map[string]any {
		p := map[string]any{}
		for _, f := range fields {
			if x, ok := Lookup(row, f); ok {
				p[f] = x
			}
		}
		return p
	}
	if rows, ok := v["data"].([]any); ok {
		p := map[string]any{}
		for k, x := range v {
			p[k] = x
		}
		data := make([]any, 0, len(rows))
		for _, row := range rows {
			if m, ok := row.(map[string]any); ok {
				data = append(data, project(m))
			} else {
				data = append(data, row)
			}
		}
		p["data"] = data
		return p
	}
	return project(v)
}

func Write(w io.Writer, v map[string]any, mode string, fields []string, quiet bool) error {
	rows := []any{v}
	if data, ok := v["data"].([]any); ok {
		rows = data
	}
	if quiet {
		for _, row := range rows {
			m, ok := row.(map[string]any)
			if !ok {
				return errfmt.New(2, "usage", "--quiet requires object results")
			}
			id, ok := m["id"]
			if !ok {
				id, ok = m["version"]
			}
			if !ok {
				return errfmt.New(2, "usage", "--quiet requires an id or version field")
			}
			if _, err := fmt.Fprintln(w, cell(id)); err != nil {
				return err
			}
		}
		return nil
	}
	v = Project(v, fields)
	if mode == "json" {
		e := json.NewEncoder(w)
		e.SetIndent("", "  ")
		return e.Encode(v)
	}
	if mode == "ndjson" {
		rows = []any{v}
		if data, ok := v["data"].([]any); ok {
			rows = data
		}
		e := json.NewEncoder(w)
		for _, row := range rows {
			if err := e.Encode(row); err != nil {
				return err
			}
		}
		return nil
	}
	if mode != "tsv" && mode != "table" {
		return errfmt.New(2, "usage", "--output must be json, table, tsv or ndjson")
	}
	if len(fields) == 0 {
		present := map[string]bool{}
		for _, row := range rows {
			if m, ok := row.(map[string]any); ok {
				for k := range m {
					present[k] = true
				}
			}
		}
		for _, f := range []string{"id", "display_name", "name", "label", "email", "status_label", "_type", "version"} {
			if present[f] {
				fields = append(fields, f)
			}
		}
		if len(fields) == 0 {
			for k := range present {
				fields = append(fields, k)
			}
			sort.Strings(fields)
		}
	}
	target := w
	var table *tabwriter.Writer
	if mode == "table" {
		table = tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		target = table
		if _, err := fmt.Fprintln(target, strings.Join(fields, "\t")); err != nil {
			return err
		}
	}
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			return errfmt.New(1, "protocol", "table output requires object records")
		}
		cells := make([]string, len(fields))
		for i, f := range fields {
			x, _ := Lookup(m, f)
			cells[i] = cell(x)
		}
		if _, err := fmt.Fprintln(target, strings.Join(cells, "\t")); err != nil {
			return err
		}
	}
	if table != nil {
		return table.Flush()
	}
	return nil
}

func cell(v any) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		b, _ := json.Marshal(v)
		s = string(b)
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || r == 0x2028 || r == 0x2029 || r == 0x202e {
			fmt.Fprintf(&b, "\\u%04x", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
