package cli

import (
	"errors"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/voska/close/internal/api"
	"github.com/voska/close/internal/auth"
	"github.com/voska/close/internal/errfmt"
	"github.com/zalando/go-keyring"
)

func (a *app) addAuth() {
	group := &cobra.Command{Use: "auth", Short: "Verify credentials or manage API keys in the OS keychain"}
	var offline bool
	status := &cobra.Command{Use: "status", Short: "Check credentials against /me/ (--offline skips the network)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if _, err := api.New(a.origin(), "", false, a.timeout); err != nil {
			return err
		}
		cred, err := a.credential()
		if err != nil {
			var e *errfmt.Error
			if offline && errors.As(err, &e) && e.Code == 4 {
				return a.print(map[string]any{"configured": false, "verified": false, "account": a.account})
			}
			return err
		}
		v := map[string]any{"configured": true, "verified": false, "source": cred.Source}
		if cred.Source == "keychain" {
			v["account"] = a.account
		}
		if !offline {
			c, err := a.newClient(cred.Secret, cred.Bearer)
			if err != nil {
				return err
			}
			me, err := c.Do(cmd.Context(), "GET", "me/", nil, nil, true)
			if err != nil {
				return err
			}
			if id, ok := me["id"].(string); !ok || id == "" {
				return errfmt.New(1, "protocol", "/me/ response has no user ID")
			}
			v["verified"] = true
			v["user"] = me
		}
		return a.print(v)
	}}
	status.Flags().BoolVar(&offline, "offline", false, "Inspect local credential configuration without verifying it")
	var stdin bool
	login := &cobra.Command{Use: "login", Short: "Verify an API key from stdin, then store it in the OS keychain", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !stdin {
			return usage("provide --with-key-stdin and pipe the API key on stdin")
		}
		if a.origin() != api.DefaultURL {
			return usage("auth login only supports the production Close API")
		}
		raw, err := io.ReadAll(io.LimitReader(a.dep.In, 8193))
		if err != nil || len(raw) > 8192 {
			return usage("cannot read key or input exceeds 8 KiB")
		}
		secret := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
		if err := auth.ValidateSecret(secret, false); err != nil {
			return err
		}
		c, err := a.newClient(secret, false)
		if err != nil {
			return err
		}
		me, err := c.Do(cmd.Context(), "GET", "me/", nil, nil, true)
		if err != nil {
			return err
		}
		if id, ok := me["id"].(string); !ok || id == "" {
			return errfmt.New(1, "protocol", "/me/ response has no user ID; key was not stored")
		}
		if err := a.dep.Store.Set(a.account, secret); err != nil {
			return errfmt.New(10, "configuration", "cannot store key in OS keychain; use CLOSE_API_KEY for headless use")
		}
		stored, err := a.dep.Store.Get(a.account)
		if err != nil || stored != secret {
			return errfmt.New(10, "configuration", "could not verify stored key")
		}
		return a.print(map[string]any{"account": a.account, "source": "keychain", "verified": true, "user_id": me["id"]})
	}}
	login.Flags().BoolVar(&stdin, "with-key-stdin", false, "Read API key from stdin; never pass secrets as arguments")
	logout := &cobra.Command{Use: "logout", Short: "Remove the selected stored API key (environment variables remain active)", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		if err := a.dep.Store.Delete(a.account); err != nil {
			return errfmt.New(10, "configuration", "cannot delete stored key")
		}
		key, err := a.dep.Store.Get(a.account)
		if (err == nil && key != "") || (err != nil && !errors.Is(err, keyring.ErrNotFound)) {
			return errfmt.New(10, "configuration", "could not verify stored key removal")
		}
		return a.print(map[string]any{"account": a.account, "stored_key_removed": true})
	}}
	group.AddCommand(status, login, logout)
	a.root.AddCommand(group)
}
