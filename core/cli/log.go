package cli

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/Szmelc-INC/Kombinat/core/events"
)

func logCommand() *Command {
	return &Command{
		Name:    "log",
		Summary: "Show the event log, oldest first",
		Flags: []Flag{
			{Name: "--tail", Arg: "N", Summary: "Show the last N events (default 20, max 1000)"},
			{Name: "--after", Arg: "ID", Summary: "Show events with an id above ID instead"},
		},
		Run: func(env *Env, args []string) int {
			path := []string{"log"}
			fs := flag.NewFlagSet("log", flag.ContinueOnError)
			tail := fs.Int("tail", 20, "")
			after := fs.Int64("after", 0, "")
			rest, code, ok := env.parseFlags(path, fs, args)
			if !ok {
				return code
			}
			if len(rest) > 0 {
				return env.UsageError(path, "log takes no arguments")
			}
			if *tail < 1 || *after < 0 {
				return env.UsageError(path, "--tail must be at least 1 and --after not negative")
			}
			core := env.OpenCore()
			if core == nil {
				return ExitError
			}
			defer core.Close()
			list, err := core.Log.List(context.Background(), events.Query{After: *after, Limit: *tail})
			if err != nil {
				env.Errorf("%v", err)
				return ExitError
			}
			if env.JSON {
				return env.PrintJSON(list)
			}
			env.printEvents(list)
			return ExitOK
		},
	}
}

func (env *Env) printEvents(list []events.Event) {
	rows := make([][]string, 0, len(list))
	for _, e := range list {
		summary := e.Summary
		if e.Subject != "" {
			summary = "[" + e.Subject + "] " + summary
		}
		rows = append(rows, []string{
			"#" + strconv.FormatInt(e.ID, 10),
			e.TS.Local().Format("2006-01-02 15:04:05"),
			e.Actor,
			e.Action,
			summary,
		})
	}
	env.grid(rows, []string{dim, dim, "", cyan, ""})
}

// grid prints aligned columns; the last column is never padded.
func (env *Env) grid(rows [][]string, styles []string) {
	if len(rows) == 0 {
		return
	}
	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, c := range r {
			if n := len([]rune(c)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	for _, r := range rows {
		var b strings.Builder
		for i, c := range r {
			cell := c
			if styles[i] != "" {
				cell = env.paint(styles[i], c)
			}
			b.WriteString(cell)
			if i < len(r)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-len([]rune(c))+2))
			}
		}
		fmt.Fprintln(env.Stdout, b.String())
	}
}
