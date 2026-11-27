package cli

import (
	"os"

	"github.com/Szmelc-INC/Kombinat/core/config"
)

// RootDir returns the Kombinat root: $KOMBINAT_ROOT, else the working directory.
func (env *Env) RootDir() (string, error) {
	if r := env.Getenv("KOMBINAT_ROOT"); r != "" {
		return r, nil
	}
	return os.Getwd()
}

// LoadConfig loads the config registry. A core config problem is printed and
// returns nil. Module problems do too when strict; otherwise they stay in the
// registry and the loader marks those modules invalid.
func (env *Env) LoadConfig(strict bool) *config.Registry {
	root, err := env.RootDir()
	if err != nil {
		env.Errorf("find root: %v", err)
		return nil
	}
	reg, err := config.Load(root)
	if err != nil {
		env.printConfigErrors(err)
		return nil
	}
	if strict && len(reg.Problems) > 0 {
		env.printConfigErrors(reg.Problems)
		return nil
	}
	return reg
}

func (env *Env) printConfigErrors(err error) {
	if es, ok := err.(config.Errors); ok {
		for _, e := range es {
			env.Errorf("%v", e)
		}
		return
	}
	env.Errorf("%v", err)
}

func configCommand() *Command {
	return &Command{
		Name:    "config",
		Summary: "Inspect the merged configuration",
		Sub: []*Command{{
			Name:    "show",
			Args:    "[core|module]",
			Summary: "Print the merged config as JSON: core plus every module, or one of them",
			Run: func(env *Env, args []string) int {
				if len(args) > 1 {
					return env.UsageError([]string{"config", "show"}, "config show takes at most one name")
				}
				reg := env.LoadConfig(true)
				if reg == nil {
					return ExitError
				}
				if len(args) == 0 {
					return env.PrintJSON(map[string]any{"core": reg.Core, "modules": reg.Modules})
				}
				if args[0] == config.CoreName {
					return env.PrintJSON(reg.Core)
				}
				m, ok := reg.Modules[args[0]]
				if !ok {
					env.Errorf("no module config: %s", args[0])
					return ExitError
				}
				return env.PrintJSON(m)
			},
		}},
	}
}
