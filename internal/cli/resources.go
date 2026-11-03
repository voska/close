package cli

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/voska/close/internal/api"
	"github.com/voska/close/internal/errfmt"
)

type resource struct {
	name, path, description string
	write, get              bool
}

var resources = []resource{
	{"lead", "lead/", "Companies and organizations", true, true},
	{"contact", "contact/", "People within leads", true, true},
	{"opportunity", "opportunity/", "Deals and pipeline progress", true, true},
	{"task", "task/", "Tasks and follow-ups", true, true},
	{"activity", "activity/", "All activity types", false, false},
	{"activity note", "activity/note/", "Notes on leads", true, true},
	{"activity call", "activity/call/", "Logged external calls", true, true},
	{"activity email", "activity/email/", "Email drafts and queue state; delivery is not verified", true, true},
	{"activity sms", "activity/sms/", "SMS records and queue state; delivery is not verified", true, true},
	{"activity custom", "activity/custom/", "Custom activity instances", true, true},
	{"activity meeting", "activity/meeting/", "Meeting records", false, true},
	{"pipeline", "pipeline/", "Opportunity pipelines", false, true},
	{"lead-status", "status/lead/", "Lead statuses", false, true},
	{"opportunity-status", "status/opportunity/", "Opportunity statuses", false, true},
	{"custom-field", "custom_field/", "Custom field definitions; select --object", false, true},
	{"user", "user/", "Organization users", false, true},
	{"sequence", "sequence/", "Sequences (workflows)", false, true},
	{"smart-view", "saved_search/", "Saved searches and smart views", false, true},
	{"event", "event/", "Event log (last 30 days)", false, true},
}

func (a *app) addResources() {
	groups := map[string]*cobra.Command{}
	for _, r := range resources {
		parts := strings.Split(r.name, " ")
		parent := a.root
		key := ""
		for _, part := range parts {
			key = strings.TrimSpace(key + " " + part)
			group := groups[key]
			if group == nil {
				group = &cobra.Command{Use: part, Short: r.description}
				parent.AddCommand(group)
				groups[key] = group
			}
			parent = group
		}
		var object string
		if r.name == "custom-field" {
			parent.PersistentFlags().StringVar(&object, "object", "", "Required: lead, contact, opportunity, activity, shared or custom_object_type")
		}
		a.addRead(parent, r, &object)
		if r.write {
			for _, action := range []string{"create", "update", "delete"} {
				a.addWrite(parent, r, action)
			}
		}
	}
}

func resourcePath(r resource, object string) (string, error) {
	if r.name != "custom-field" {
		return r.path, nil
	}
	switch object {
	case "lead", "contact", "opportunity", "activity", "shared", "custom_object_type":
		return r.path + object + "/", nil
	}
	return "", usage("custom-field requires --object lead|contact|opportunity|activity|shared|custom_object_type")
}

type listFlags struct {
	filters     []string
	limit, skip int
	all         bool
	apiFields   string
}

func (f *listFlags) add(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&f.filters, "filter", nil, "API query parameter key=value (repeatable; values are URL-encoded)")
	cmd.Flags().IntVar(&f.limit, "limit", 50, "Records per page (endpoint maximum applies)")
	cmd.Flags().IntVar(&f.skip, "skip", 0, "Initial offset; unsupported for event cursors")
	cmd.Flags().BoolVar(&f.all, "all", false, "Collect every page; API errors fail without a partial aggregate")
	cmd.Flags().StringVar(&f.apiFields, "api-fields", "", "Request these fields from Close (_fields); --fields projects locally")
}

func (f *listFlags) query(path string) (url.Values, error) {
	if f.limit < 1 || f.limit > 1000 || f.skip < 0 {
		return nil, usage("--limit must be 1-1000 and --skip must be nonnegative (endpoint limits may be lower)")
	}
	q, err := filters(f.filters)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"_limit", "_skip"} {
		if q.Has(key) {
			return nil, usage("use --limit/--skip instead of pagination filters")
		}
	}
	q.Set("_limit", strconv.Itoa(f.limit))
	if strings.Trim(path, "/") == "event" {
		if f.skip != 0 {
			return nil, usage("event uses --filter _cursor=TOKEN instead of --skip")
		}
	} else {
		q.Set("_skip", strconv.Itoa(f.skip))
	}
	if f.apiFields != "" {
		q.Set("_fields", f.apiFields)
	}
	return q, nil
}

func filters(values []string) (url.Values, error) {
	q := url.Values{}
	for _, filter := range values {
		k, v, ok := strings.Cut(filter, "=")
		if !ok || k == "" || strings.ContainsAny(k, "\x00\r\n\t&?# ") {
			return nil, usage("--filter expects key=value")
		}
		q.Add(k, v)
	}
	return q, nil
}

func (a *app) addRead(parent *cobra.Command, r resource, object *string) {
	f := &listFlags{}
	list := &cobra.Command{Use: "list", Short: "List records (one page unless --all)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		path, err := resourcePath(r, *object)
		if err != nil {
			return err
		}
		q, err := f.query(path)
		if err != nil {
			return err
		}
		c, err := a.client()
		if err != nil {
			return err
		}
		v, err := c.List(cmd.Context(), "GET", path, q, nil, f.all)
		return a.result(v, err)
	}}
	f.add(list)
	parent.AddCommand(list)
	if !r.get {
		return
	}
	var apiFields string
	get := &cobra.Command{Use: "get ID", Short: "Get an individual record", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		path, err := resourcePath(r, *object)
		if err != nil {
			return err
		}
		path, err = withID(path, args[0])
		if err != nil {
			return err
		}
		q := url.Values{}
		if apiFields != "" {
			q.Set("_fields", apiFields)
		}
		c, err := a.client()
		if err != nil {
			return err
		}
		v, err := c.Do(cmd.Context(), "GET", path, q, nil, true)
		return a.result(v, err)
	}}
	get.Flags().StringVar(&apiFields, "api-fields", "", "Request these fields from Close (_fields)")
	parent.AddCommand(get)
}

func withID(path, id string) (string, error) {
	if id == "" || strings.Contains(id, "/") {
		return "", usage("ID must be a single resource identifier")
	}
	if _, err := api.Endpoint(id); err != nil {
		return "", err
	}
	return path + id + "/", nil
}

type bodyFlags struct{ data, file string }

func (b *bodyFlags) add(cmd *cobra.Command) {
	cmd.Flags().StringVar(&b.data, "data", "", "Request body as a JSON object")
	cmd.Flags().StringVar(&b.file, "data-file", "", "Read JSON object from a file, or - for stdin")
}
func (b *bodyFlags) read(cmd *cobra.Command, in io.Reader, required bool) (map[string]any, error) {
	hasData, hasFile := cmd.Flags().Changed("data"), cmd.Flags().Changed("data-file")
	if hasData && hasFile {
		return nil, usage("--data and --data-file are mutually exclusive")
	}
	if !hasData && !hasFile {
		if required {
			return nil, usage("provide --data JSON or --data-file PATH|- ")
		}
		return nil, nil
	}
	var raw []byte
	if hasData {
		raw = []byte(b.data)
	} else {
		reader := in
		if b.file != "-" {
			f, err := os.Open(b.file)
			if err != nil {
				return nil, errfmt.New(2, "usage", "cannot open --data-file: %v", err)
			}
			defer f.Close()
			reader = f
		}
		var err error
		raw, err = io.ReadAll(io.LimitReader(reader, (1<<20)+1))
		if err != nil {
			return nil, errfmt.New(2, "usage", "cannot read JSON input: %v", err)
		}
	}
	if len(raw) > 1<<20 {
		return nil, usage("request body exceeds 1 MiB")
	}
	v, err := api.Decode(raw)
	if err != nil {
		return nil, usage("request body must contain exactly one JSON object")
	}
	return v, nil
}

type mutationFlags struct{ confirm, dryRun bool }

func (f *mutationFlags) add(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.confirm, "confirm", false, "Confirm deletion, sending or a raw API mutation")
	cmd.Flags().BoolVarP(&f.dryRun, "dry-run", "n", false, "Print request preview without credentials or network activity")
}
func (a *app) mutation(f *mutationFlags, needsConfirm bool) error {
	if a.readonly {
		return usage("--readonly rejects CRM mutations")
	}
	if needsConfirm && !f.confirm && !f.dryRun {
		return usage("this operation requires --confirm; use --dry-run to preview")
	}
	return nil
}
func (a *app) preview(method, path string, q url.Values, body map[string]any) error {
	return a.print(map[string]any{"dry_run": true, "method": method, "path": "/api/v1/" + path, "query": q, "body": body})
}

func (a *app) addWrite(parent *cobra.Command, r resource, action string) {
	b, f := &bodyFlags{}, &mutationFlags{}
	method := map[string]string{"create": "POST", "update": "PUT", "delete": "DELETE"}[action]
	use := action
	args := cobra.NoArgs
	if action != "create" {
		use += " ID"
		args = cobra.ExactArgs(1)
	}
	cmd := &cobra.Command{Use: use, Short: fmt.Sprintf("%s a record and verify stored state", strings.ToUpper(action[:1])+action[1:]), Args: args, RunE: func(cmd *cobra.Command, args []string) error {
		path := r.path
		if len(args) > 0 {
			var err error
			path, err = withID(path, args[0])
			if err != nil {
				return err
			}
		}
		var body map[string]any
		if action != "delete" {
			var err error
			body, err = b.read(cmd, a.dep.In, true)
			if err != nil {
				return err
			}
		}
		if err := a.mutation(f, action == "delete" || r.name == "activity email" || r.name == "activity sms"); err != nil {
			return err
		}
		if f.dryRun {
			return a.preview(method, path, nil, body)
		}
		c, err := a.client()
		if err != nil {
			return err
		}
		v, err := c.Write(cmd.Context(), method, path, body)
		return a.result(v, err)
	}}
	if (r.name == "activity email" || r.name == "activity sms") && action != "delete" {
		cmd.Short = "Store or queue a message and verify its record; --confirm required"
		cmd.Long = "status=outbox or scheduled queues a send; status=draft stores a draft.\n--confirm is required for every write, including drafts.\nExit 0 verifies the stored record or queue state, not delivery.\nOn exit 9, read the record before retrying to avoid sending twice."
		cmd.Example = "  close " + r.name + " create --data-file draft.json --confirm\n  close " + r.name + " create --data-file send.json --dry-run\n  close " + r.name + " update acti_demo --data '{\"status\":\"outbox\"}' --confirm"
	}
	if action == "delete" {
		cmd.Short = "Delete a record and verify its absence; --confirm required"
		if r.name == "lead" {
			cmd.Long = "Deletes the lead and its contacts, opportunities, tasks and activities.\nRequires --confirm. Absence is verified by an individual GET returning 404."
		}
	}
	if action != "delete" {
		b.add(cmd)
	}
	f.add(cmd)
	parent.AddCommand(cmd)
}
