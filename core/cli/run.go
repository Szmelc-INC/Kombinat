package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/Szmelc-INC/Kombinat/core/modules"
)

func moduleRunCommand() *Command {
	return &Command{
		Name:    "run",
		Args:    "<name> [--] [args...]",
		Summary: "Run a running module's entrypoint once; its output and exit code pass through",
		Flags: []Flag{
			{Name: "--entrypoint", Arg: "KIND", Summary: "Entrypoint to run: cli (default) or api"},
			{Name: "--stdin", Summary: "Forward this process's stdin to the module (max 1 MiB)"},
		},
		Run: func(env *Env, args []string) int {
			path := []string{"module", "run"}
			fs := flag.NewFlagSet("run", flag.ContinueOnError)
			kind := fs.String("entrypoint", "cli", "")
			fwd := fs.Bool("stdin", false, "")
			rest, code, ok := env.parseFlags(path, fs, args)
			if !ok {
				return code
			}
			if len(rest) == 0 {
				return env.UsageError(path, "module run needs a module name")
			}
			name, margs := rest[0], rest[1:]
			if len(margs) > 0 && margs[0] == "--" {
				margs = margs[1:]
			}
			req := modules.RunRequest{Entrypoint: *kind, Args: margs}
			if *fwd {
				if env.Stdin == nil {
					return env.UsageError(path, "--stdin: no stdin available")
				}
				b, err := io.ReadAll(io.LimitReader(env.Stdin, modules.MaxStdin+1))
				if err != nil {
					env.Errorf("read stdin: %v", err)
					return ExitError
				}
				req.Stdin = string(b)
			}
			core := env.OpenCore()
			if core == nil {
				return ExitError
			}
			defer core.Close()
			res, err := core.Runner.Run(context.Background(), name, req, cliActor)
			if err != nil && res.Module == "" {
				env.Errorf("%v", err)
				return ExitError
			}
			if err != nil {
				env.Warnf("run not logged: %v", err)
			}
			if env.JSON {
				env.PrintJSON(res)
			} else {
				fmt.Fprint(env.Stdout, res.Stdout)
				fmt.Fprint(env.Stderr, res.Stderr)
			}
			switch res.Status {
			case modules.StatusOK:
				return ExitOK
			case modules.StatusFailed:
				if res.ExitCode > 0 {
					return res.ExitCode
				}
			}
			env.Errorf("%s %s: %s %s", name, res.Entrypoint, res.Status, res.Error)
			return ExitError
		},
	}
}
