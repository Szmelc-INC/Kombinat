package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Szmelc-INC/Kombinat/core/events"
)

type memBackend struct{ list []events.Event }

func (m *memBackend) InsertEvent(_ context.Context, e events.Event) (int64, error) {
	e.ID = int64(len(m.list) + 1)
	m.list = append(m.list, e)
	return e.ID, nil
}

func (m *memBackend) ListEvents(_ context.Context, q events.Query) ([]events.Event, error) {
	out := []events.Event{}
	for _, e := range m.list {
		if e.ID > q.After {
			out = append(out, e)
		}
	}
	if q.After == 0 && len(out) > q.Limit {
		out = out[len(out)-q.Limit:]
	}
	return out, nil
}

const port = 2137

func do(t *testing.T, h http.Handler, method, target, body string, hdr map[string]string) (int, envelope) {
	t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Host = "127.0.0.1:2137"
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s %s: response is not the JSON envelope: %q", method, target, w.Body.String())
	}
	return w.Code, env
}

func handler() http.Handler {
	return (&Server{Log: events.New(&memBackend{})}).Handler(port)
}

func TestAddThenListEvent(t *testing.T) {
	h := handler()
	code, env := do(t, h, "POST", "/api/events", `{"action":"core.note","summary":"hi","payload":{"token":"x"}}`, nil)
	if code != http.StatusCreated || !env.OK {
		t.Fatalf("add: %d %+v", code, env)
	}
	code, env = do(t, h, "GET", "/api/events?limit=1", "", nil)
	list, _ := json.Marshal(env.Data)
	if code != http.StatusOK || !strings.Contains(string(list), `"action":"core.note"`) || !strings.Contains(string(list), `"actor":"api"`) {
		t.Fatalf("list: %d %s", code, list)
	}
	if strings.Contains(string(list), `"x"`) {
		t.Fatalf("payload secret leaked: %s", list)
	}
}

func TestRejections(t *testing.T) {
	h := handler()
	cases := []struct {
		name, method, target, body string
		hdr                        map[string]string
		want                       int
	}{
		{"foreign host", "GET", "/api/events", "", map[string]string{"Host": "evil.example:2137"}, http.StatusForbidden},
		{"foreign origin", "GET", "/api/events", "", map[string]string{"Origin": "http://evil.example"}, http.StatusForbidden},
		{"no json content type", "POST", "/api/events", "", map[string]string{"Content-Type": "text/plain"}, http.StatusUnsupportedMediaType},
		{"unknown field", "POST", "/api/events", `{"action":"core.x","actor":"root"}`, nil, http.StatusBadRequest},
		{"bad action", "POST", "/api/events", `{"action":"x"}`, nil, http.StatusBadRequest},
		{"trailing data", "POST", "/api/events", `{"action":"core.x"} {}`, nil, http.StatusBadRequest},
		{"bad limit", "GET", "/api/events?limit=-1", "", nil, http.StatusBadRequest},
		{"unknown route", "GET", "/api/nope", "", nil, http.StatusNotFound},
	}
	for _, c := range cases {
		hdr := c.hdr
		r := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))
		r.Host = "127.0.0.1:2137"
		if c.body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		for k, v := range hdr {
			if k == "Host" {
				r.Host = v
				continue
			}
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want || !strings.Contains(w.Body.String(), `"ok":false`) {
			t.Errorf("%s: %d %s, want %d", c.name, w.Code, w.Body.String(), c.want)
		}
	}
}

func TestLocalhostOriginAllowed(t *testing.T) {
	code, env := do(t, handler(), "GET", "/api/events", "", map[string]string{"Origin": "http://localhost:2137"})
	if code != http.StatusOK || !env.OK {
		t.Fatalf("%d %+v", code, env)
	}
}

func TestListenFallsBackWhenBusy(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	taken := Port(busy)
	ln, fellBack, err := Listen(taken)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if !fellBack || Port(ln) == taken {
		t.Fatalf("fellBack %v port %d (taken %d)", fellBack, Port(ln), taken)
	}
	if host := ln.Addr().(*net.TCPAddr).IP.String(); host != Host {
		t.Fatalf("bound to %s, want %s", host, Host)
	}
}
