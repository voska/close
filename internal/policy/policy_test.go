package policy

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voska/close/internal/errfmt"
)

func TestRules(t *testing.T) {
	for _, tc := range []struct {
		rule, method, path string
		query              url.Values
		body               map[string]any
		deny               bool
	}{
		{"message-content", "GET", "activity/email/", nil, nil, true},
		{"message-content", "GET", "activity/emailthread/x/", nil, nil, true},
		{"message-content", "GET", "activity/sms/", nil, nil, true},
		{"message-content", "GET", "activity/call/x/transcription/", nil, nil, true},
		{"message-content", "GET", "activity/meeting/", nil, nil, true},
		{"message-content", "GET", "activity/whatsapp_message/", nil, nil, true},
		{"message-content", "GET", "activity/", nil, nil, true},
		{"message-content", "GET", "event/x/", nil, nil, true},
		{"message-content", "GET", "task/", nil, nil, true},
		{"message-content", "GET", "task/x/", nil, nil, true},
		{"message-content", "GET", "task/", url.Values{"_type": {"email_followup"}}, nil, true},
		{"message-content", "GET", "task/x/", url.Values{"_fields": {""}}, nil, true},
		{"message-content", "GET", "task/x/", url.Values{"_fields": {" , "}}, nil, true},
		{"message-content", "GET", "task/x/", url.Values{"_fields": {"id", ""}}, nil, true},
		{"message-content", "GET", "task/", url.Values{"_fields": {"body_preview"}}, nil, true},
		{"message-content", "GET", "task/x/", url.Values{"_fields": {"id,subject"}}, nil, true},
		{"message-content", "GET", "task/x/", url.Values{"_fields": {"voicemail_url"}}, nil, true},
		{"message-content", "GET", "task/x/", url.Values{"_fields": {"recording_url"}}, nil, true},
		{"message-content", "GET", "task/x/", url.Values{"_fields": {"attachments"}}, nil, true},
		{"message-content", "GET", "task/x/", url.Values{"_fields": {"id", "body_preview"}}, nil, true},
		{"message-content", "GET", "task/x/", url.Values{"_fields": {"id,*"}}, nil, true},
		{"message-content", "GET", "task/", url.Values{"_fields": {"id,_type,date,is_complete"}}, nil, false},
		{"message-content", "GET", "task/x/", url.Values{"_fields": {"id,_type,date,is_complete"}}, nil, false},
		{"message-content", "GET", "task/", url.Values{"_type": {"incoming_sms"}, "_fields": {"id,text"}}, nil, true},
		{"message-content", "GET", "task/", url.Values{"_type": {"lead"}, "_fields": {"id,text"}}, nil, true},
		{"message-content", "GET", "task/x/", url.Values{"_fields": {"id,text"}}, nil, true},
		{"message-content", "PUT", "task/x/", nil, map[string]any{"text": ""}, true},
		{"message-content", "GET", "lead/", nil, nil, true},
		{"message-content", "GET", "lead/x/", nil, nil, true},
		{"message-content", "GET", "lead/x/", url.Values{"_fields": {"id,tasks"}}, nil, true},
		{"message-content", "GET", "lead/x/", url.Values{"_fields": {"tasks.text"}}, nil, true},
		{"message-content", "GET", "activity/note/", nil, nil, false},
		{"message-content", "GET", "activity/custom/", nil, nil, false},
		{"message-content", "GET", "Activity/Email/", nil, nil, true},
		{"message-content", "GET", "activity/future_message/", nil, nil, true},
		{"message-content", "PUT", "contact/x/", nil, map[string]any{"emails": []any{map[string]any{"email": "synthetic@example.test"}}}, false},
		{"ai-summaries", "GET", "activity/meeting/", nil, nil, true},
		{"ai-summaries", "GET", "activity/emailthread/", nil, nil, true},
		{"ai-summaries", "GET", "lead/x/", url.Values{"_fields": {"id,primary_address_summary,contacts_summary"}}, nil, false},
		{"message-content", "GET", "lead/x/", url.Values{"_fields": {"id,name"}}, nil, false},
		{"message-content", "GET", "lead/x/", url.Values{"_fields": {"id,activities"}}, nil, true},
		{"message-content", "POST", "data/search/", nil, map[string]any{"_fields": map[string]any{"activity.email": []any{"body_text"}}}, true},
		{"message-content", "POST", "data/search/", nil, map[string]any{"query": map[string]any{"object_type": "activity.call"}}, true},
		{"message-content", "POST", "data/search/", nil, map[string]any{"_fields": map[string]any{"lead": []any{"id", "display_name"}}}, false},
		{"message-content", "POST", "data/search/", nil, map[string]any{"_fields": map[string]any{"lead": []any{"id", "tasks"}}}, true},
		{"message-content", "POST", "data/search/", nil, map[string]any{"_fields": map[string]any{"task.email_followup": []any{"body_preview"}}}, true},
		{"message-content", "POST", "data/search/", nil, map[string]any{"query": map[string]any{"field_name": "subject", "object_type": "task.incoming_email"}}, true},
		{"message-content", "POST", "data/search/", nil, map[string]any{"_fields": map[string]any{"task.voicemail": []any{"voicemail_url"}}}, true},
		{"message-content", "POST", "data/search/", nil, map[string]any{"_fields": map[string]any{"task.incoming_sms": []any{"attachments"}}}, true},
		{"message-content", "POST", "data/search/", nil, map[string]any{"_fields": map[string]any{"task.incoming_sms": []any{"text"}}}, true},
		{"message-content", "POST", "data/search/", nil, map[string]any{"_fields": map[string]any{"task.incoming_whatsapp_message": []any{"text"}}}, true},
		{"message-content", "POST", "data/search/", nil, map[string]any{"_fields": map[string]any{"task.lead": []any{"text"}}}, true},
		{"message-content", "POST", "data/search/", nil, map[string]any{"_fields": map[string]any{"task": []any{"text"}}}, true},
		{"message-content", "POST", "data/search/", nil, map[string]any{"query": map[string]any{"object_type": "task.incoming_sms", "field_name": "text"}}, true},
		{"message-content", "POST", "data/search/", nil, map[string]any{"query": map[string]any{"field_name": "text"}}, true},
		{"message-content", "POST", "data/search/", nil, map[string]any{"_fields": map[string]any{"task.incoming_sms": []any{"id", "date", "is_complete"}}}, false},
		{"ai-summaries", "GET", "activity/lead_summary/", nil, nil, true},
		{"ai-summaries", "GET", "ai/summaries/", nil, nil, true},
		{"ai-summaries", "GET", "event/", nil, nil, true},
		{"ai-summaries", "GET", "activity/", nil, nil, true},
		{"ai-summaries", "GET", "lead/x/", url.Values{"_fields": {"id,ai_summary"}}, nil, true},
		{"ai-summaries", "POST", "data/search/", nil, map[string]any{"_fields": map[string]any{"lead": []any{"id", "summary"}}}, true},
		{"ai-summaries", "GET", "lead/x/", nil, nil, false},
		{"ai-summaries", "GET", "lead/x/", url.Values{"_fields": {"id,call_summary"}}, nil, true},
		{"ai-summaries", "GET", "lead/x/", url.Values{"_fields": {"id,meeting_summary"}}, nil, true},
		{"ai-summaries", "GET", "lead/x/", url.Values{"_fields": {"id,conversation_summary"}}, nil, true},
		{"ai-summaries", "GET", "lead/x/", url.Values{"_fields": {"id,custom.cf_summarY"}}, nil, false},
		{"deletes", "DELETE", "lead/x/", nil, nil, true},
		{"deletes", "GET", "lead/x/", nil, nil, false},
		{"deletes", "POST", "bulk_action/delete/", nil, nil, true},
		{"merges", "POST", "lead/merge/", nil, nil, true},
		{"exports", "GET", "export/lead/x/", nil, nil, true},
		{"bulk-actions", "POST", "bulk_action/edit/", nil, nil, true},
		{"bulk-actions", "GET", "send_as/bulk/", nil, nil, true},
		{"writes", "PUT", "lead/x/", nil, nil, true},
		{"writes", "DELETE", "lead/x/", nil, nil, true},
		{"writes", "POST", "data/search/", nil, map[string]any{"query": map[string]any{}}, false},
		{"writes", "GET", "lead/", nil, nil, false},
	} {
		t.Run(tc.rule+"/"+tc.method+"/"+tc.path, func(t *testing.T) {
			p, err := parse(strings.NewReader(`{"version":1,"deny":["` + tc.rule + `"]}`))
			if err != nil {
				t.Fatal(err)
			}
			err = p.Check(tc.method, tc.path, tc.query, tc.body)
			if !tc.deny {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var e *errfmt.Error
			if !errors.As(err, &e) || e.Code != 11 || e.Type != "policy_denied" || e.Rule != tc.rule {
				t.Fatalf("%#v", err)
			}
		})
	}
}

func TestParseAndLoad(t *testing.T) {
	for _, raw := range []string{`{}`, `{"version":1}`, `{"version":1,"deny":["writes"],"deny":[]}`, `null`, `{"version":2}`, `{"version":1,"deny":["typo"]}`, `{"version":1,"readonly":true}`, `{"version":1} {}`, strings.Repeat(" ", 65537)} {
		if _, err := parse(strings.NewReader(raw)); err == nil {
			t.Fatal("accepted", raw[:min(len(raw), 100)])
		}
	}
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"deny":["raw-api"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := load(path, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err = p.CheckRaw(); err == nil {
		t.Fatal("raw API allowed")
	}
	p, err = load(path+"-missing", func(string) error { return nil })
	if err != nil || p.CheckRaw() != nil {
		t.Fatal(p, err)
	}
	if _, err = load(path, func(string) error { return errors.New("untrusted") }); err == nil {
		t.Fatal("ignored trust error")
	}
	if err = os.WriteFile(path, []byte(`{"version":1,"deny":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	p, err = load(path, func(string) error { return nil })
	if err != nil || p.CheckRaw() != nil {
		t.Fatal(p, err)
	}
}
