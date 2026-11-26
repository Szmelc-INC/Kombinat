package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/Szmelc-INC/Kombinat/core/modules"
)

func moduleCommand() *Command {
	lifecycle := func(name, summary string, call func(*modules.Manager, context.Context, string, string) (modules.Result, error)) *Command {
		return &Command{
			Name:    name,
			Args:    "<name>",
			Summary: summary,
			Run: func(env *Env, args []string) int {
				path := []string{"module", name}
				if len(args) != 1 {
					return env.UsageError(path, "module %s takes exactly one module name", name)
				}
				core := env.OpenCore()
				if core == nil {
					return ExitError
				}
				defer core.Close()
				res, err := call(core.Modules, context.Background(), args[0], cliActor)
				if err != nil {
					env.Errorf("%v", err)
					return ExitError
				}
				if env.JSON {
					return env.PrintJSON(res)
				}
				if res.Changed {
					env.Okf("%s %s: now %s (event #%d)", name, res.Module, res.Status, res.EventID)
				} else {
					env.Okf("%s %s: already %s, nothing changed", name, res.Module, res.Status)
				}
				return ExitOK
			},
		}
	}
	return &Command{
		Name:    "module",
		Summary: "List, check and run the lifecycle of modules",
		Sub: []*Command{
			{
				Name:    "check",
				Summary: "Validate every module; exit 1 when any is invalid",
				Run:     moduleCheck,
			},
			lifecycle("disable", "Stop a module and turn it off", (*modules.Manager).Disable),
			lifecycle("enable", "Turn a module on and start it", (*modules.Manager).Enable),
			{
				Name:    "ls",
				Summary: "List every module with its version and state",
				Run:     moduleList,
			},
			lifecycle("start", "Start an enabled module that was stopped", (*modules.Manager).Start),
			lifecycle("stop", "Stop a module without disabling it", (*modules.Manager).Stop),
		},
	}
}

func moduleList(env *Env, args []string) int {
	if len(args) > 0 {
		return env.UsageError([]string{"module", "ls"}, "module ls takes no arguments")
	}
	core := env.OpenCore()
	if core == nil {
		return ExitError
	}
	defer core.Close()
	states, err := core.Modules.States(context.Background())
	if err != nil {
		env.Errorf("%v", err)
		return ExitError
	}
	views := make([]modules.View, 0, len(core.Modules.Modules))
	for _, m := range core.Modules.Modules {
		views = append(views, modules.ViewOf(m, states[m.Name]))
	}
	if env.JSON {
		return env.PrintJSON(views)
	}
	if len(views) == 0 {
		fmt.Fprintln(env.Stderr, ":: no modules installed")
		return ExitOK
	}
	rows := [][]string{{"NAME", "VERSION", "STATUS", "SOURCE", "DESCRIPTION"}}
	for _, v := range views {
		version, desc := "-", ""
		if v.Manifest != nil {
			version, desc = v.Manifest.Version, v.Manifest.Description
		}
		rows = append(rows, []string{v.Name, version, v.Status, v.State.Source, desc})
	}
	env.grid(rows, []string{cyan, "", "", dim, ""})
	for _, v := range views {
		for _, p := range v.Problems {
			fmt.Fprintf(env.Stdout, "%s %s: %s\n", env.paint(yellow, "!!"), v.Name, p)
		}
	}
	return ExitOK
}

func moduleCheck(env *Env, args []string) int {
	if len(args) > 0 {
		return env.UsageError([]string{"module", "check"}, "module check takes no arguments")
	}
	// Validation needs no store: check a checkout without creating a database.
	reg := env.LoadConfig(false)
	if reg == nil {
		return ExitError
	}
	mods, err := modules.Discover(reg)
	if err != nil {
		env.Errorf("%v", err)
		return ExitError
	}
	bad := 0
	for _, m := range mods {
		for _, p := range m.Problems {
			env.Errorf("%s: %s", m.Name, p)
		}
		if !m.Valid() {
			bad++
		}
	}
	n := len(mods)
	if bad > 0 {
		env.Errorf("%d of %d modules invalid", bad, n)
		return ExitError
	}
	env.Okf("%d %s valid", n, plural(n, "module", "modules"))
	return ExitOK
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// logInvalid records every invalid module once per server start, so the
// event log says why a module is skipped.
func (env *Env) logInvalid(ctx context.Context, core *Core) {
	for _, m := range core.Modules.Modules {
		if m.Valid() {
			continue
		}
		env.Warnf("module %s skipped: %s", m.Name, strings.Join(m.Problems, "; "))
		env.event(ctx, core, "core.module.invalid", "skipped "+m.Name, map[string]any{"module": m.Name, "problems": m.Problems})
	}
}
