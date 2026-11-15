package api

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/voska/close/internal/errfmt"
	"github.com/voska/close/internal/policy"
)

func TestClientPolicyBlocksBeforeRequest(t *testing.T) {
	calls := 0
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) })
	c.Policy = &policy.Policy{Version: 1, Deny: []string{"message-content", "writes"}}
	for _, request := range []struct{ method, path string }{{"GET", "/Activity/Email/"}, {"POST", "lead/"}, {"PUT", "contact/cont_demo/"}, {"DELETE", "task/task_demo/"}} {
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
