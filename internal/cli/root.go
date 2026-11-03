package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/voska/close/internal/api"
	"github.com/voska/close/internal/auth"
	"github.com/voska/close/internal/errfmt"
	"github.com/voska/close/internal/output"
)

type Dependencies struct {
	In        io.Reader
	Out       io.Writer
	Err       io.Writer
	Getenv    func(string) string
	Store     auth.Store
	Transport http.RoundTripper
}

type Build struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

type app struct {
	dep                                            Dependencies
	build                                          Build
	root                                           *cobra.Command
	mode, fields, account                          string
	json, plain, quiet, readonly, noInput, noColor bool
	timeout                                        time.Duration
}

func Execute(ctx context.Context, args []string, dep Dependencies, build Build) int {
	if dep.In == nil {
		dep.In = os.Stdin
	}
	if dep.Out == nil {
		dep.Out = os.Stdout
	}
	if dep.Err == nil {
		dep.Err = os.Stderr
	}
	if dep.Getenv == nil {
		dep.Getenv = os.Getenv
	}
	if dep.Store == nil {
		dep.Store = auth.Keychain{}
	}
	a := &app{dep: dep, build: build}
	root := a.command()
	root.SetArgs(args)
	root.SetIn(dep.In)
	root.SetOut(dep.Out)
	root.SetErr(dep.Err)
	if err := root.ExecuteContext(ctx); err != nil {
		var e *errfmt.Error
		if !errors.As(err, &e) {
			e = errfmt.New(2, "usage", "%s; run close --help", err)
		}
		_ = json.NewEncoder(dep.Err).Encode(map[string]any{"error": e})
		return e.Code
	}
	return 0
}

func (a *app) command() *cobra.Command {
	root := &cobra.Command{Use: "close", Short: "Close CRM from the terminal", Version: a.build.Version, SilenceUsage: true, SilenceErrors: true,
		Long:    "Read and update Close CRM with JSON output, verified writes and explicit pagination.\nIndependent CLI; not affiliated with Close. Start with close auth status.",
		Example: "  close lead list --fields id,display_name --all\n  close contact get cont_demo\n  close activity note create --data-file note.json --dry-run\n  close api GET /me/\n  close schema lead list",
	}
	a.root = root
	f := root.PersistentFlags()
	f.StringVar(&a.mode, "output", "json", "Output format: json, table, tsv or ndjson")
	f.BoolVar(&a.json, "json", false, "Output JSON (default)")
	f.BoolVar(&a.plain, "plain", false, "Output TSV without a header")
	f.BoolVar(&a.quiet, "quiet", false, "Output only record IDs (or the version)")
	f.StringVar(&a.fields, "fields", "", "Project output to comma-separated field names; nested paths supported")
	account := a.dep.Getenv("CLOSE_ACCOUNT")
	if account == "" {
		account = "default"
	}
	f.StringVar(&a.account, "account", account, "OS keychain account (CLOSE_ACCOUNT)")
	f.BoolVar(&a.readonly, "readonly", a.dep.Getenv("CLOSE_READONLY") == "1", "Reject all CRM mutations")
	f.DurationVar(&a.timeout, "timeout", 30*time.Second, "HTTP timeout per request, including response body")
	f.BoolVar(&a.noInput, "no-input", false, "Never prompt (all commands are noninteractive)")
	f.BoolVar(&a.noColor, "no-color", false, "Disable color (output never contains terminal color codes)")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return errfmt.New(2, "usage", "%s", err) })
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if !cmd.Flags().Changed("json") && !cmd.Flags().Changed("plain") && !cmd.Flags().Changed("output") {
			a.json = a.dep.Getenv("CLOSE_JSON") == "1"
			a.plain = a.dep.Getenv("CLOSE_PLAIN") == "1"
		}
		if a.json && a.plain {
			return usage("--json and --plain are mutually exclusive")
		}
		if (a.json || a.plain) && cmd.Flags().Changed("output") {
			return usage("use one of --output, --json or --plain")
		}
		if a.json {
			a.mode = "json"
		}
		if a.plain {
			a.mode = "tsv"
		}
		if a.mode != "json" && a.mode != "table" && a.mode != "tsv" && a.mode != "ndjson" {
			return usage("--output must be json, table, tsv or ndjson")
		}
		if a.timeout <= 0 {
			return usage("--timeout must be positive")
		}
		if err := auth.ValidateAccount(a.account); err != nil {
			return err
		}
		if a.fields != "" {
			for _, f := range strings.Split(a.fields, ",") {
				if strings.TrimSpace(f) == "" || strings.ContainsAny(f, "\r\n\t\x1b") {
					return usage("--fields requires nonempty comma-separated field names")
				}
			}
		}
		return nil
	}
	root.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	a.addResources()
	a.addAuth()
	a.addAPI()
	a.addSearch()
	a.addSchema()
	root.AddCommand(&cobra.Command{Use: "version", Short: "Print version, commit and build date", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		return a.print(map[string]any{"version": a.build.Version, "commit": a.build.Commit, "date": a.build.Date})
	}})
	root.AddCommand(&cobra.Command{Use: "me", Short: "Get the authenticated user", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := a.client()
		if err != nil {
			return err
		}
		v, err := c.Do(cmd.Context(), "GET", "me/", nil, nil, true)
		return a.result(v, err)
	}})
	return root
}

func usage(message string) error { return errfmt.New(2, "usage", "%s", message) }

func (a *app) origin() string {
	if u := a.dep.Getenv("CLOSE_BASE_URL"); u != "" {
		return u
	}
	return api.DefaultURL
}

func (a *app) credential() (auth.Credential, error) {
	if (a.root.PersistentFlags().Changed("account") || a.dep.Getenv("CLOSE_ACCOUNT") != "") && (a.dep.Getenv("CLOSE_ACCESS_TOKEN") != "" || a.dep.Getenv("CLOSE_API_KEY") != "") {
		return auth.Credential{}, usage("explicit --account/CLOSE_ACCOUNT conflicts with CLOSE_ACCESS_TOKEN/CLOSE_API_KEY; unset environment credentials to use the selected keychain account")
	}
	return auth.Resolve(a.dep.Getenv, a.dep.Store, a.account, a.origin() == api.DefaultURL)
}

func (a *app) client() (*api.Client, error) {
	// Validate the origin before looking up a credential.
	if _, err := api.New(a.origin(), "", false, a.timeout); err != nil {
		return nil, err
	}
	c, err := a.credential()
	if err != nil {
		return nil, err
	}
	return a.newClient(c.Secret, c.Bearer)
}

func (a *app) newClient(secret string, bearer bool) (*api.Client, error) {
	c, err := api.New(a.origin(), secret, bearer, a.timeout)
	if err == nil && a.dep.Transport != nil {
		c.HTTP.Transport = a.dep.Transport
	}
	return c, err
}

func (a *app) print(v map[string]any) error {
	var fields []string
	if a.fields != "" {
		for _, f := range strings.Split(a.fields, ",") {
			fields = append(fields, strings.TrimSpace(f))
		}
	}
	if err := output.Write(a.dep.Out, v, a.mode, fields, a.quiet); err != nil {
		var e *errfmt.Error
		if errors.As(err, &e) {
			return e
		}
		return errfmt.New(1, "output", "could not write output: %v", err)
	}
	return nil
}

func (a *app) result(v map[string]any, err error) error {
	if v != nil {
		if outputErr := a.print(v); outputErr != nil && err == nil {
			return outputErr
		}
	}
	return err
}
