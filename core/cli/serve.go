package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Szmelc-INC/Kombinat/core/api"
	"github.com/Szmelc-INC/Kombinat/core/config"
	"github.com/Szmelc-INC/Kombinat/core/events"
)

// RuntimeFile records the live API address, relative to the root, for clients.
const RuntimeFile = "var/run/api.json"

// SecretsDir holds the API tokens, relative to the root; it is gitignored.
const SecretsDir = "secrets"

// Runtime is the content of RuntimeFile.
type Runtime struct {
	PID     int       `json:"pid"`
	Host    string    `json:"host"`
	Port    int       `json:"port"`
	Started time.Time `json:"started"`
}

func serveCommand() *Command {
	return &Command{
		Name:    "serve",
		Summary: "Serve the HTTP JSON API on 127.0.0.1 until interrupted",
		Run: func(env *Env, args []string) int {
			if len(args) > 0 {
				return env.UsageError([]string{"serve"}, "serve takes no arguments")
			}
			core := env.OpenCore()
			if core == nil {
				return ExitError
			}
			defer core.Close()
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return env.serve(ctx, core)
		},
	}
}

func (env *Env) serve(ctx context.Context, core *Core) int {
	want := int(config.Int(core.Config.Core, "api.port"))
	ln, fellBack, err := api.Listen(want)
	if err != nil {
		env.Errorf("listen on %s:%d: %v", api.Host, want, err)
		return ExitError
	}
	port := api.Port(ln)
	if fellBack {
		env.Warnf("port %d is busy; serving on %d instead (recorded in %s)", want, port, RuntimeFile)
		env.event(ctx, core, "core.api.port_fallback", fmt.Sprintf("port %d busy, serving on %d", want, port),
			map[string]any{"configured": want, "port": port})
	}
	tokens, created, err := api.LoadTokens(filepath.Join(core.Config.Root, SecretsDir))
	if err != nil {
		ln.Close()
		env.Errorf("%v", err)
		return ExitError
	}
	if created {
		env.Okf("created %s/%s (mode 0600); API clients send it as a bearer token", SecretsDir, api.TokenFile)
	}
	srv := &http.Server{
		Handler:           (&api.Server{Config: core.Config, Log: core.Log, Modules: core.Modules, Tokens: tokens}).Handler(port),
		ReadHeaderTimeout: 10 * time.Second,
	}
	env.event(ctx, core, "core.api.start", fmt.Sprintf("API on %s:%d", api.Host, port),
		map[string]any{"port": port, "actors": tokens.Actors()})
	env.logInvalid(ctx, core)
	if _, err := core.Modules.Boot(ctx, cliActor); err != nil {
		env.Warnf("boot modules: %v", err)
	}
	// The runtime file is written last: once it exists, the server is ready.
	rt := filepath.Join(core.Config.Root, RuntimeFile)
	if err := writeRuntime(rt, Runtime{PID: os.Getpid(), Host: api.Host, Port: port, Started: time.Now().UTC()}); err != nil {
		ln.Close()
		env.Errorf("record runtime: %v", err)
		return ExitError
	}
	defer os.Remove(rt)
	fmt.Fprintf(env.Stderr, ":: serving http://%s:%d (Ctrl+C stops)\n", api.Host, port)

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		env.Errorf("serve: %v", err)
		return ExitError
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
		env.Warnf("shutdown: %v", err)
	}
	env.event(shutdown, core, "core.api.stop", "API stopped", nil)
	return ExitOK
}

// event appends a core event and only warns on failure: losing the server
// over a log write would be worse than the missing entry, which is reported.
func (env *Env) event(ctx context.Context, core *Core, action, summary string, payload map[string]any) {
	if _, err := core.Log.Append(ctx, events.Event{Actor: cliActor, Action: action, Summary: summary, Payload: payload}); err != nil {
		env.Warnf("event %s not logged: %v", action, err)
	}
}

func writeRuntime(path string, rt Runtime) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rt, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
