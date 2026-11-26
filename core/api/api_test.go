package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Szmelc-INC/Kombinat/core/config"
	"github.com/Szmelc-INC/Kombinat/core/events"
	"github.com/Szmelc-INC/Kombinat/core/modules"
	"github.com/Szmelc-INC/Kombinat/core/store"
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

// memStates is an in-memory lifecycle store.
type memStates struct {
	states map[string]store.ModuleState
	log    *memBackend
}

func (m *memStates) ModuleStates(context.Context) (map[string]store.ModuleState, error) {
	return m.states, nil
}

func (m *memStates) SetModuleState(ctx context.Context, st store.ModuleState, e events.Event) (int64, error) {
	m.states[st.Name] = st
	return m.log.InsertEvent(ctx, e)
}

const (
	port       = 2137
	token      = "0123456789abcdef0123456789abcdef"
	agentToken = "fedcba9876543210fedcba9876543210"
)

// do sends one request with the default token unless hdr sets Authorization
// ("" removes it), and decodes the envelope.
func do(t *testing.T, h http.Handler, method, target, body string, hdr map[string]string) (int, envelope) {
	t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Host = "127.0.0.1:2137"
	r.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		switch {
		case k == "Host":
			r.Host = v
		case v == "":
			r.Header.Del(k)
		default:
			r.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s %s: response is not the JSON envelope: %q", method, target, w.Body.String())
	}
	return w.Code, env
}

type fixture struct {
	h   http.Handler
	log *memBackend
}

func newFixture() fixture {
	log := &memBackend{}
	off := &modules.Module{Name: "off", Manifest: &modules.Manifest{Name: "off", Version: "0.1.0"}, Config: map[string]any{"enabled": false}}
	on := &modules.Module{Name: "on", Manifest: &modules.Manifest{Name: "on", Version: "0.1.0"}, Config: map[string]any{"enabled": true}}
	bad := &modules.Module{Name: "bad", Problems: []string{"module.json: invalid JSON"}}
	srv := &Server{
		Config: &config.Registry{
			Core:    config.Table{"api": map[string]any{"port": int64(port)}},
			Modules: map[string]config.Table{"on": {"enabled": true, "api_token": "s3cret"}},
		},
		Log:     events.New(log),
		Modules: &modules.Manager{Modules: []*modules.Module{bad, off, on}, Store: &memStates{states: map[string]store.ModuleState{}, log: log}, Log: events.New(log)},
		Tokens:  Tokens{token: "api", agentToken: "agent-1"},
	}
	srv.Runner = &modules.Runner{Manager: srv.Modules}
	return fixture{srv.Handler(port), log}
}

func TestAddThenListEvent(t *testing.T) {
	f := newFixture()
	code, env := do(t, f.h, "POST", "/api/events", `{"action":"core.note","summary":"hi","payload":{"token":"x"}}`, nil)
	if code != http.StatusCreated || !env.OK {
		t.Fatalf("add: %d %+v", code, env)
	}
	code, env = do(t, f.h, "GET", "/api/events?limit=1", "", nil)
	list, _ := json.Marshal(env.Data)
	if code != http.StatusOK || !strings.Contains(string(list), `"action":"core.note"`) || !strings.Contains(string(list), `"actor":"api"`) {
		t.Fatalf("list: %d %s", code, list)
	}
	if strings.Contains(string(list), `"x"`) {
		t.Fatalf("payload secret leaked: %s", list)
	}
}

func TestAuth(t *testing.T) {
	f := newFixture()
	for name, hdr := range map[string]map[string]string{
		"no token":     {"Authorization": ""},
		"wrong token":  {"Authorization": "Bearer " + strings.Repeat("x", 32)},
		"basic scheme": {"Authorization": "Basic " + token},
	} {
		for _, route := range Routes {
			method, path, _ := strings.Cut(route, " ")
			path = strings.NewReplacer("{name}", "on", "{action}", "stop").Replace(path)
			code, env := do(t, f.h, method, path, "{}", hdr)
			if code != http.StatusUnauthorized || env.OK {
				t.Errorf("%s: %s answered %d, want 401", name, route, code)
			}
		}
	}
	if len(f.log.list) != 0 {
		t.Fatalf("an unauthenticated call changed state: %v", f.log.list)
	}
	code, _ := do(t, f.h, "POST", "/api/events", `{"action":"core.note"}`, map[string]string{"Authorization": "Bearer " + agentToken})
	if code != http.StatusCreated || f.log.list[0].Actor != "agent-1" {
		t.Fatalf("per-actor token: %d %+v", code, f.log.list)
	}
}

func TestRoutesAnswerTheRightStatus(t *testing.T) {
	f := newFixture()
	cases := []struct {
		method, target, body string
		want                 int
	}{
		{"GET", "/api/config", "", http.StatusOK},
		{"GET", "/api/events", "", http.StatusOK},
		{"POST", "/api/events", `{"action":"core.note"}`, http.StatusCreated},
		{"GET", "/api/modules", "", http.StatusOK},
		{"GET", "/api/modules/on", "", http.StatusOK},
		{"GET", "/api/modules/off", "", http.StatusNotFound},
		{"GET", "/api/modules/ghost", "", http.StatusNotFound},
		{"POST", "/api/modules/off/start", "{}", http.StatusConflict},
		{"POST", "/api/modules/bad/enable", "{}", http.StatusUnprocessableEntity},
		{"POST", "/api/modules/ghost/enable", "{}", http.StatusNotFound},
		{"POST", "/api/modules/on/explode", "{}", http.StatusNotFound},
		{"POST", "/api/modules/on/run", `{"entrypoint":"cli"}`, http.StatusBadRequest},
		{"POST", "/api/modules/on/run", `{"command":"rm"}`, http.StatusBadRequest},
		{"POST", "/api/modules/off/run", "{}", http.StatusNotFound},
		{"POST", "/api/modules/off/enable", "", http.StatusOK},
		{"GET", "/api/modules/off", "", http.StatusOK},
		{"POST", "/api/modules/off/disable", `{}`, http.StatusOK},
		{"POST", "/api/modules/off/disable", `{"force":true}`, http.StatusBadRequest},
		{"GET", "/api/nope", "", http.StatusNotFound},
	}
	for _, c := range cases {
		hdr := map[string]string{}
		if c.method == "POST" {
			hdr["Content-Type"] = "application/json"
		}
		code, env := do(t, f.h, c.method, c.target, c.body, hdr)
		if code != c.want || env.OK != (code < 300) {
			t.Errorf("%s %s: %d %+v, want %d", c.method, c.target, code, env, c.want)
		}
	}
	if got := f.log.list[len(f.log.list)-1]; got.Action != "core.module.disable" || got.Actor != "api" || got.Subject != "off" {
		t.Fatalf("lifecycle event: %+v", got)
	}
}

func TestConfigRouteRedacts(t *testing.T) {
	_, env := do(t, newFixture().h, "GET", "/api/config", "", nil)
	b, _ := json.Marshal(env.Data)
	if strings.Contains(string(b), "s3cret") || !strings.Contains(string(b), `"port":2137`) {
		t.Fatalf("config: %s", b)
	}
}

func TestModulesListShowsEveryModule(t *testing.T) {
	_, env := do(t, newFixture().h, "GET", "/api/modules", "", nil)
	b, _ := json.Marshal(env.Data)
	var views []modules.View
	if err := json.Unmarshal(b, &views); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range views {
		got = append(got, v.Name+"="+v.Status)
	}
	if strings.Join(got, ",") != "bad=invalid,off=disabled,on=running" {
		t.Fatalf("modules list: %v", got)
	}
}

func TestRejections(t *testing.T) {
	f := newFixture()
	cases := []struct {
		name, method, target, body string
		hdr                        map[string]string
		want                       int
	}{
		{"foreign host", "GET", "/api/events", "", map[string]string{"Host": "evil.example:2137"}, http.StatusForbidden},
		{"foreign origin", "GET", "/api/events", "", map[string]string{"Origin": "http://evil.example"}, http.StatusForbidden},
		{"no json content type", "POST", "/api/events", "x", map[string]string{"Content-Type": "text/plain"}, http.StatusUnsupportedMediaType},
		{"unknown field", "POST", "/api/events", `{"action":"core.x","actor":"root"}`, nil, http.StatusBadRequest},
		{"bad action", "POST", "/api/events", `{"action":"x"}`, nil, http.StatusBadRequest},
		{"trailing data", "POST", "/api/events", `{"action":"core.x"} {}`, nil, http.StatusBadRequest},
		{"bad limit", "GET", "/api/events?limit=-1", "", nil, http.StatusBadRequest},
	}
	for _, c := range cases {
		code, env := do(t, f.h, c.method, c.target, c.body, c.hdr)
		if code != c.want || env.OK {
			t.Errorf("%s: %d %+v, want %d", c.name, code, env, c.want)
		}
	}
}

func TestLocalhostOriginAllowed(t *testing.T) {
	code, env := do(t, newFixture().h, "GET", "/api/events", "", map[string]string{"Origin": "http://localhost:2137"})
	if code != http.StatusOK || !env.OK {
		t.Fatalf("%d %+v", code, env)
	}
}

func TestLoadTokens(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secrets")
	tokens, created, err := LoadTokens(dir)
	if err != nil || !created || len(tokens) != 1 || tokens.Actors()[0] != "api" {
		t.Fatalf("first load: %v %v %v", tokens.Actors(), created, err)
	}
	info, err := os.Stat(filepath.Join(dir, TokenFile))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("token mode %v, want 0600", info.Mode().Perm())
	}
	if err := os.WriteFile(filepath.Join(dir, "token-agent-1"), []byte(agentToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if tokens, created, err = LoadTokens(dir); err != nil || created || strings.Join(tokens.Actors(), ",") != "agent-1,api" {
		t.Fatalf("second load: %v %v %v", tokens.Actors(), created, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "token-weak"), []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadTokens(dir); err == nil || !strings.Contains(err.Error(), "token-weak") {
		t.Fatalf("weak token: %v", err)
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
