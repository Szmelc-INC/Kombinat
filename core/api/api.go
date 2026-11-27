// Package api is the core HTTP JSON API. It binds to 127.0.0.1 only, answers
// in one envelope ({"ok":true,"data":...} or {"ok":false,"error":"..."}), and
// rejects foreign Host and Origin headers so other sites cannot reach it
// through the browser.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"strconv"
	"syscall"

	"github.com/Szmelc-INC/Kombinat/core/events"
)

// Host is the only address the API ever binds to.
const Host = "127.0.0.1"

// MaxBody caps a request body.
const MaxBody = 1 << 20

// Server serves the API for one Kombinat root.
type Server struct {
	Log *events.Log
}

// Handler returns the API handler for a server bound to port.
func (s *Server) Handler(port int) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events", s.listEvents)
	mux.HandleFunc("POST /api/events", s.addEvent)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		Error(w, http.StatusNotFound, "no such route: "+r.Method+" "+r.URL.Path)
	})
	return guard(port, mux)
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
		Actor:   "api",
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
