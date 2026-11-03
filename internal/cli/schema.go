package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/voska/close/internal/errfmt"
)

func commandSchema(cmd *cobra.Command) map[string]any {
	flags := []any{}
	seen := map[string]bool{}
	visit := func(f *pflag.Flag) {
		if seen[f.Name] {
			return
		}
		seen[f.Name] = true
		flags = append(flags, map[string]any{"name": f.Name, "type": f.Value.Type(), "default": f.DefValue, "description": f.Usage})
	}
	cmd.InheritedFlags().VisitAll(visit)
	cmd.LocalFlags().VisitAll(visit)
	children := []any{}
	for _, c := range cmd.Commands() {
		if !c.Hidden {
			children = append(children, commandSchema(c))
		}
	}
	return map[string]any{"command": cmd.CommandPath(), "use": cmd.Use, "description": cmd.Short, "flags": flags, "commands": children}
}

func (a *app) addSchema() {
	a.root.AddCommand(&cobra.Command{Use: "schema [COMMAND ... | exit-codes]", Short: "Print the command tree, flags and exit codes as JSON", Args: cobra.ArbitraryArgs, RunE: func(_ *cobra.Command, args []string) error {
		if len(args) == 1 && args[0] == "exit-codes" {
			return a.print(map[string]any{"exit_codes": errfmt.ExitCodes})
		}
		target := a.root
		for _, part := range args {
			var found *cobra.Command
			for _, child := range target.Commands() {
				if child.Name() == part {
					found = child
					break
				}
			}
			if found == nil {
				return usage("unknown command in schema: " + strings.Join(args, " "))
			}
			target = found
		}
		v := commandSchema(target)
		if target == a.root {
			v["exit_codes"] = errfmt.ExitCodes
			v["output_contract"] = map[string]any{"default": "json", "list": "API envelope with data array and pagination metadata", "errors": "stderr: {error: {code,type,message,http_status?,request_id?}}", "raw_writes": "exit 9; submitted, unverified"}
		}
		return a.print(v)
	}})
}
