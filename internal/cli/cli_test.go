package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type memoryStore struct{ key string }

func (s *memoryStore) Get(string) (string, error)     { return s.key, nil }
func (s *memoryStore) Set(_ string, key string) error { s.key = key; return nil }
func (s *memoryStore) Delete(string) error            { s.key = ""; return nil }

func run(t *testing.T, args []string, input string, handler http.HandlerFunc) (int, string, string) {
	t.Helper()
	env := map[string]string{}
	if handler != nil {
		s := httptest.NewServer(handler)
		t.Cleanup(s.Close)
		env["CLOSE_BASE_URL"] = s.URL + "/api/v1/"
		env["CLOSE_API_KEY"] = "fixture-key"
	}
	var out, stderr bytes.Buffer
	code := Execute(context.Background(), args, Dependencies{In: strings.NewReader(input), Out: &out, Err: &stderr, Getenv: func(k string) string { return env[k] }, Store: &memoryStore{}}, Build{Version: "test", Commit: "abc", Date: "today"})
	return code, out.String(), stderr.String()
}

func TestOfflineCommands(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"lead", "--help"}, {"activity", "note", "create", "--help"}, {"schema"}, {"schema", "lead", "list"}, {"schema", "exit-codes"}, {"version"}, {"auth", "status", "--offline"}, {"completion", "bash"}} {
		code, out, stderr := run(t, args, "", nil)
		if code != 0 || out == "" || stderr != "" {
			t.Fatalf("%v: %d %q %q", args, code, out, stderr)
		}
	}
}

func TestListFiltersAndProjection(t *testing.T) {
	code, out, stderr := run(t, []string{"lead", "list", "--filter", "query=name:Demo & Co", "--fields", "id,name"}, "", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") != "name:Demo & Co" || r.URL.Query().Get("_limit") != "50" {
			t.Error(r.URL)
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"lead_demo","name":"Demo","extra":true}],"has_more":true}`)
	})
	if code != 0 || stderr != "" {
		t.Fatalf("%d %s", code, stderr)
	}
	var v map[string]any
	if json.Unmarshal([]byte(out), &v) != nil || v["has_more"] != true {
		t.Fatal(out)
	}
	if strings.Contains(out, "extra") {
		t.Fatal(out)
	}
}

func TestWriteVerification(t *testing.T) {
	calls := 0
	code, out, stderr := run(t, []string{"contact", "create", "--data-file", "-"}, `{"lead_id":"lead_demo","name":"Demo"}`, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == "POST" {
			if r.URL.Path != "/api/v1/contact/" {
				t.Error(r.URL)
			}
			_, _ = io.WriteString(w, `{"id":"cont_demo"}`)
		} else {
			if r.URL.Path != "/api/v1/contact/cont_demo/" {
				t.Error(r.URL)
			}
			_, _ = io.WriteString(w, `{"id":"cont_demo","lead_id":"lead_demo","name":"Demo"}`)
		}
	})
	if code != 0 || calls != 2 || !strings.Contains(out, "cont_demo") || stderr != "" {
		t.Fatalf("%d %d %s %s", code, calls, out, stderr)
	}
}

func TestDryRunWithoutCredentials(t *testing.T) {
	for _, args := range [][]string{
		{"lead", "update", "lead_demo", "--data", `{"name":"Demo"}`, "--dry-run"},
		{"lead", "delete", "lead_demo", "--dry-run"},
		{"api", "POST", "lead/", "--data", `{"name":"Demo"}`, "--dry-run"},
	} {
		code, out, stderr := run(t, args, "", nil)
		if code != 0 || !strings.Contains(out, `"dry_run": true`) || stderr != "" {
			t.Fatalf("%v: %d %s %s", args, code, out, stderr)
		}
	}
}

func TestRejectBeforeNetwork(t *testing.T) {
	for _, args := range [][]string{
		{"lead", "get", "../me"},
		{"lead", "delete", "lead_demo"},
		{"activity", "email", "create", "--data", `{"status":"outbox"}`},
		{"lead", "create", "--data", `[]`},
		{"lead", "create", "--data", `{} {}`},
		{"lead", "create", "--data", `{}`, "--data-file", "-"},
		{"lead", "list", "--limit", "0"},
		{"lead", "list", "--skip", "-1"},
		{"lead", "list", "--filter", "broken"},
		{"lead", "list", "--filter", "_skip=abc"},
		{"event", "list", "--skip", "1"},
		{"api", "GET", "https://evil.test"},
		{"api", "DELETE", "lead/lead_demo/"},
		{"--readonly", "lead", "create", "--data", `{}`},
		{"lead", "list", "--json", "--plain"},
		{"lead", "list", "--output", "oops"},
		{"custom-field", "list"},
		{"custom-field", "list", "--object", "wrong"},
		{"schema", "unknown"},
	} {
		code, out, stderr := run(t, args, "", nil)
		if code != 2 || out != "" || !strings.Contains(stderr, `"error"`) {
			t.Fatalf("%v: %d %q %q", args, code, out, stderr)
		}
	}
}

func TestRawWriteUnverified(t *testing.T) {
	code, out, stderr := run(t, []string{"api", "POST", "lead/", "--data", `{}`, "--confirm"}, "", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"id":"lead_demo"}`) })
	if code != 9 || !strings.Contains(out, "lead_demo") || !strings.Contains(stderr, "unverified") {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
}

func TestUnverifiedUpdate(t *testing.T) {
	code, out, stderr := run(t, []string{"lead", "update", "lead_demo", "--data", `{"name":"new"}`}, "", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"lead_demo","name":"old"}`)
	})
	if code != 9 || !strings.Contains(out, "old") || !strings.Contains(stderr, "read-back") {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type testStore struct {
	key                    string
	getErr, setErr, delErr error
	keep                   bool
}

func (s *testStore) Get(string) (string, error) { return s.key, s.getErr }
func (s *testStore) Set(_ string, key string) error {
	if !s.keep {
		s.key = key
	}
	return s.setErr
}
func (s *testStore) Delete(string) error {
	if !s.keep {
		s.key = ""
	}
	return s.delErr
}

func TestAuthLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name         string
		args         []string
		input, reply string
		status, want int
		store        *testStore
	}{
		{"login", []string{"auth", "login", "--with-key-stdin"}, "synthetic-key\n", `{"id":"user_demo"}`, 200, 0, &testStore{}},
		{"login-flag", []string{"auth", "login"}, "key", `{}`, 200, 2, &testStore{}},
		{"login-empty", []string{"auth", "login", "--with-key-stdin"}, "", `{}`, 200, 10, &testStore{}},
		{"login-rejected", []string{"auth", "login", "--with-key-stdin"}, "key", `{}`, 401, 4, &testStore{}},
		{"login-no-id", []string{"auth", "login", "--with-key-stdin"}, "key", `{}`, 200, 1, &testStore{}},
		{"login-store-error", []string{"auth", "login", "--with-key-stdin"}, "key", `{"id":"user_demo"}`, 200, 10, &testStore{setErr: errors.New("unavailable")}},
		{"login-store-mismatch", []string{"auth", "login", "--with-key-stdin"}, "key", `{"id":"user_demo"}`, 200, 10, &testStore{keep: true}},
		{"offline", []string{"auth", "status", "--offline"}, "", `{}`, 200, 0, &testStore{key: "key"}},
		{"online", []string{"auth", "status"}, "", `{"id":"user_demo"}`, 200, 0, &testStore{key: "key"}},
		{"status-no-id", []string{"auth", "status"}, "", `{}`, 200, 1, &testStore{key: "key"}},
		{"status-denied", []string{"auth", "status"}, "", `{}`, 403, 6, &testStore{key: "key"}},
		{"logout", []string{"auth", "logout"}, "", `{}`, 200, 0, &testStore{key: "key"}},
		{"logout-failed", []string{"auth", "logout"}, "", `{}`, 200, 10, &testStore{delErr: errors.New("unavailable")}},
		{"logout-kept", []string{"auth", "logout"}, "", `{}`, 200, 10, &testStore{key: "key", keep: true}},
		{"logout-not-verified", []string{"auth", "logout"}, "", `{}`, 200, 10, &testStore{getErr: errors.New("unavailable")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			code := Execute(context.Background(), tc.args, Dependencies{In: strings.NewReader(tc.input), Out: &out, Err: &stderr, Getenv: func(string) string { return "" }, Store: tc.store, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://api.close.com/api/v1/me/" {
					t.Fatal(r.URL)
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.reply))}, nil
			})}, Build{Version: "test"})
			if code != tc.want {
				t.Fatalf("got=%d want=%d out=%s err=%s", code, tc.want, out.String(), stderr.String())
			}
			if strings.Contains(out.String(), "synthetic-key") || strings.Contains(stderr.String(), "synthetic-key") {
				t.Fatal("leaked key")
			}
		})
	}
}

func TestReadVariants(t *testing.T) {
	for _, args := range [][]string{
		{"me"}, {"api", "GET", "me/"}, {"contact", "get", "cont_demo", "--api-fields", "id,name"},
		{"custom-field", "list", "--object", "lead"}, {"event", "list", "--filter", "_cursor=next"},
		{"lead", "list", "--api-fields", "id,name", "--plain"}, {"lead", "list", "--quiet"},
		{"lead", "list", "--output", "ndjson"}, {"lead", "list", "--json"},
		{"search", "--data", `{"query":{"type":"object_type","object_type":"lead"}}`, "--all"},
		{"--readonly", "api", "POST", "data/search/", "--data", `{"query":{}}`, "--all"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, out, stderr := run(t, args, "", func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "me/") || strings.Contains(r.URL.Path, "cont_demo") {
					_, _ = io.WriteString(w, `{"id":"cont_demo","name":"Demo"}`)
				} else {
					_, _ = io.WriteString(w, `{"data":[{"id":"lead_demo","name":"Demo"}],"has_more":false,"cursor":null}`)
				}
			})
			if code != 0 || out == "" || stderr != "" {
				t.Fatalf("%d %q %q", code, out, stderr)
			}
		})
	}
}

func TestInputFilesAndAdditionalErrors(t *testing.T) {
	file := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(file, []byte(`{"name":"Demo"}`), 0600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := run(t, []string{"lead", "create", "--data-file", file, "--dry-run"}, "", nil)
	if code != 0 || !strings.Contains(out, "Demo") || stderr != "" {
		t.Fatal(code, out, stderr)
	}
	for _, args := range [][]string{
		{"lead", "create", "--data-file", file + "missing"},
		{"lead", "create"}, {"api", "TRACE", "me/"}, {"api", "GET", "me/", "--data", "{}"},
		{"api", "POST", "data/search/"}, {"api", "POST", "lead/", "--all", "--data", "{}", "--confirm"},
		{"search"}, {"schema", "unknown"}, {"--account", "../bad", "version"},
		{"version", "--timeout", "0s"}, {"version", "--fields", "id,,name"},
		{"version", "--json", "--output", "table"}, {"unknown"}, {"lead", "get"},
	} {
		code, out, stderr := run(t, args, "", nil)
		if code != 2 || out != "" || stderr == "" {
			t.Fatalf("%v: %d %q %q", args, code, out, stderr)
		}
	}
	code, _, _ = run(t, []string{"lead", "list"}, "", nil)
	if code != 4 {
		t.Fatal(code)
	}
}

func TestExplicitAccountCannotUseInheritedEnvironmentCredential(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		account string
	}{
		{[]string{"--account", "client-b", "lead", "list"}, ""},
		{[]string{"lead", "list"}, "client-b"},
	} {
		var out, stderr bytes.Buffer
		calls := 0
		code := Execute(context.Background(), tc.args, Dependencies{Out: &out, Err: &stderr, Store: &memoryStore{}, Getenv: func(k string) string {
			switch k {
			case "CLOSE_API_KEY":
				return "inherited-client-a"
			case "CLOSE_ACCOUNT":
				return tc.account
			}
			return ""
		}, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[],"has_more":false}`))}, nil
		})}, Build{})
		if code != 2 || calls != 0 {
			t.Fatalf("code=%d calls=%d stderr=%s", code, calls, stderr.String())
		}
	}
}

func TestFormattingCannotHideUnverifiedWrite(t *testing.T) {
	for _, args := range [][]string{
		{"api", "DELETE", "lead/lead_demo/", "--confirm", "--quiet"},
		{"lead", "create", "--data", `{"name":"Demo"}`, "--quiet"},
	} {
		code, _, stderr := run(t, args, "", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "DELETE" {
				w.WriteHeader(204)
			} else {
				_, _ = io.WriteString(w, `{}`)
			}
		})
		if code != 9 || !strings.Contains(stderr, "unverified") {
			t.Fatalf("%v: code=%d err=%s", args, code, stderr)
		}
	}
}

func TestOutputFlagsOverrideEnvironmentDefaults(t *testing.T) {
	for _, tc := range []struct {
		env      string
		args     []string
		wantJSON bool
	}{
		{"CLOSE_PLAIN", []string{"version", "--output", "json"}, true},
		{"CLOSE_JSON", []string{"version", "--plain"}, false},
	} {
		var out, stderr bytes.Buffer
		code := Execute(context.Background(), tc.args, Dependencies{Out: &out, Err: &stderr, Getenv: func(k string) string {
			if k == tc.env {
				return "1"
			}
			return ""
		}, Store: &memoryStore{}}, Build{Version: "test"})
		if code != 0 || json.Valid(out.Bytes()) != tc.wantJSON {
			t.Fatalf("code=%d out=%s err=%s", code, out.String(), stderr.String())
		}
	}
}
