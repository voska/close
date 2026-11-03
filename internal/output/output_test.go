package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestModes(t *testing.T) {
	v := map[string]any{"data": []any{map[string]any{"id": "lead_demo", "name": "Demo\t\x1b[31m", "custom.cf_demo": "value", "nested": map[string]any{"a": true}}}, "has_more": true}
	for _, mode := range []string{"json", "ndjson", "tsv", "table"} {
		t.Run(mode, func(t *testing.T) {
			var b bytes.Buffer
			if err := Write(&b, v, mode, []string{"id", "name", "custom.cf_demo", "nested.a"}, false); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(b.String(), "\x1b") {
				t.Fatal("terminal escape")
			}
			if mode == "json" || mode == "ndjson" {
				var m map[string]any
				if err := json.Unmarshal(b.Bytes(), &m); err != nil {
					t.Fatal(err)
				}
				if mode == "json" && m["has_more"] != true {
					t.Fatal("lost pagination")
				}
			} else if !strings.Contains(b.String(), "true") {
				t.Fatal(b.String())
			}
		})
	}
	var b bytes.Buffer
	if err := Write(&b, v, "json", nil, true); err != nil || b.String() != "lead_demo\n" {
		t.Fatalf("%q %v", b.String(), err)
	}
}

func TestOutputEdges(t *testing.T) {
	for _, tc := range []struct {
		v     map[string]any
		mode  string
		quiet bool
		fail  bool
	}{
		{map[string]any{"data": []any{}}, "table", false, false},
		{map[string]any{"nested": map[string]any{"a": 1}}, "tsv", false, false},
		{map[string]any{"a": 1}, "unknown", false, true},
		{map[string]any{"a": 1}, "json", true, true},
		{map[string]any{"data": []any{"bad"}}, "table", false, true},
		{map[string]any{"version": "dev"}, "json", true, false},
	} {
		var b bytes.Buffer
		if err := Write(&b, tc.v, tc.mode, nil, tc.quiet); (err != nil) != tc.fail {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	if _, ok := Lookup(map[string]any{}, "x.y"); ok {
		t.Fatal("missing lookup")
	}
}
