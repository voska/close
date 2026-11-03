package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/voska/close/internal/errfmt"
)

func fixture(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	c, err := New(s.URL+"/api/v1/", "test-key", false, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.Wait = func(context.Context, time.Duration) error { return nil }
	return c
}

func TestRequestContract(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		key, password, ok := r.BasicAuth()
		if !ok || key != "test-key" || password != "" || r.URL.Path != "/api/v1/lead/" || r.URL.Query().Get("query") != "name:Demo & Co" {
			t.Errorf("bad request: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"lead_demo","unknown":9007199254740993}],"has_more":false}`))
	})
	v, err := c.Do(context.Background(), "GET", "lead/", url.Values{"query": {"name:Demo & Co"}}, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if v["data"].([]any)[0].(map[string]any)["unknown"].(json.Number).String() != "9007199254740993" {
		t.Fatal(v)
	}
}

func TestRetryReadsNeverWrites(t *testing.T) {
	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			n := 0
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				n++
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(429)
				_, _ = w.Write([]byte(`{"error":"slow down"}`))
			})
			_, err := c.Do(context.Background(), method, "lead/", nil, nil, method == "GET")
			var e *errfmt.Error
			if !errors.As(err, &e) || e.Code != 7 {
				t.Fatalf("%v", err)
			}
			want := 1
			if method == "GET" {
				want = 3
			}
			if n != want {
				t.Fatalf("calls %d want %d", n, want)
			}
		})
	}
}

func TestRejectPathAndOrigin(t *testing.T) {
	for _, p := range []string{"https://evil.test/", "//evil.test/", "../lead", "lead/../me", "lead/%2e%2e/me", "lead?x=y", "lead#x", "lead\\x", "lead/\n"} {
		if _, err := Endpoint(p); err == nil {
			t.Errorf("accepted %q", p)
		}
	}
	for _, origin := range []string{"http://api.close.com/", "https://evil.test/", "http://localhost:1234/", "https://api.close.com/api/v1/?key=x", "https://user:pass@api.close.com/api/v1/"} {
		if _, err := New(origin, "test", false, time.Second); err == nil {
			t.Errorf("accepted %q", origin)
		}
	}
}

func TestRedirectDoesNotForwardCredentials(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	if _, err := c.Do(context.Background(), "GET", "lead/", nil, nil, true); err == nil || called {
		t.Fatalf("redirect followed: %v %v", called, err)
	}
}

func TestPagination(t *testing.T) {
	for _, mode := range []string{"offset", "event", "search"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				if calls == 1 {
					switch mode {
					case "offset":
						_, _ = w.Write([]byte(`{"data":[{"id":"one"}],"has_more":true}`))
					case "event":
						_, _ = w.Write([]byte(`{"data":[{"id":"one"}],"cursor_next":"next"}`))
					case "search":
						_, _ = w.Write([]byte(`{"data":[{"id":"one"}],"cursor":"next"}`))
					}
					return
				}
				switch mode {
				case "offset":
					if r.URL.Query().Get("_skip") != "1" {
						t.Error(r.URL)
					}
				case "event":
					if r.URL.Query().Get("_cursor") != "next" {
						t.Error(r.URL)
					}
				case "search":
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["cursor"] != "next" || body["query"] == nil {
						t.Error(body)
					}
				}
				_, _ = w.Write([]byte(`{"data":[{"id":"two"}],"has_more":false,"cursor":null,"cursor_next":null}`))
			})
			method, path := "GET", "lead/"
			var body map[string]any
			if mode == "event" {
				path = "event/"
			}
			if mode == "search" {
				method = "POST"
				path = "data/search/"
				body = map[string]any{"query": map[string]any{"type": "and"}}
			}
			v, err := c.List(context.Background(), method, path, url.Values{"_limit": {"1"}}, body, true)
			if err != nil || len(v["data"].([]any)) != 2 || calls != 2 {
				t.Fatalf("%v %v calls=%d", v, err, calls)
			}
		})
	}
}

func TestPaginationFailsWithoutProgress(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"data":[],"has_more":true}`)) })
	if _, err := c.List(context.Background(), "GET", "lead/", nil, nil, true); err == nil {
		t.Fatal("accepted non-progress")
	}
	c = fixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"same"}],"cursor_next":"same"}`))
	})
	if _, err := c.List(context.Background(), "GET", "event/", nil, nil, true); err == nil {
		t.Fatal("accepted repeated cursor")
	}
}

func TestPaginationRejectsRepeatedOffsetPage(t *testing.T) {
	calls := 0
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		// Stop the fixture after three requests so a broken client cannot hang.
		_, _ = w.Write([]byte(`{"data":[{"id":"same"}],"has_more":` + map[bool]string{true: "true", false: "false"}[calls < 3] + `}`))
	})
	_, err := c.List(context.Background(), "GET", "lead/", nil, nil, true)
	if err == nil || calls != 2 {
		t.Fatalf("repeated page accepted: err=%v calls=%d", err, calls)
	}
}

func TestWriteReadback(t *testing.T) {
	for _, match := range []bool{true, false} {
		t.Run(map[bool]string{true: "match", false: "mismatch"}[match], func(t *testing.T) {
			calls := 0
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method == "PUT" {
					_, _ = w.Write([]byte(`{"id":"lead_demo","name":"Demo"}`))
					return
				}
				if r.URL.RawQuery != "" {
					t.Error("verification used filters")
				}
				name := "Demo"
				if !match {
					name = "Old"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "lead_demo", "name": name, "extra": true})
			})
			_, err := c.Write(context.Background(), "PUT", "lead/lead_demo/", map[string]any{"name": "Demo"})
			if (err == nil) != match || calls != 2 {
				t.Fatalf("match=%v err=%v calls=%d", match, err, calls)
			}
		})
	}
}

func TestSubset(t *testing.T) {
	if !Subset(map[string]any{"x": []any{map[string]any{"n": json.Number("1")}}}, map[string]any{"x": []any{map[string]any{"n": json.Number("1.0"), "extra": true}}}) {
		t.Fatal("subset")
	}
	if Subset(map[string]any{"missing": nil}, map[string]any{}) {
		t.Fatal("missing != null")
	}
	if Subset([]any{true}, []any{true, false}) {
		t.Fatal("different arrays")
	}
}

func TestErrorsRedactCredential(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"test-key"}`))
	})
	_, err := c.Do(context.Background(), "GET", "me/", nil, nil, true)
	if err == nil || strings.Contains(err.Error(), "test-key") {
		t.Fatal(err)
	}
}

func TestWriteFailureAndDeleteContracts(t *testing.T) {
	for _, tc := range []struct {
		name, method, reply, observed string
		writeStatus, readStatus, want int
	}{
		{"create-no-id", "POST", `{}`, `{}`, 200, 200, 9},
		{"create-bad-id", "POST", `{"id":"../me"}`, `{}`, 200, 200, 9},
		{"create-read-failed", "POST", `{"id":"lead_demo"}`, `{}`, 201, 403, 9},
		{"create-verified", "POST", `{"id":"lead_demo"}`, `{"id":"lead_demo","name":"Demo"}`, 201, 200, 0},
		{"delete-verified", "DELETE", `{}`, `{}`, 204, 404, 0},
		{"delete-repeat", "DELETE", `{}`, `{}`, 404, 404, 0},
		{"delete-still-exists", "DELETE", `{}`, `{"id":"lead_demo"}`, 204, 200, 9},
		{"write-server-error", "PUT", `{}`, `{}`, 503, 200, 9},
		{"write-validation", "PUT", `{}`, `{}`, 400, 200, 2},
		{"wrong-read-id", "PUT", `{}`, `{"id":"lead_other","name":"Demo"}`, 200, 200, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					w.WriteHeader(tc.readStatus)
					_, _ = w.Write([]byte(tc.observed))
				} else {
					writes++
					w.WriteHeader(tc.writeStatus)
					if tc.writeStatus != 204 {
						_, _ = w.Write([]byte(tc.reply))
					}
				}
			})
			path := "lead/lead_demo/"
			if tc.method == "POST" {
				path = "lead/"
			}
			var body map[string]any
			if tc.method != "DELETE" {
				body = map[string]any{"name": "Demo"}
			}
			v, err := c.Write(context.Background(), tc.method, path, body)
			var e *errfmt.Error
			got := 0
			if err != nil {
				if !errors.As(err, &e) {
					t.Fatal(err)
				}
				got = e.Code
			}
			if got != tc.want || writes != 1 {
				t.Fatalf("code=%d want=%d writes=%d err=%v", got, tc.want, writes, err)
			}
			if tc.name == "delete-repeat" && (v["deleted"] != false || v["already_absent"] != true) {
				t.Fatalf("misreported repeated delete: %v", v)
			}
		})
	}
}

func TestHTTPErrorCodesAndDecode(t *testing.T) {
	for status, want := range map[int]int{400: 2, 401: 4, 403: 6, 404: 5, 408: 8, 422: 2, 429: 7, 500: 8, 302: 1} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Request-Id", "req_demo")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"demo"}`))
			})
			_, err := c.Do(context.Background(), "GET", "me/", nil, nil, false)
			var e *errfmt.Error
			if !errors.As(err, &e) || e.Code != want || e.RequestID != "req_demo" {
				t.Fatal(err)
			}
		})
	}
	for _, raw := range []string{`null`, `[]`, `{} {}`, `invalid`} {
		if _, err := Decode([]byte(raw)); err == nil {
			t.Fatal(raw)
		}
	}
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`invalid`)) })
	if _, err := c.Do(context.Background(), "GET", "me/", nil, nil, true); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if _, err := New(DefaultURL, "x", false, 0); err == nil {
		t.Fatal("zero timeout")
	}
	if _, err := New(":", "x", false, time.Second); err == nil {
		t.Fatal("invalid origin")
	}
}

func TestRateDelayAndCancellation(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	if retryDelay(resp, nil, 1) != 2*time.Second {
		t.Fatal("backoff")
	}
	resp.Header.Set("Retry-After", "1")
	if retryDelay(resp, []byte(`{"rate_reset":1.5}`), 0) != 1500*time.Millisecond {
		t.Fatal("legacy fractional reset")
	}
	for _, raw := range []string{`{"rate_reset":"1.5"}`, `{"error":{"rate_reset":"1.5"}}`} {
		if retryDelay(resp, []byte(raw), 0) != 1500*time.Millisecond {
			t.Fatalf("legacy string reset: %s", raw)
		}
	}
	resp.Header.Set("Retry-After", "90")
	if retryDelay(resp, nil, 0) <= 30*time.Second {
		t.Fatal("long delay")
	}
	resp.Header.Set("Retry-After", time.Now().Add(2*time.Second).UTC().Format(http.TimeFormat))
	if d := retryDelay(resp, nil, 0); d < 0 || d > 3*time.Second {
		t.Fatal(d)
	}
	n := 0
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		n++
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(429)
	})
	if _, err := c.Do(context.Background(), "GET", "me/", nil, nil, true); err == nil || n != 1 {
		t.Fatal("retried before reset")
	}
	c = fixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	c.Wait = func(context.Context, time.Duration) error { return context.Canceled }
	if _, err := c.Do(context.Background(), "GET", "me/", nil, nil, true); err == nil {
		t.Fatal("ignored cancellation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Write(ctx, "PUT", "lead/lead_demo/", nil); err == nil {
		t.Fatal("cancelled write succeeded")
	}
}

func TestBearerAndFailedLaterPage(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("bearer missing")
		}
		_, _ = w.Write([]byte(`{"id":"user_demo"}`))
	})
	c.Bearer = true
	if _, err := c.Do(context.Background(), "GET", "me/", nil, nil, true); err != nil {
		t.Fatal(err)
	}
	calls := 0
	c = fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"data":[{"id":"first"}],"has_more":true}`))
		} else {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"offset cap"}`))
		}
	})
	if v, err := c.List(context.Background(), "GET", "lead/", nil, nil, true); err == nil || v != nil {
		t.Fatal("partial aggregate", v, err)
	}
	c = fixture(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"unexpected":[]}`)) })
	if _, err := c.List(context.Background(), "GET", "lead/", nil, nil, false); err == nil {
		t.Fatal("missing data")
	}
}
