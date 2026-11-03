package auth

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

type fakeStore struct {
	key   string
	err   error
	reads int
}

func (s *fakeStore) Get(string) (string, error)     { s.reads++; return s.key, s.err }
func (s *fakeStore) Set(_ string, key string) error { s.key = key; return s.err }
func (s *fakeStore) Delete(string) error            { s.key = ""; return s.err }

func TestResolve(t *testing.T) {
	for _, tc := range []struct {
		name, token, key, stored, want string
		allow                          bool
		err                            error
		fail                           bool
	}{
		{"bearer", "token", "key", "stored", "CLOSE_ACCESS_TOKEN", true, nil, false},
		{"env", "", "key", "stored", "CLOSE_API_KEY", true, nil, false},
		{"keychain", "", "", "stored", "keychain", true, nil, false},
		{"missing", "", "", "", "", true, keyring.ErrNotFound, true},
		{"headless", "", "", "", "", true, errors.New("no bus"), true},
		{"fixture", "", "", "stored", "", false, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &fakeStore{key: tc.stored, err: tc.err}
			c, err := Resolve(func(k string) string {
				if k == "CLOSE_ACCESS_TOKEN" {
					return tc.token
				}
				return tc.key
			}, s, "default", tc.allow)
			if (err != nil) != tc.fail {
				t.Fatal(err)
			}
			if !tc.fail && c.Source != tc.want {
				t.Fatal(c.Source)
			}
			if (!tc.allow || tc.token != "" || tc.key != "") && s.reads != 0 {
				t.Fatal("unexpected keychain lookup")
			}
			b, _ := json.Marshal(c)
			if strings.Contains(string(b), `"Secret"`) {
				t.Fatal(string(b))
			}
		})
	}
}

func TestValidation(t *testing.T) {
	for _, a := range []string{"", "../x", "bad\n", "a/b", strings.Repeat("x", 65)} {
		if ValidateAccount(a) == nil {
			t.Fatal(a)
		}
	}
	if ValidateAccount("work-2") != nil {
		t.Fatal("account")
	}
	for _, s := range []string{"", "a:b", "a\nb", " key", "key\x00"} {
		if ValidateSecret(s, false) == nil {
			t.Fatal(s)
		}
	}
	if ValidateSecret("bearer:token", true) != nil {
		t.Fatal("bearer")
	}
}
