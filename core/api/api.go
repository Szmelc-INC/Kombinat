// Package api is the core HTTP JSON API. It binds to 127.0.0.1 only, answers
// in one envelope ({"ok":true,"data":...} or {"ok":false,"error":"..."}), and
// rejects foreign Host and Origin headers so other sites cannot reach it
// through the browser.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"syscall"

	"github.com/Szmelc-INC/Kombinat/core/config"
	"github.com/Szmelc-INC/Kombinat/core/events"
	"github.com/Szmelc-INC/Kombinat/core/modules"
)

// Host is the only address the API ever binds to.
const Host = "127.0.0.1"

// MaxBody caps a request body.
const MaxBody = 1 << 20

// Server serves the API for one Kombinat root.
type Server struct {
	Config  *config.Registry
	Log     *events.Log
	Modules *modules.Manager
	Runner  *modules.Runner
	Tokens  Tokens
}

// Routes lists every route the core serves, for docs and tests.
var Routes = []string{
	"GET /api/config",
	"GET /api/events",
	"POST /api/events",
	"GET /api/modules",
	"GET /api/modules/{name}",
	"POST /api/modules/{name}/{action}",
	"POST /api/modules/{name}/run",
}

// Handler returns the API handler for a server bound to port. Every request
// passes the browser guard, then bearer-token auth, then routing.
func (s *Server) Handler(port int) http.Handler {
	mux := http.NewServeMux()
	handlers := map[string]http.HandlerFunc{
		"GET /api/config":                   s.getConfig,
		"GET /api/events":                   s.listEvents,
		"POST /api/events":                  s.addEvent,
		"GET /api/modules":                  s.listModules,
		"GET /api/modules/{name}":           s.getModule,
		"POST /api/modules/{name}/{action}": s.moduleAction,
		"POST /api/modules/{name}/run":      s.runModule,
	}
	for _, route := range Routes {
		mux.HandleFunc(route, handlers[route])
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		Error(w, http.StatusNotFound, "no such route: "+r.Method+" "+r.URL.Path)
	})
	return guard(port, authenticate(s.Tokens, mux))
}

// guard enforces the loopback-only browser rules before any route runs.
func guard(port int, next http.Handler) http.Handler {
	p := strconv.Itoa(port)
	hosts := map[string]bool{Host + ":" + p: true, "localhost:" + p: true}
	origins := map[string]bool{"http://" + Host + ":" + p: true, "http://localhost:" + p: true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hosts[r.Host] {
			Error(w, http.StatusForbidden, "foreign Host header")
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !origins[o] {
			Error(w, http.StatusForbidden, "foreign Origin header")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mt != "application/json" {
				Error(w, http.StatusUnsupportedMediaType, "state-changing requests need Content-Type: application/json")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
		}
		next.ServeHTTP(w, r)
	})
}

type envelope struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// JSON writes data in the success envelope.
func JSON(w http.ResponseWriter, status int, data any) {
	write(w, status, envelope{OK: true, Data: data})
}

// Error writes msg in the error envelope.
func Error(w http.ResponseWriter, status int, msg string) {
	write(w, status, envelope{OK: false, Error: msg})
}

func write(w http.ResponseWriter, status int, v envelope) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// decode reads one strict JSON object into v: unknown fields are an error.
func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("bad JSON body: %w", err)
	}
	if dec.More() {
		return errors.New("bad JSON body: trailing data")
	}
	return nil
}

func queryInt(r *http.Request, name string, def int64) (int64, error) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return n, nil
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	after, err := queryInt(r, "after", 0)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	limit, err := queryInt(r, "limit", 50)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := s.Log.List(r.Context(), events.Query{After: after, Limit: int(min(limit, events.MaxLimit))})
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, list)
}

type addEventRequest struct {
	Action  string         `json:"action"`
	Subject string         `json:"subject"`
	Summary string         `json:"summary"`
	Payload map[string]any `json:"payload"`
}

func (s *Server) addEvent(w http.ResponseWriter, r *http.Request) {
	var req addEventRequest
	if err := decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	e, err := s.Log.Append(r.Context(), events.Event{
		Actor:   Actor(r),
		Action:  req.Action,
		Subject: req.Subject,
		Summary: req.Summary,
		Payload: req.Payload,
	})
	if errors.Is(err, events.ErrInvalid) {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, e)
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	mods := make(map[string]any, len(s.Config.Modules))
	for name, t := range s.Config.Modules {
		mods[name] = events.Redact(t)
	}
	JSON(w, http.StatusOK, map[string]any{"core": events.Redact(s.Config.Core), "modules": mods})
}

// listModules answers every discovered module, valid or not, with its state.
func (s *Server) listModules(w http.ResponseWriter, r *http.Request) {
	states, err := s.Modules.States(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	views := make([]modules.View, 0, len(s.Modules.Modules))
	for _, m := range s.Modules.Modules {
		views = append(views, modules.ViewOf(m, states[m.Name]))
	}
	JSON(w, http.StatusOK, views)
}

// moduleAction runs one lifecycle call: enable, disable, start or stop.
func (s *Server) moduleAction(w http.ResponseWriter, r *http.Request) {
	calls := map[string]func(*modules.Manager, context.Context, string, string) (modules.Result, error){
		"enable":  (*modules.Manager).Enable,
		"disable": (*modules.Manager).Disable,
		"start":   (*modules.Manager).Start,
		"stop":    (*modules.Manager).Stop,
	}
	call, ok := calls[r.PathValue("action")]
	if !ok {
		Error(w, http.StatusNotFound, "no such module action: "+r.PathValue("action")+" (want enable, disable, start or stop)")
		return
	}
	var empty struct{}
	if err := decode(r, &empty); err != nil && !errors.Is(err, io.EOF) {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := call(s.Modules, r.Context(), r.PathValue("name"), Actor(r))
	switch {
	case errors.Is(err, modules.ErrNotFound):
		Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, modules.ErrInvalid):
		Error(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, modules.ErrConflict):
		Error(w, http.StatusConflict, err.Error())
	case err != nil:
		Error(w, http.StatusInternalServerError, err.Error())
	default:
		JSON(w, http.StatusOK, res)
	}
}

// runModule runs one entrypoint of a running module and answers its result;
// the run itself is logged as core.module.run, whatever its outcome.
func (s *Server) runModule(w http.ResponseWriter, r *http.Request) {
	var req modules.RunRequest
	if err := decode(r, &req); err != nil && !errors.Is(err, io.EOF) {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.Runner.Run(r.Context(), r.PathValue("name"), req, Actor(r))
	switch {
	case errors.Is(err, modules.ErrNotFound):
		Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, modules.ErrBadRequest):
		Error(w, http.StatusBadRequest, err.Error())
	case err != nil:
		Error(w, http.StatusInternalServerError, err.Error())
	default:
		JSON(w, http.StatusOK, res)
	}
}

// getModule answers a running module's description; a module that is
// unknown, invalid, disabled or stopped answers 404 like any absent route.
func (s *Server) getModule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	m, running, err := s.Modules.Running(r.Context(), name)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !running {
		Error(w, http.StatusNotFound, "module not running: "+name)
		return
	}
	_, st, err := s.Modules.State(r.Context(), name)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, modules.ViewOf(m, st))
}

// Listen binds 127.0.0.1:port. When the port is busy it binds a free port
// instead and reports fellBack, so the caller can record and warn once.
// Port 0 always picks a free port.
func Listen(port int) (ln net.Listener, fellBack bool, err error) {
	ln, err = net.Listen("tcp", net.JoinHostPort(Host, strconv.Itoa(port)))
	if err == nil || port == 0 || !errors.Is(err, syscall.EADDRINUSE) {
		return ln, false, err
	}
	ln, err = net.Listen("tcp", net.JoinHostPort(Host, "0"))
	return ln, err == nil, err
}

// Port returns the TCP port ln is bound to.
func Port(ln net.Listener) int { return ln.Addr().(*net.TCPAddr).Port }
