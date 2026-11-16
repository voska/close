package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/voska/close/internal/errfmt"
)

func TestWriteRequestsOnlySuppliedFields(t *testing.T) {
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			reads, writes := 0, 0
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					writes++
					_, _ = w.Write([]byte(`{"id":"lead_demo"}`))
					return
				}
				reads++
				want := "custom.cf_demo,id,name"
				if method == "DELETE" {
					want = "id"
				}
				if r.URL.Query().Get("_fields") != want || len(r.URL.Query()) != 1 {
					t.Errorf("read-back query = %v, want _fields=%s", r.URL.Query(), want)
				}
				if method == "DELETE" {
					w.WriteHeader(404)
					return
				}
				_, _ = w.Write([]byte(`{"id":"lead_demo","name":"Demo","custom.cf_demo":"value"}`))
			})
			path := "lead/lead_demo/"
			body := map[string]any{"name": "Demo", "custom.cf_demo": "value"}
			if method == "POST" {
				path = "lead/"
			}
			if method == "DELETE" {
				body = nil
			}
			if _, err := c.Write(context.Background(), method, path, body); err != nil {
				t.Fatal(err)
			}
			if reads != 1 || writes != 1 {
				t.Fatalf("reads=%d writes=%d", reads, writes)
			}
		})
	}
}

func TestReadbackFallbackOnlyForExplicitUnsupportedFields(t *testing.T) {
	for _, tc := range []struct {
		name, reply         string
		status, reads, code int
	}{
		{"unsupported", `{"error":"_fields is not supported on this endpoint"}`, 400, 2, 0},
		{"unsupported-422", `{"errors":{"_fields":"Unknown query parameter"}}`, 422, 2, 0},
		{"scoped-invalid-field", `{"errors":{"_fields":"field name is not supported"}}`, 400, 1, 9},
		{"unsupported-field", `{"error":"_fields contains an unsupported field: name"}`, 400, 1, 9},
		{"different-parameter", `{"error":"unknown query parameter other","_fields":"id,name"}`, 400, 1, 9},
		{"invalid-field", `{"error":"Invalid field in _fields: name"}`, 400, 1, 9},
		{"other-validation", `{"error":"Invalid parameter"}`, 400, 1, 9},
		{"forbidden", `{"error":"_fields is not supported"}`, 403, 1, 9},
		{"missing", `{}`, 404, 1, 9},
		{"mismatch", `{"id":"lead_demo","name":"Old"}`, 200, 1, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads, writes := 0, 0
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					writes++
					_, _ = w.Write([]byte(`{"id":"lead_demo"}`))
					return
				}
				reads++
				if reads == 1 {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.reply))
					return
				}
				if r.URL.RawQuery != "" {
					t.Error("fallback still projected", r.URL)
				}
				_, _ = w.Write([]byte(`{"id":"lead_demo","name":"Demo","extra":true}`))
			})
			v, err := c.Write(context.Background(), "PUT", "lead/lead_demo/", map[string]any{"name": "Demo"})
			code := 0
			var e *errfmt.Error
			if err != nil {
				if !errors.As(err, &e) {
					t.Fatal(err)
				}
				code = e.Code
			}
			if code != tc.code || reads != tc.reads || writes != 1 {
				t.Fatalf("code=%d err=%v reads=%d writes=%d", code, err, reads, writes)
			}
			if tc.name == "mismatch" && !reflect.DeepEqual(v, map[string]any{"id": "lead_demo", "name": "Old"}) {
				t.Fatal(v)
			}
		})
	}
}

func TestEmptyReadbackAndNumericContract(t *testing.T) {
	for _, body := range []map[string]any{{}, {"id": "lead_demo"}, {"value": json.Number("1")}} {
		c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && r.URL.Query().Get("_fields") == "" {
				t.Error("missing _fields")
			}
			_, _ = w.Write([]byte(`{"id":"lead_demo","value":1.0}`))
		})
		if _, err := c.Write(context.Background(), "PUT", "lead/lead_demo/", body); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWriteRejectsInvalidProjectionKeysBeforeHTTP(t *testing.T) {
	for _, key := range []string{"", " ", " name", "name ", "\tname", "name\n", "x,", "x,y", "x y"} {
		t.Run(key, func(t *testing.T) {
			calls := 0
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = w.Write([]byte(`{"id":"demo"}`))
			})
			v, err := c.Write(context.Background(), "PUT", "task/demo/", map[string]any{key: 0, "is_complete": false})
			var e *errfmt.Error
			if !errors.As(err, &e) || e.Code != 2 || calls != 0 || v != nil {
				t.Fatalf("key=%q result=%v err=%v calls=%d", key, v, err, calls)
			}
		})
	}
}

func TestUnverifiedWriteEchoContainsOnlyID(t *testing.T) {
	for _, tc := range []struct {
		name, method, id string
		status           int
	}{
		{"no-id", "POST", "", 200},
		{"slash-id", "POST", "../me", 200},
		{"invalid-id", "POST", "bad?", 200},
		{"forbidden", "PUT", "demo", 403},
		{"server-error", "PUT", "demo", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					w.WriteHeader(tc.status)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": tc.id, "is_complete": false, "text": "SECRET_SMS_BODY", "custom.cf_demo": json.Number("9007199254740993")})
			})
			path := "task/demo/"
			if tc.method == "POST" {
				path = "task/"
			}
			v, err := c.Write(context.Background(), tc.method, path, map[string]any{"is_complete": false, "custom.cf_demo": json.Number("9007199254740993")})
			var e *errfmt.Error
			want := map[string]any{"id": tc.id}
			if !errors.As(err, &e) || e.Code != 9 || !reflect.DeepEqual(v, want) {
				t.Fatalf("result=%v err=%v, want=%v and exit 9", v, err, want)
			}
		})
	}
}
