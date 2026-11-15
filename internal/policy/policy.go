// Package policy enforces deployment restrictions independently of CLI flags.
package policy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/voska/close/internal/errfmt"
)

type Policy struct {
	Version int      `json:"version"`
	Deny    []string `json:"deny"`
}

var rules = []string{"message-content", "ai-summaries", "deletes", "merges", "exports", "bulk-actions", "raw-api", "writes"}

func load(path string, check func(string) error) (*Policy, error) {
	if err := check(path); err != nil {
		return nil, errfmt.New(10, "configuration", "deployment policy: %v", err)
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, errfmt.New(10, "configuration", "cannot read deployment policy: %v", err)
	}
	defer func() { _ = f.Close() }()
	return parse(f)
}

func parse(r io.Reader) (*Policy, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 65537))
	if err != nil || len(raw) > 65536 {
		return nil, errfmt.New(10, "configuration", "deployment policy must be readable and at most 64 KiB")
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))

	var p Policy
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errfmt.New(10, "configuration", "deployment policy must be a JSON object")
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return nil, errfmt.New(10, "configuration", "invalid or duplicate deployment policy key")
		}
		seen[key] = true
		switch key {
		case "version":
			err = d.Decode(&p.Version)
		case "deny":
			err = d.Decode(&p.Deny)
		default:
			return nil, errfmt.New(10, "configuration", "unknown deployment policy field %q", key)
		}
		if err != nil {
			return nil, errfmt.New(10, "configuration", "invalid deployment policy %s: %v", key, err)
		}
	}
	if _, err = d.Token(); err != nil || d.Decode(new(any)) != io.EOF || p.Version != 1 || p.Deny == nil {
		return nil, errfmt.New(10, "configuration", "deployment policy requires version 1 and a deny array")
	}
	for _, rule := range p.Deny {
		if !slices.Contains(rules, rule) {
			return nil, errfmt.New(10, "configuration", "unknown deployment policy rule %q", rule)
		}
	}
	return &p, nil
}

func IsRead(method, path string) bool {
	return method == "GET" || method == "POST" && strings.Trim(path, "/") == "data/search"
}

func (p *Policy) denied(rule string) error {
	return &errfmt.Error{Code: 11, Type: "policy_denied", Rule: rule, Message: fmt.Sprintf("deployment policy rule %q refuses this operation", rule)}
}

func (p *Policy) CheckRaw() error {
	if p != nil && slices.Contains(p.Deny, "raw-api") {
		return p.denied("raw-api")
	}
	return nil
}

func (p *Policy) Check(method, path string, query url.Values, body map[string]any) error {
	if p == nil {
		return nil
	}
	read := IsRead(method, path)
	path = strings.Trim(strings.ToLower(path), "/")
	parts := strings.Split(path, "/")
	for _, rule := range rules {
		if !slices.Contains(p.Deny, rule) {
			continue
		}
		blocked := false
		switch rule {
		case "writes":
			blocked = !read
		case "deletes":
			blocked = method == "DELETE" || slices.Contains(parts, "delete")
		case "merges":
			blocked = slices.Contains(parts, "merge") || slices.Contains(parts, "lead_merge")
		case "exports":
			blocked = slices.Contains(parts, "export") || slices.Contains(parts, "exports")
		case "bulk-actions":
			blocked = slices.Contains(parts, "bulk_action") || slices.Contains(parts, "bulk_actions") || slices.Contains(parts, "bulk")
		case "message-content", "ai-summaries":
			blocked = contentPath(rule, path)
			if rule == "message-content" && read && (parts[0] == "task" || parts[0] == "lead") {
				// Tasks carry communication previews; leads embed task objects.
				blocked = blocked || len(query["_fields"]) == 0
				for _, fields := range query["_fields"] {
					for _, field := range strings.Split(fields, ",") {
						blocked = blocked || strings.TrimSpace(field) == ""
					}
				}
			}
			for _, fields := range query["_fields"] {
				blocked = blocked || contentField(rule, fields)
				blocked = blocked || (parts[0] == "task" && taskTextField(rule, fields))
			}
			if parts[0] == "task" {
				for field := range body {
					blocked = blocked || taskTextField(rule, field)
				}
			}
			blocked = blocked || contentBody(rule, body)
		}
		if blocked {
			return p.denied(rule)
		}
	}
	return nil
}

func contentPath(rule, path string) bool {
	path = strings.Trim(strings.ToLower(strings.ReplaceAll(path, ".", "/")), "/")
	if path == "activity" || path == "activities" || path == "event" || strings.HasPrefix(path, "event/") {
		return true
	}
	if strings.HasPrefix(path, "activity/") {
		typeName := strings.Split(path, "/")[1]
		if rule == "message-content" && typeName != "note" && typeName != "custom" {
			return true
		}
		if rule == "ai-summaries" && (typeName == "call" || typeName == "meeting" || typeName == "emailthread") {
			return true
		}
	}
	for _, part := range strings.Split(path, "/") {
		if rule == "message-content" {
			switch part {
			case "email", "emailthread", "email_template", "sms", "sms_template", "call", "meeting", "whatsapp", "whatsapp_message":
				return true
			}
		}
		if contentField(rule, part) {
			return true
		}
	}
	return false
}

func contentField(rule, fields string) bool {
	for _, field := range strings.FieldsFunc(strings.ToLower(fields), func(r rune) bool { return r == ',' || r == '.' || r == '/' }) {
		field = strings.TrimSpace(field)
		if field == "_all" || field == "*" || field == "activities" || field == "events" {
			return true
		}
		if rule == "message-content" {
			switch field {
			case "body_text", "body_html", "body_preview", "subject", "voicemail_url", "recording_url", "attachments", "tasks":
				return true
			}
			if strings.Contains(field, "transcript") {
				return true
			}
		}
		if rule == "ai-summaries" {
			if strings.Contains(field, "summar") && !strings.HasPrefix(field, "cf_") && !slices.Contains([]string{"primary_address_summary", "contacts_summary", "comment_summary"}, field) {
				return true
			}
		}
	}
	return false
}

func taskTextField(rule, fields string) bool {
	if rule == "message-content" {
		for _, field := range strings.FieldsFunc(strings.ToLower(fields), func(r rune) bool { return r == ',' || r == '.' || r == '/' }) {
			if strings.TrimSpace(field) == "text" {
				return true
			}
		}
	}
	return false
}

func contentBody(rule string, v any) bool {
	switch value := v.(type) {
	case map[string]any:
		for key, item := range value {
			if contentField(rule, key) {
				return true
			}
			switch key {
			case "object_type", "related_object_type", "this_object_type":
				if s, ok := item.(string); ok && contentPath(rule, s) {
					return true
				}
			case "field_name":
				// Search conditions may omit their task type; text can be an SMS body.
				if s, ok := item.(string); ok && (contentField(rule, s) || taskTextField(rule, s)) {
					return true
				}
			case "_fields":
				switch fields := item.(type) {
				case map[string]any:
					for object, projection := range fields {
						if contentPath(rule, object) {
							return true
						}
						task := strings.Split(strings.ToLower(object), ".")[0] == "task"
						if list, ok := projection.([]any); ok {
							for _, field := range list {
								if s, ok := field.(string); ok && (contentField(rule, s) || (task && taskTextField(rule, s))) {
									return true
								}
							}
						}
					}
				case string:
					if contentField(rule, fields) || taskTextField(rule, fields) {
						return true
					}
				}
			}
			if contentBody(rule, item) {
				return true
			}
		}
	case []any:
		for _, item := range value {
			if contentBody(rule, item) {
				return true
			}
		}
	}
	return false
}
