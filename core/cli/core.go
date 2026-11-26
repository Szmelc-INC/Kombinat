package cli

import (
	"flag"
	"io"

	"github.com/Szmelc-INC/Kombinat/core/config"
	"github.com/Szmelc-INC/Kombinat/core/events"
	"github.com/Szmelc-INC/Kombinat/core/store"
)

// cliActor is the event actor of commands run from the CLI.
const cliActor = "cli"

// Core is the opened spine of one Kombinat root: config, store and event log.
// The CLI and the API call the same Core code; there is no second path.
type Core struct {
	Config *config.Registry
	Store  *store.Store
	Log    *events.Log
}

// Close releases the store.
func (c *Core) Close() { _ = c.Store.Close() }

// OpenCore loads the config and opens the store; on failure it prints the
// reason and returns nil.
func (env *Env) OpenCore() *Core {
	reg := env.LoadConfig()
	if reg == nil {
		return nil
	}
	st, err := store.Open(reg.Path(config.String(reg.Core, "store.path")))
	if err != nil {
		env.Errorf("%v", err)
		return nil
	}
	return &Core{Config: reg, Store: st, Log: events.New(st)}
}

// parseFlags parses args into fs; a bad flag prints the command usage and
// returns ok=false with ExitUsage.
func (env *Env) parseFlags(path []string, fs *flag.FlagSet, args []string) (rest []string, code int, ok bool) {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return nil, env.UsageError(path, "%v", err), false
	}
	return fs.Args(), ExitOK, true
}
