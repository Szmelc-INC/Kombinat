package cli

import (
	"fmt"
	"io"
	"strings"
)

// ANSI styles; applied only when Env.Color is true.
const (
	bold   = "1"
	red    = "31"
	green  = "32"
	yellow = "33"
	cyan   = "36"
	dim    = "2"
)

func (env *Env) paint(style, s string) string {
	if !env.Color {
		return s
	}
	return "\x1b[" + style + "m" + s + "\x1b[0m"
}

// printHelp writes the help for cmd, reached through path, to w.
func (env *Env) printHelp(w io.Writer, cmd *Command, path []string) {
	full := strings.TrimSpace("kombinat " + strings.Join(path, " "))
	fmt.Fprintf(w, "%s — %s\n\n", env.paint(bold, full), cmd.Summary)

	fmt.Fprintln(w, env.paint(bold, "Usage"))
	switch {
	case len(cmd.Sub) > 0 && len(path) == 0:
		fmt.Fprintf(w, "  %s [global flags] <command> [args]\n", full)
	case len(cmd.Sub) > 0:
		fmt.Fprintf(w, "  %s <command> [args]\n", full)
	default:
		line := full
		if len(cmd.Flags) > 0 {
			line += " [flags]"
		}
		if cmd.Args != "" {
			line += " " + cmd.Args
		}
		fmt.Fprintf(w, "  %s\n", line)
	}

	if len(cmd.Sub) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, env.paint(bold, "Commands"))
		rows := make([][2]string, 0, len(cmd.Sub))
		for _, s := range cmd.Sub {
			left := s.Name
			if s.Args != "" {
				left += " " + s.Args
			}
			rows = append(rows, [2]string{left, s.Summary})
		}
		env.table(w, rows, cyan)
	}
	if len(cmd.Flags) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, env.paint(bold, "Flags"))
		env.table(w, flagRows(cmd.Flags), cyan)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, env.paint(bold, "Global flags"))
	env.table(w, flagRows(globalFlags), cyan)
	if len(cmd.Sub) > 0 {
		fmt.Fprintf(w, "\n%s\n", env.paint(dim, "Run '"+full+" <command> --help' for one command."))
	}
}

func flagRows(flags []Flag) [][2]string {
	rows := make([][2]string, 0, len(flags))
	for _, f := range flags {
		left := f.Name
		if f.Arg != "" {
			left += " " + f.Arg
		}
		rows = append(rows, [2]string{left, f.Summary})
	}
	return rows
}

// table prints two aligned columns; padding is computed on the plain text so
// colors never break the alignment.
func (env *Env) table(w io.Writer, rows [][2]string, style string) {
	width := 0
	for _, r := range rows {
		if n := len([]rune(r[0])); n > width {
			width = n
		}
	}
	for _, r := range rows {
		pad := strings.Repeat(" ", width-len([]rune(r[0])))
		fmt.Fprintf(w, "  %s%s  %s\n", env.paint(style, r[0]), pad, r[1])
	}
}
