package modules

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Szmelc-INC/Kombinat/core/events"
	"github.com/Szmelc-INC/Kombinat/core/store"
)

// Errors a lifecycle call returns; callers map them to exit codes and HTTP statuses.
var (
	ErrNotFound = errors.New("no such module")
	ErrInvalid  = errors.New("module is invalid")
	ErrConflict = errors.New("lifecycle conflict")
)

// StateStore keeps lifecycle state; package store implements it on SQLite.
type StateStore interface {
	ModuleStates(ctx context.Context) (map[string]store.ModuleState, error)
	SetModuleState(ctx context.Context, st store.ModuleState, e events.Event) (int64, error)
}

// State is a module's effective lifecycle state.
type State struct {
	Enabled bool `json:"enabled"`
	Running bool `json:"running"`
	// Source is "config" while no lifecycle command has run (the module's
	// `enabled` key decides), else "store".
	Source string `json:"source"`
}

// Status names a state for people: invalid, disabled, stopped or running.
func (s State) Status(valid bool) string {
	switch {
	case !valid:
		return "invalid"
	case !s.Enabled:
		return "disabled"
	case !s.Running:
		return "stopped"
	}
	return "running"
}

// View is the public description of a module: what `module ls --json` and
// the API return.
type View struct {
	Name        string                `json:"name"`
	Status      string                `json:"status"`
	State       State                 `json:"state"`
	Manifest    *Manifest             `json:"manifest,omitempty"`
	Entrypoints map[string]Entrypoint `json:"entrypoints,omitempty"`
	Problems    []string              `json:"problems,omitempty"`
}

// ViewOf describes m in state st.
func ViewOf(m *Module, st State) View {
	return View{
		Name:        m.Name,
		Status:      st.Status(m.Valid()),
		State:       st,
		Manifest:    m.Manifest,
		Entrypoints: m.Entrypoints,
		Problems:    m.Problems,
	}
}

// Manager runs the lifecycle of discovered modules.
type Manager struct {
	Modules []*Module
	Store   StateStore
	Log     *events.Log
}

// Find returns the module named name.
func (mg *Manager) Find(name string) (*Module, error) {
	for _, m := range mg.Modules {
		if m.Name == name {
			return m, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
}

// States returns the effective state of every module.
func (mg *Manager) States(ctx context.Context) (map[string]State, error) {
	stored, err := mg.Store.ModuleStates(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]State, len(mg.Modules))
	for _, m := range mg.Modules {
		if st, ok := stored[m.Name]; ok {
			out[m.Name] = State{Enabled: st.Enabled, Running: st.Running, Source: "store"}
			continue
		}
		out[m.Name] = State{Enabled: m.Enabled(), Running: m.Enabled(), Source: "config"}
	}
	return out, nil
}

// State returns the effective state of one module.
func (mg *Manager) State(ctx context.Context, name string) (*Module, State, error) {
	m, err := mg.Find(name)
	if err != nil {
		return nil, State{}, err
	}
	states, err := mg.States(ctx)
	if err != nil {
		return nil, State{}, err
	}
	return m, states[name], nil
}

// Running reports whether name is valid, enabled and running: only then are its routes live.
func (mg *Manager) Running(ctx context.Context, name string) (*Module, bool, error) {
	m, st, err := mg.State(ctx, name)
	if errors.Is(err, ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return m, m.Valid() && st.Enabled && st.Running, nil
}

// Result reports what a lifecycle call did; Changed is false for a no-op.
type Result struct {
	Module  string `json:"module"`
	Status  string `json:"status"`
	Changed bool   `json:"changed"`
	EventID int64  `json:"event_id,omitempty"`
}

// Enable turns a module on and starts it (enabled and running). Every
// dependency must already be enabled.
func (mg *Manager) Enable(ctx context.Context, name, actor string) (Result, error) {
	m, states, err := mg.prepare(ctx, name)
	if err != nil {
		return Result{}, err
	}
	if !m.Valid() {
		return Result{}, fmt.Errorf("%w: %s: %s", ErrInvalid, name, strings.Join(m.Problems, "; "))
	}
	var missing []string
	for _, d := range m.Manifest.Depends {
		if !states[d].Enabled {
			missing = append(missing, d)
		}
	}
	if len(missing) > 0 {
		return Result{}, fmt.Errorf("%w: %s depends on %s; enable %s first", ErrConflict, name, strings.Join(missing, ", "), strings.Join(missing, ", "))
	}
	return mg.set(ctx, m, states[name], State{Enabled: true, Running: true}, actor, "core.module.enable", "enabled "+name)
}

// Disable stops a module and turns it off. No enabled module may depend on it.
func (mg *Manager) Disable(ctx context.Context, name, actor string) (Result, error) {
	m, states, err := mg.prepare(ctx, name)
	if err != nil {
		return Result{}, err
	}
	var dependents []string
	for _, o := range mg.Modules {
		if o.Manifest != nil && contains(o.Manifest.Depends, name) && states[o.Name].Enabled {
			dependents = append(dependents, o.Name)
		}
	}
	if len(dependents) > 0 {
		sort.Strings(dependents)
		return Result{}, fmt.Errorf("%w: %s is needed by %s; disable them first", ErrConflict, name, strings.Join(dependents, ", "))
	}
	return mg.set(ctx, m, states[name], State{}, actor, "core.module.disable", "disabled "+name)
}

// Start runs an enabled module that was stopped.
func (mg *Manager) Start(ctx context.Context, name, actor string) (Result, error) {
	m, states, err := mg.prepare(ctx, name)
	if err != nil {
		return Result{}, err
	}
	if !m.Valid() {
		return Result{}, fmt.Errorf("%w: %s: %s", ErrInvalid, name, strings.Join(m.Problems, "; "))
	}
	if !states[name].Enabled {
		return Result{}, fmt.Errorf("%w: %s is disabled; enable it instead", ErrConflict, name)
	}
	return mg.set(ctx, m, states[name], State{Enabled: true, Running: true}, actor, "core.module.start", "started "+name)
}

// Stop halts an enabled module without disabling it; its routes answer 404
// until it starts again or the server restarts.
func (mg *Manager) Stop(ctx context.Context, name, actor string) (Result, error) {
	m, states, err := mg.prepare(ctx, name)
	if err != nil {
		return Result{}, err
	}
	return mg.set(ctx, m, states[name], State{Enabled: states[name].Enabled, Running: false}, actor, "core.module.stop", "stopped "+name)
}

// Boot starts every enabled module that is stopped; the server calls it once
// at startup, so a restart brings back what is enabled.
func (mg *Manager) Boot(ctx context.Context, actor string) ([]Result, error) {
	states, err := mg.States(ctx)
	if err != nil {
		return nil, err
	}
	var out []Result
	for _, m := range mg.Modules {
		st := states[m.Name]
		if m.Valid() && st.Enabled && !st.Running {
			r, err := mg.set(ctx, m, st, State{Enabled: true, Running: true}, actor, "core.module.start", "started "+m.Name+" at boot")
			if err != nil {
				return out, err
			}
			out = append(out, r)
		}
	}
	return out, nil
}

func (mg *Manager) prepare(ctx context.Context, name string) (*Module, map[string]State, error) {
	m, err := mg.Find(name)
	if err != nil {
		return nil, nil, err
	}
	states, err := mg.States(ctx)
	if err != nil {
		return nil, nil, err
	}
	return m, states, nil
}

// set writes the new state with its event in one transaction; an unchanged
// state writes nothing and logs nothing.
func (mg *Manager) set(ctx context.Context, m *Module, cur, next State, actor, action, summary string) (Result, error) {
	if cur.Enabled == next.Enabled && cur.Running == next.Running {
		return Result{Module: m.Name, Status: cur.Status(m.Valid()), Changed: false}, nil
	}
	payload := map[string]any{"enabled": next.Enabled, "running": next.Running}
	if m.Manifest != nil {
		payload["version"] = m.Manifest.Version
	}
	e, err := mg.Log.Prepare(events.Event{Actor: actor, Action: action, Subject: m.Name, Summary: summary, Payload: payload})
	if err != nil {
		return Result{}, err
	}
	id, err := mg.Store.SetModuleState(ctx, store.ModuleState{Name: m.Name, Enabled: next.Enabled, Running: next.Running}, e)
	if err != nil {
		return Result{}, err
	}
	next.Source = "store"
	return Result{Module: m.Name, Status: next.Status(m.Valid()), Changed: true, EventID: id}, nil
}
