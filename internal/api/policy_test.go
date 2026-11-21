package api

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voska/close/internal/errfmt"
	"github.com/voska/close/internal/policy"
)

func TestClientPolicyBlocksBeforeRequest(t *testing.T) {
	calls := 0
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) })
	c.Policy = &policy.Policy{Version: 1, Deny: []string{"message-content", "writes"}}
	for _, request := range []struct{ method, path string }{{"GET", "/Activity/Email/"}, {"GET", "task/task_demo/"}, {"GET", "lead/lead_demo/"}, {"POST", "lead/"}, {"PUT", "contact/cont_demo/"}, {"DELETE", "task/task_demo/"}} {
		_, err := c.Do(context.Background(), request.method, request.path, nil, nil, true)
		var e *errfmt.Error
		if !errors.As(err, &e) || e.Code != 11 || calls != 0 {
			t.Fatalf("%+v err=%v calls=%d", request, err, calls)
		}
	}
	if _, err := c.Write(context.Background(), "PUT", "lead/lead_demo/", map[string]any{"name": "Demo"}); err == nil {
		t.Fatal("write bypassed policy")
	}
	if calls != 0 {
		t.Fatal("policy refusal reached transport")
	}
}

func TestPolicyRefusesFullReadbackFallback(t *testing.T) {
	for _, tc := range []struct{ resource, field string }{{"task", "date"}, {"lead", "name"}} {
		t.Run(tc.resource, func(t *testing.T) {
			reads, writes := 0, 0
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "PUT" {
					writes++
					_, _ = w.Write([]byte(`{"id":"demo","date":"Demo","name":"Demo","text":"SECRET_SMS_BODY","body_preview":"SECRET_EMAIL_BODY","tasks":[{"body_preview":"SECRET_EMBEDDED_TASK_BODY"}]}`))
					return
				}
				reads++
				fields := []string{"id", tc.field}
				sort.Strings(fields)
				if r.URL.Query().Get("_fields") != strings.Join(fields, ",") {
					t.Errorf("unexpected read query: %v", r.URL.Query())
				}
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":"_fields is not supported"}`))
			})
			c.Policy = &policy.Policy{Version: 1, Deny: []string{"message-content"}}
			v, err := c.Write(context.Background(), "PUT", tc.resource+"/demo/", map[string]any{tc.field: "Demo"})
			var e *errfmt.Error
			if !errors.As(err, &e) || e.Code != 9 || !strings.Contains(e.Message, "message-content") || reads != 1 || writes != 1 {
				t.Fatalf("err=%v reads=%d writes=%d", err, reads, writes)
			}
			if !reflect.DeepEqual(v, map[string]any{"id": "demo"}) {
				t.Fatalf("unverified echo exposed unsupplied fields: %v", v)
			}
		})
	}
}

func TestLeadTaskIndexRetryStopsOnHTTPError(t *testing.T) {
	var reads, writes atomic.Int32
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/task/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method == http.MethodPost {
			writes.Add(1)
			_, _ = w.Write([]byte(`{"id":"task_demo"}`))
			return
		}
		if reads.Add(1) == 2 {
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c.Policy = &policy.Policy{Version: 1, Deny: []string{"message-content"}}
	var waits []time.Duration
	c.Wait = func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}
	got, err := c.Write(context.Background(), http.MethodPost, "task/", map[string]any{"text": "Call back"})
	var e *errfmt.Error
	if !errors.As(err, &e) || e.Code != 9 || !reflect.DeepEqual(got, map[string]any{"id": "task_demo"}) {
		t.Fatalf("result=%v err=%v, want ID-only output and exit 9", got, err)
	}
	if reads.Load() != 3 || writes.Load() != 1 || !reflect.DeepEqual(waits, []time.Duration{30 * time.Second, time.Second}) {
		t.Fatalf("reads=%d writes=%d waits=%v", reads.Load(), writes.Load(), waits)
	}
}
