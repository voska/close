package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/voska/close/internal/errfmt"
	"github.com/voska/close/internal/policy"
)

func TestDeploymentPolicyRefusesBeforeHTTP(t *testing.T) {
	for _, tc := range []struct {
		rule string
		args []string
	}{
		{"message-content", []string{"--readonly", "activity", "email", "list"}},
		{"message-content", []string{"activity", "call", "get", "acti_demo", "--fields", "id"}},
		{"message-content", []string{"activity", "list", "--api-fields", "id"}},
		{"message-content", []string{"event", "list", "--filter", "object_type=lead"}},
		{"message-content", []string{"task", "list"}},
		{"message-content", []string{"task", "get", "task_demo"}},
		{"message-content", []string{"task", "list", "--filter", "_type=email_followup"}},
		{"message-content", []string{"task", "get", "task_demo", "--fields", "id"}},
		{"message-content", []string{"task", "list", "--api-fields", "body_preview"}},
		{"message-content", []string{"task", "get", "task_demo", "--api-fields", "subject,voicemail_url,attachments"}},
		{"message-content", []string{"lead", "list"}},
		{"message-content", []string{"lead", "get", "lead_demo"}},
		{"message-content", []string{"lead", "get", "lead_demo", "--api-fields", "id,tasks"}},
		{"message-content", []string{"activity", "email", "create", "--data", "{}", "--dry-run", "--confirm"}},
		{"message-content", []string{"api", "GET", "/activity/whatsapp_message/", "--readonly=false"}},
		{"message-content", []string{"search", "--data", `{"_fields":{"activity.email":["body_text"]}}`}},
		{"ai-summaries", []string{"api", "GET", "/activity/lead_summary/"}},
		{"ai-summaries", []string{"search", "--data", `{"_fields":{"lead":["id","summary"]}}`}},
		{"deletes", []string{"lead", "delete", "lead_demo", "--confirm"}},
		{"deletes", []string{"lead", "delete", "lead_demo", "--dry-run"}},
		{"merges", []string{"api", "POST", "/lead/merge/", "--data", "{}", "--confirm"}},
		{"exports", []string{"api", "GET", "/export/lead/"}},
		{"bulk-actions", []string{"api", "GET", "/bulk_action/edit/"}},
		{"raw-api", []string{"api", "GET", "/me/"}},
		{"raw-api", []string{"api", "POST", "/data/search/", "--data", "{}"}},
		{"raw-api", []string{"api", "GET", "/me/", "--dry-run"}},
		{"writes", []string{"lead", "update", "lead_demo", "--data", "{}", "--readonly=false"}},
	} {
		t.Run(tc.rule+"/"+strings.Join(tc.args, " "), func(t *testing.T) {
			calls := 0
			var out, stderr bytes.Buffer
			code := Execute(context.Background(), tc.args, Dependencies{Out: &out, Err: &stderr, In: strings.NewReader(""), Store: &memoryStore{}, Getenv: func(k string) string {
				return ""
			}, LoadPolicy: func() (*policy.Policy, error) { return &policy.Policy{Version: 1, Deny: []string{tc.rule}}, nil }, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[],"has_more":false}`))}, nil
			})}, Build{})
			var result struct {
				Error errfmt.Error `json:"error"`
			}
			if err := json.Unmarshal(stderr.Bytes(), &result); err != nil {
				t.Fatal(err, stderr.String())
			}
			if code != 11 || result.Error.Rule != tc.rule || calls != 0 || out.Len() != 0 {
				t.Fatalf("code=%d rule=%q calls=%d out=%s err=%s", code, result.Error.Rule, calls, out.String(), stderr.String())
			}
		})
	}
}

func TestDeploymentPolicyAllowsLocalDiscoveryAndReads(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"schema", "exit-codes"}, {"lead", "--help"}, {"completion", "bash"}, {"lead", "list", "--api-fields", "id,name"}, {"task", "list", "--api-fields", "id,_type,text,date"}, {"task", "get", "task_demo", "--api-fields", "id,_type,text,date"}, {"search", "--data", `{"query":{},"_fields":{"lead":["id","name"]}}`}} {
		var out, stderr bytes.Buffer
		code := Execute(context.Background(), args, Dependencies{Out: &out, Err: &stderr, LoadPolicy: func() (*policy.Policy, error) {
			return &policy.Policy{Version: 1, Deny: []string{"writes", "raw-api", "message-content", "ai-summaries"}}, nil
		}, Getenv: func(k string) string {
			if k == "CLOSE_API_KEY" {
				return "fixture-key"
			}
			return ""
		}, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[],"has_more":false}`))}, nil
		})}, Build{})
		if code != 0 || stderr.Len() != 0 || out.Len() == 0 {
			t.Fatalf("%v: code=%d out=%s err=%s", args, code, out.String(), stderr.String())
		}
	}
}

func TestInvalidPolicyFailsClosedButDiscoveryWorks(t *testing.T) {
	for _, args := range [][]string{{"lead", "list"}, {"lead", "create", "--data", "{}", "--dry-run"}, {"api", "GET", "/me/"}, {"version"}, {"schema"}, {"--help"}, {"help", "lead"}, {}, {"auth", "status", "--offline"}} {
		var out, stderr bytes.Buffer
		code := Execute(context.Background(), args, Dependencies{Out: &out, Err: &stderr, Store: &memoryStore{}, Getenv: func(string) string { return "" }, LoadPolicy: func() (*policy.Policy, error) {
			return nil, errfmt.New(10, "configuration", "invalid deployment policy")
		}}, Build{})
		want := 0
		if len(args) > 0 && (args[0] == "lead" || args[0] == "api") {
			want = 10
		}
		if code != want {
			t.Fatalf("%v: code=%d want=%d err=%s", args, code, want, stderr.String())
		}
	}
}
