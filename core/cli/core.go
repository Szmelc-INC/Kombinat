package cli

import (
	"flag"
	"io"
	"time"

	"github.com/Szmelc-INC/Kombinat/core/config"
	"github.com/Szmelc-INC/Kombinat/core/events"
	"github.com/Szmelc-INC/Kombinat/core/modules"
	"github.com/Szmelc-INC/Kombinat/core/store"
)

// cliActor is the event actor of commands run from the CLI.
const cliActor = "cli"

// Core is the opened spine of one Kombinat root: config, store, event log and
// module loader. The CLI and the API call the same Core code; there is no
// second path.
type Core struct {
	Config  *config.Registry
	Store   *store.Store
	Log     *events.Log
	Modules *modules.Manager
	Runner  *modules.Runner
}

// Close releases the store.
func (c *Core) Close() { _ = c.Store.Close() }

// OpenCore loads the config, opens the store and discovers modules; on
// failure it prints the reason and returns nil. Module problems do not fail
// it: those modules are listed as invalid and skipped.
func (env *Env) OpenCore() *Core {
	reg := env.LoadConfig(false)
	if reg == nil {
		return nil
	}
	st, err := store.Open(reg.Path(config.String(reg.Core, "store.path")))
	if err != nil {
		env.Errorf("%v", err)
		return nil
	}
	mods, err := modules.Discover(reg)
	if err != nil {
		st.Close()
		env.Errorf("%v", err)
		return nil
	}
	log := events.New(st)
	mgr := &modules.Manager{Modules: mods, Store: st, Log: log}
	runner := &modules.Runner{
		Manager:  mgr,
		CacheDir: reg.Path(CacheDir),
		Timeout:  time.Duration(config.Int(reg.Core, "runtime.timeout_seconds")) * time.Second,
		Path:     env.Getenv("PATH"),
	}
	return &Core{Config: reg, Store: st, Log: log, Modules: mgr, Runner: runner}
}

// CacheDir holds built Go entrypoints, relative to the root.
const CacheDir = "var/cache/modules"

// parseFlags parses args into fs; a bad flag prints the command usage and
// returns ok=false with ExitUsage.
func (env *Env) parseFlags(path []string, fs *flag.FlagSet, args []string) (rest []string, code int, ok bool) {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return nil, env.UsageError(path, "%v", err), false
	}
	return fs.Args(), ExitOK, true
}
