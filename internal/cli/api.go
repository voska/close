package cli

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"
	"github.com/voska/close/internal/api"
	"github.com/voska/close/internal/errfmt"
)

func (a *app) addAPI() {
	b, f := &bodyFlags{}, &mutationFlags{}
	var values []string
	var all bool
	cmd := &cobra.Command{Use: "api METHOD PATH", Short: "Call any JSON API endpoint; raw writes exit 9 (unverified)", Args: cobra.ExactArgs(2), Example: "  close api GET /me/\n  close api GET /lead/ --filter _limit=10\n  close api POST /lead/ --data-file lead.json --dry-run", RunE: func(cmd *cobra.Command, args []string) error {
		method := strings.ToUpper(args[0])
		switch method {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			return usage("METHOD must be GET, POST, PUT, PATCH or DELETE")
		}
		path, err := api.Endpoint(args[1])
		if err != nil {
			return err
		}
		read := method == "GET" || (method == "POST" && path == "data/search/")
		body, err := b.read(cmd, a.dep.In, !read && method != "DELETE")
		if err != nil {
			return err
		}
		if method == "GET" && body != nil {
			return usage("GET does not accept a JSON body")
		}
		if method == "POST" && path == "data/search/" && body == nil {
			return usage("search requires --data or --data-file")
		}
		q, err := filters(values)
		if err != nil {
			return err
		}
		if all && !read {
			return usage("--all only applies to reads")
		}
		if !read {
			if err := a.mutation(f, true); err != nil {
				return err
			}
		}
		if f.dryRun {
			return a.preview(method, path, q, body)
		}
		c, err := a.client()
		if err != nil {
			return err
		}
		if all {
			v, err := c.List(cmd.Context(), method, path, q, body, true)
			return a.result(v, err)
		}
		v, err := c.Do(cmd.Context(), method, path, q, body, read)
		if !read {
			if err == nil {
				err = errfmt.New(9, "unverified", "raw write submitted, unverified; read the resulting resource before retrying")
			} else {
				var e *errfmt.Error
				if errors.As(err, &e) && (e.Code == 8 || e.Code == 1) {
					err = errfmt.New(9, "unverified", "raw write outcome unknown; do not retry blindly: %s", err)
				}
			}
		}
		return a.result(v, err)
	}}
	b.add(cmd)
	f.add(cmd)
	cmd.Flags().StringArrayVar(&values, "filter", nil, "API query parameter key=value (repeatable)")
	cmd.Flags().BoolVar(&all, "all", false, "Collect all pages of a list/search endpoint")
	a.root.AddCommand(cmd)
}

func (a *app) addSearch() {
	b := &bodyFlags{}
	var all bool
	cmd := &cobra.Command{Use: "search", Short: "Search objects with Close's advanced JSON filtering API", Args: cobra.NoArgs, Example: "  close search --data-file filters.json --all", RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := b.read(cmd, a.dep.In, true)
		if err != nil {
			return err
		}
		c, err := a.client()
		if err != nil {
			return err
		}
		v, err := c.List(cmd.Context(), "POST", "data/search/", nil, body, all)
		return a.result(v, err)
	}}
	b.add(cmd)
	cmd.Flags().BoolVar(&all, "all", false, "Collect cursor pages (Close caps searches at 10,000 results)")
	a.root.AddCommand(cmd)
}
