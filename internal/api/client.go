package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/voska/close/internal/errfmt"
)

const DefaultURL = "https://api.close.com/api/v1/"
const maxResponse = 32 << 20

type Client struct {
	Base   *url.URL
	HTTP   *http.Client
	Secret string
	Bearer bool
	Wait   func(context.Context, time.Duration) error
}

func New(origin, secret string, bearer bool, timeout time.Duration) (*Client, error) {
	u, err := url.Parse(origin)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, errfmt.New(10, "configuration", "invalid API origin")
	}
	ip := net.ParseIP(u.Hostname())
	loopback := ip != nil && ip.IsLoopback()
	if origin != DefaultURL && (!loopback || u.Scheme != "http" || u.Path != "/api/v1/") {
		return nil, errfmt.New(10, "configuration", "API origin must be %s or an HTTP loopback IP with /api/v1/", DefaultURL)
	}
	if timeout <= 0 {
		return nil, errfmt.New(2, "usage", "--timeout must be positive")
	}
	return &Client{Base: u, Secret: secret, Bearer: bearer, HTTP: &http.Client{
		Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, Wait: func(ctx context.Context, d time.Duration) error {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			return nil
		}
	}}, nil
}

var safePath = regexp.MustCompile(`^[A-Za-z0-9_.@+/-]+$`)

func Endpoint(path string) (string, error) {
	if strings.HasPrefix(path, "//") || !safePath.MatchString(path) {
		return "", errfmt.New(2, "usage", "use a relative API path without a URL, query, fragment or encoded characters")
	}
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return "", errfmt.New(2, "usage", "invalid API path segment")
		}
	}
	return path + "/", nil
}

func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body map[string]any, retry bool) (map[string]any, error) {
	p, err := Endpoint(path)
	if err != nil {
		return nil, err
	}
	u := *c.Base
	u.Path += p
	u.RawQuery = query.Encode()
	var payload []byte
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, errfmt.New(2, "usage", "invalid JSON body: %v", err)
		}
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(payload))
		if err != nil {
			return nil, errfmt.New(2, "usage", "invalid request")
		}
		if c.Bearer {
			req.Header.Set("Authorization", "Bearer "+c.Secret)
		} else {
			req.SetBasicAuth(c.Secret, "")
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "close-cli (github.com/voska/close)")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, errfmt.New(8, "retryable", "request failed or timed out; check connectivity before retrying")
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, errfmt.New(8, "retryable", "could not read API response")
		}
		if len(raw) > maxResponse {
			return nil, errfmt.New(1, "protocol", "API response exceeds 32 MiB; request fewer fields or smaller pages")
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if resp.StatusCode == 204 {
				return map[string]any{}, nil
			}
			v, err := Decode(raw)
			if err != nil {
				return nil, errfmt.New(1, "protocol", "API returned invalid JSON object")
			}
			return v, nil
		}
		e := c.responseError(resp, raw)
		if retry && attempt < 2 && (resp.StatusCode == 429 || resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504) {
			d := retryDelay(resp, raw, attempt)
			if d > 30*time.Second {
				return nil, e
			}
			if err := c.Wait(ctx, d); err != nil {
				return nil, errfmt.New(8, "retryable", "request cancelled during retry delay")
			}
			continue
		}
		return nil, e
	}
}

func Decode(raw []byte) (map[string]any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v map[string]any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if v == nil {
		return nil, fmt.Errorf("expected a JSON object")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("expected exactly one JSON object")
	}
	return v, nil
}

func retryDelay(resp *http.Response, raw []byte, attempt int) time.Duration {
	seconds := math.Pow(2, float64(attempt))
	if n, err := strconv.ParseFloat(resp.Header.Get("Retry-After"), 64); err == nil && n >= 0 {
		seconds = n
	} else if date, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil {
		seconds = math.Max(0, time.Until(date).Seconds())
	}
	if v, err := Decode(raw); err == nil {
		nested, _ := v["error"].(map[string]any)
		for _, reset := range []any{v["rate_reset"], nested["rate_reset"]} {
			if f, err := strconv.ParseFloat(fmt.Sprint(reset), 64); err == nil && f > seconds {
				seconds = f
			}
		}
	}
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > 30 {
		return 31 * time.Second
	}
	return time.Duration(seconds * float64(time.Second))
}

func (c *Client) responseError(resp *http.Response, raw []byte) *errfmt.Error {
	code, kind := 1, "api"
	switch resp.StatusCode {
	case 400, 405, 409, 422:
		code, kind = 2, "validation"
	case 401:
		code, kind = 4, "authentication"
	case 403:
		code, kind = 6, "forbidden"
	case 404:
		code, kind = 5, "not_found"
	case 429:
		code, kind = 7, "rate_limited"
	default:
		if resp.StatusCode >= 500 || resp.StatusCode == 408 {
			code, kind = 8, "retryable"
		}
	}
	message := fmt.Sprintf("Close API returned HTTP %d", resp.StatusCode)
	if v, err := Decode(raw); err == nil {
		b, _ := json.Marshal(v)
		detail := string(b)
		if c.Secret != "" {
			detail = strings.ReplaceAll(detail, c.Secret, "[redacted]")
			detail = strings.ReplaceAll(detail, base64.StdEncoding.EncodeToString([]byte(c.Secret+":")), "[redacted]")
		}
		if len(detail) > 500 {
			detail = detail[:500] + "..."
		}
		message += ": " + detail
	}
	if resp.StatusCode == 401 {
		message += "; set CLOSE_API_KEY or run close auth login --with-key-stdin"
	}
	return &errfmt.Error{Code: code, Type: kind, Message: message, HTTPStatus: resp.StatusCode, RequestID: resp.Header.Get("X-Request-Id")}
}

func (c *Client) List(ctx context.Context, method, path string, query url.Values, body map[string]any, all bool) (map[string]any, error) {
	q := url.Values{}
	for k, v := range query {
		q[k] = append([]string(nil), v...)
	}
	b := map[string]any{}
	for k, v := range body {
		b[k] = v
	}
	if body == nil {
		b = nil
	}
	rows := []any{}
	seenIDs := map[string]bool{}
	seenCursors := map[string]bool{}
	if s := q.Get("_cursor"); s != "" {
		seenCursors[s] = true
	}
	if s, _ := b["cursor"].(string); s != "" {
		seenCursors[s] = true
	}
	skip, _ := strconv.Atoi(q.Get("_skip"))
	for {
		v, err := c.Do(ctx, method, path, q, b, true)
		if err != nil {
			return nil, err
		}
		page, ok := v["data"].([]any)
		if !ok {
			return nil, errfmt.New(1, "protocol", "list response has no data array")
		}
		if !all {
			return v, nil
		}
		before := len(rows)
		for _, item := range page {
			if obj, ok := item.(map[string]any); ok {
				if id, ok := obj["id"].(string); ok && id != "" {
					if seenIDs[id] {
						continue
					}
					seenIDs[id] = true
				}
			}
			rows = append(rows, item)
		}
		cursor, _ := v["cursor_next"].(string)
		cursorKey := "_cursor"
		if strings.Trim(path, "/") == "data/search" {
			cursor, _ = v["cursor"].(string)
			cursorKey = "cursor"
		}
		if cursor != "" {
			if seenCursors[cursor] {
				return nil, errfmt.New(1, "protocol", "pagination repeated a cursor; no aggregate emitted")
			}
			seenCursors[cursor] = true
			if cursorKey == "cursor" {
				b["cursor"] = cursor
			} else {
				q.Set("_cursor", cursor)
			}
			continue
		}
		more, _ := v["has_more"].(bool)
		if !more {
			v["data"] = rows
			return v, nil
		}
		if len(rows) == before {
			return nil, errfmt.New(1, "protocol", "pagination made no progress with has_more=true; no aggregate emitted")
		}
		skip += len(page)
		q.Set("_skip", strconv.Itoa(skip))
	}
}

func (c *Client) Write(ctx context.Context, method, path string, body map[string]any) (map[string]any, error) {
	v, err := c.Do(ctx, method, path, nil, body, false)
	var e *errfmt.Error
	alreadyAbsent := method == "DELETE" && errors.As(err, &e) && e.Code == 5
	if err != nil && !alreadyAbsent {
		if errors.As(err, &e) && (e.Code == 8 || e.Code == 1) {
			return nil, unverified("write outcome unknown; do not retry blindly", err)
		}
		return nil, err
	}
	verifyPath := path
	if method == "POST" {
		id, ok := v["id"].(string)
		if !ok || id == "" {
			return v, unverified("write returned no ID", nil)
		}
		if strings.Contains(id, "/") {
			return v, unverified("write returned invalid ID", nil)
		}
		if _, err := Endpoint(id); err != nil {
			return v, unverified("write returned invalid ID", nil)
		}
		verifyPath = strings.TrimSuffix(path, "/") + "/" + id + "/"
	}
	observed, err := c.Do(ctx, "GET", verifyPath, nil, nil, true)
	if method == "DELETE" {
		if errors.As(err, &e) && e.Code == 5 {
			parts := strings.Split(strings.Trim(path, "/"), "/")
			result := map[string]any{"id": parts[len(parts)-1], "deleted": !alreadyAbsent}
			if alreadyAbsent {
				result["already_absent"] = true
			}
			return result, nil
		}
		return observed, unverified("delete submitted; absence could not be verified", err)
	}
	if err != nil {
		return v, unverified("write submitted; read-back failed", err)
	}
	parts := strings.Split(strings.Trim(verifyPath, "/"), "/")
	if observed["id"] != parts[len(parts)-1] || !Subset(body, observed) {
		return observed, unverified("write submitted; read-back did not match the ID and supplied fields", nil)
	}
	return observed, nil
}

func unverified(message string, cause error) *errfmt.Error {
	if cause != nil {
		message += ": " + cause.Error()
	}
	return errfmt.New(9, "unverified", "%s", message)
}

func Subset(want, got any) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range w {
			actual, exists := g[k]
			if !exists || !Subset(v, actual) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok || len(w) != len(g) {
			return false
		}
		for i, v := range w {
			if !Subset(v, g[i]) {
				return false
			}
		}
		return true
	case json.Number:
		g, ok := got.(json.Number)
		if !ok {
			return false
		}
		a, aok := new(big.Rat).SetString(string(w))
		b, bok := new(big.Rat).SetString(string(g))
		return aok && bok && a.Cmp(b) == 0
	default:
		return reflect.DeepEqual(want, got)
	}
}
