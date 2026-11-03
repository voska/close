package auth

import (
	"errors"
	"regexp"
	"strings"
	"unicode"

	"github.com/voska/close/internal/errfmt"
	"github.com/zalando/go-keyring"
)

type Store interface {
	Get(string) (string, error)
	Set(string, string) error
	Delete(string) error
}

type Keychain struct{}

func (Keychain) Get(account string) (string, error) { return keyring.Get("close-cli", account) }
func (Keychain) Set(account, key string) error      { return keyring.Set("close-cli", account, key) }
func (Keychain) Delete(account string) error {
	err := keyring.Delete("close-cli", account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

type Credential struct {
	Secret string `json:"-"`
	Bearer bool   `json:"-"`
	Source string `json:"source"`
}

var accountPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

func ValidateAccount(account string) error {
	if !accountPattern.MatchString(account) {
		return errfmt.New(2, "usage", "account must be 1-64 letters, digits, dots, underscores or hyphens, starting with a letter or digit")
	}
	return nil
}

func ValidateSecret(secret string, bearer bool) error {
	if secret == "" || strings.ContainsFunc(secret, unicode.IsSpace) || strings.ContainsFunc(secret, unicode.IsControl) || (!bearer && strings.Contains(secret, ":")) {
		return errfmt.New(10, "configuration", "credential is empty or contains invalid characters")
	}
	return nil
}

func Resolve(getenv func(string) string, store Store, account string, allowStored bool) (Credential, error) {
	if err := ValidateAccount(account); err != nil {
		return Credential{}, err
	}
	c := Credential{Secret: getenv("CLOSE_ACCESS_TOKEN"), Bearer: true, Source: "CLOSE_ACCESS_TOKEN"}
	if c.Secret == "" {
		c = Credential{Secret: getenv("CLOSE_API_KEY"), Source: "CLOSE_API_KEY"}
	}
	if c.Secret == "" && allowStored {
		key, err := store.Get(account)
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return Credential{}, errfmt.New(10, "configuration", "cannot read OS keychain; set CLOSE_API_KEY for headless use")
		}
		c = Credential{Secret: key, Source: "keychain"}
	}
	if c.Secret == "" {
		return Credential{}, errfmt.New(4, "authentication", "set CLOSE_API_KEY or run close auth login --with-key-stdin")
	}
	if err := ValidateSecret(c.Secret, c.Bearer); err != nil {
		return Credential{}, err
	}
	return c, nil
}
