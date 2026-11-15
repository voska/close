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
		{"message-content", []string{"task", "list", "--filter", "_type=incoming_sms", "--api-fields", "id,text"}},
		{"message-content", []string{"task", "get", "task_demo", "--api-fields", "id,text"}},
		{"message-content", []string{"task", "update", "task_demo", "--data", `{"text":""}`, "--confirm"}},
		{"message-content", []string{"search", "--data", `{"_fields":{"task.incoming_sms":["text"]}}`}},
		{"message-content", []string{"search", "--data", `{"query":{"field_name":"text","object_type":"task.incoming_sms"}}`}},
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
	for _, args := range [][]string{{"version"}, {"schema", "exit-codes"}, {"lead", "--help"}, {"completion", "bash"}, {"lead", "list", "--api-fields", "id,name"}, {"task", "list", "--api-fields", "id,_type,date,is_complete"}, {"task", "get", "task_demo", "--api-fields", "id,_type,date,is_complete"}, {"search", "--data", `{"query":{},"_fields":{"lead":["id","name"]}}`}} {
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

func TestTaskWriteOutputOnVerificationFailure(t *testing.T) {
	for _, tc := range []struct {
		name, body               string
		status, code, wantWrites int
	}{
		{"empty-key", `{"is_complete":false,"":0}`, 200, 2, 0},
		{"comma-key", `{"is_complete":false,"x,":0}`, 200, 2, 0},
		{"space-key", `{"is_complete":false," ":0}`, 200, 2, 0},
		{"refused-fallback", `{"is_complete":false}`, 400, 9, 1},
		{"server-error", `{"is_complete":false}`, 503, 9, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			writes := 0
			code := Execute(context.Background(), []string{"task", "update", "task_demo", "--data", tc.body, "--confirm"}, Dependencies{
				Out: &out, Err: &stderr,
				LoadPolicy: func() (*policy.Policy, error) {
					return &policy.Policy{Version: 1, Deny: []string{"message-content"}}, nil
				},
				Getenv: func(k string) string {
					if k == "CLOSE_API_KEY" {
						return "fixture-key"
					}
					return ""
				},
				Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					status, reply := tc.status, `{"error":"_fields is not supported"}`
					if r.Method == "PUT" {
						writes++
						status, reply = 200, `{"id":"task_demo","is_complete":false,"text":"SECRET_SMS_BODY","body_preview":"SECRET_EMAIL_BODY"}`
					}
					return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": {"31"}}, Body: io.NopCloser(strings.NewReader(reply))}, nil
				}),
			}, Build{})
			if code != tc.code || writes != tc.wantWrites || strings.Contains(out.String(), "SECRET") {
				t.Fatalf("code=%d writes=%d out=%s err=%s", code, writes, out.String(), stderr.String())
			}
			if tc.code == 2 && out.Len() != 0 {
				t.Fatal("invalid input printed a write result", out.String())
			}
			if tc.code == 9 {
				var v map[string]any
				if err := json.Unmarshal(out.Bytes(), &v); err != nil || len(v) != 2 || v["id"] != "task_demo" || v["is_complete"] != false {
					t.Fatal("wrong unverified output", out.String(), err)
				}
			}
		})
	}
}
