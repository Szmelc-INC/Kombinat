// Package cli is the kombinat command line: one registry of subcommands,
// global flags, colored help and shell completions generated from the registry.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// version is the core version (SemVer); a release build may override it with
// -ldflags "-X github.com/Szmelc-INC/Kombinat/core/cli.version=X.Y.Z".
var version = "0.1.0-dev"

// Exit codes per the CLI conventions.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// IO is everything a run needs from the process: streams, environment and
// whether stdout is an interactive terminal.
type IO struct {
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	TTY    bool
}

// Env is the per-run state handed to every command.
type Env struct {
	IO
	Color bool
	JSON  bool
	root  *Command
}

// Command is one entry of the registry. A command either runs (Run) or
// dispatches to its subcommands (Sub).
type Command struct {
	Name     string
	Args     string // argument synopsis shown in help, e.g. "[command]"
	Summary  string
	Flags    []Flag
	Complete func(env *Env) []string // values offered for the first positional argument
	Run      func(env *Env, args []string) int
	Sub      []*Command
}

// Flag documents one command flag for help and completion.
type Flag struct {
	Name    string // with dashes, e.g. "--tail"
	Arg     string // value synopsis, empty for a boolean flag
	Summary string
}

var globalFlags = []Flag{
	{Name: "--json", Summary: "Print data as JSON"},
	{Name: "--no-color", Summary: "Plain output without colors (also NO_COLOR)"},
	{Name: "--help", Summary: "Show help (also -h)"},
}

// Root returns the full command registry.
func Root() *Command {
	root := &Command{
		Name:    "kombinat",
		Summary: "Kombinat MkII core: one tiny spine, everything else a module",
	}
	root.Sub = []*Command{
		completionCommand(),
		configCommand(),
		helpCommand(),
		logCommand(),
		serveCommand(),
		versionCommand(),
	}
	return root
}

// Run parses global flags, dispatches to a command and returns the exit code.
func Run(args []string, pio IO) int {
	env := &Env{IO: pio, root: Root()}
	rest, help := env.parseGlobal(args)
	if len(rest) == 0 {
		if help {
			env.printHelp(env.Stdout, env.root, nil)
			return ExitOK
		}
		env.printHelp(env.Stderr, env.root, nil)
		return ExitUsage
	}
	return env.dispatch(env.root, nil, rest, help)
}

// parseGlobal removes the global flags from args, wherever they appear
// before a "--" separator, and applies them.
func (env *Env) parseGlobal(args []string) (rest []string, help bool) {
	noColor := env.Getenv("NO_COLOR") != ""
	for i, a := range args {
		switch a {
		case "--":
			rest = append(rest, args[i:]...)
			env.Color = env.TTY && !noColor
			return rest, help
		case "--json":
			env.JSON = true
		case "--no-color":
			noColor = true
		case "-h", "--help":
			help = true
		default:
			rest = append(rest, a)
		}
	}
	env.Color = env.TTY && !noColor
	return rest, help
}

func (env *Env) dispatch(cmd *Command, path []string, args []string, help bool) int {
	name := args[0]
	sub := find(cmd.Sub, name)
	if sub == nil {
		env.Errorf("unknown command: %s", strings.Join(append(path, name), " "))
		env.printHelp(env.Stderr, cmd, path)
		return ExitUsage
	}
	path = append(path, sub.Name)
	args = args[1:]
	if len(sub.Sub) > 0 {
		if len(args) == 0 {
			if help {
				env.printHelp(env.Stdout, sub, path)
				return ExitOK
			}
			env.printHelp(env.Stderr, sub, path)
			return ExitUsage
		}
		return env.dispatch(sub, path, args, help)
	}
	if help {
		env.printHelp(env.Stdout, sub, path)
		return ExitOK
	}
	return sub.Run(env, args)
}

func find(cmds []*Command, name string) *Command {
	for _, c := range cmds {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// UsageError reports a usage problem for the command at path and returns ExitUsage.
func (env *Env) UsageError(path []string, format string, a ...any) int {
	env.Errorf(format, a...)
	cmd := env.root
	for _, p := range path {
		if c := find(cmd.Sub, p); c != nil {
			cmd = c
		}
	}
	env.printHelp(env.Stderr, cmd, path)
	return ExitUsage
}

// Errorf prints an error message to stderr with the error prefix.
func (env *Env) Errorf(format string, a ...any) {
	fmt.Fprintf(env.Stderr, "%s %s\n", env.paint(red, "✗"), fmt.Sprintf(format, a...))
}

// Warnf prints a warning to stderr with the warning prefix.
func (env *Env) Warnf(format string, a ...any) {
	fmt.Fprintf(env.Stderr, "%s %s\n", env.paint(yellow, "!!"), fmt.Sprintf(format, a...))
}

// Okf prints a success message to stderr with the ok prefix.
func (env *Env) Okf(format string, a ...any) {
	fmt.Fprintf(env.Stderr, "%s %s\n", env.paint(green, "✓"), fmt.Sprintf(format, a...))
}

// PrintJSON writes v to stdout as indented JSON.
func (env *Env) PrintJSON(v any) int {
	enc := json.NewEncoder(env.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		env.Errorf("encode json: %v", err)
		return ExitError
	}
	return ExitOK
}

func versionCommand() *Command {
	return &Command{
		Name:    "version",
		Summary: "Print the core version",
		Run: func(env *Env, args []string) int {
			if len(args) > 0 {
				return env.UsageError([]string{"version"}, "version takes no arguments")
			}
			if env.JSON {
				return env.PrintJSON(map[string]string{"name": "kombinat", "version": version})
			}
			fmt.Fprintf(env.Stdout, "kombinat %s\n", version)
			return ExitOK
		},
	}
}

func helpCommand() *Command {
	return &Command{
		Name:    "help",
		Args:    "[command...]",
		Summary: "Show help for kombinat or one command",
		Complete: func(env *Env) []string {
			return names(env.root.Sub)
		},
		Run: func(env *Env, args []string) int {
			cmd, path := env.root, []string(nil)
			for _, a := range args {
				sub := find(cmd.Sub, a)
				if sub == nil {
					return env.UsageError(nil, "unknown command: %s", strings.Join(append(path, a), " "))
				}
				cmd, path = sub, append(path, sub.Name)
			}
			env.printHelp(env.Stdout, cmd, path)
			return ExitOK
		},
	}
}

func names(cmds []*Command) []string {
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, c.Name)
	}
	return out
}
