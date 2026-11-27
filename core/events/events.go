// Package events is the append-only event log: every state-changing action
// in the core and in modules lands here as one Event. Payloads are redacted
// before they are stored, so secrets never reach the log.
package events

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Event is one entry of the log.
type Event struct {
	ID      int64          `json:"id"`
	TS      time.Time      `json:"ts"`
	Actor   string         `json:"actor"`
	Action  string         `json:"action"`
	Subject string         `json:"subject,omitempty"`
	Summary string         `json:"summary"`
	Payload map[string]any `json:"payload,omitempty"`
}

// Query selects events. After > 0 returns events with a larger id, oldest
// first, at most Limit; otherwise the last Limit events, oldest first.
type Query struct {
	After int64
	Limit int
}

// MaxLimit caps one query.
const MaxLimit = 1000

// Backend stores events; package store implements it on SQLite.
type Backend interface {
	InsertEvent(ctx context.Context, e Event) (int64, error)
	ListEvents(ctx context.Context, q Query) ([]Event, error)
}

// Log is the event log over a Backend.
type Log struct {
	backend Backend
	now     func() time.Time
}

// New returns a Log over b.
func New(b Backend) *Log { return &Log{backend: b, now: time.Now} }

// actionRe: a namespace ("core" or a module name) and at least one dotted part.
var actionRe = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z0-9_-]+)+$`)

// ValidAction reports whether action is namespaced, e.g. "core.module.enable".
func ValidAction(action string) bool { return actionRe.MatchString(action) }

// ErrInvalid marks an event rejected before it reached the backend.
var ErrInvalid = errors.New("invalid event")

// Prepare validates and redacts e and stamps its time, without storing it;
// callers that write state and event in one transaction use it.
func (l *Log) Prepare(e Event) (Event, error) {
	e.Actor = strings.TrimSpace(e.Actor)
	if e.Actor == "" {
		return Event{}, fmt.Errorf("%w: actor is required", ErrInvalid)
	}
	if !ValidAction(e.Action) {
		return Event{}, fmt.Errorf("%w: action %q must look like namespace.verb", ErrInvalid, e.Action)
	}
	if len(e.Summary) > 1000 {
		return Event{}, fmt.Errorf("%w: summary longer than 1000 bytes", ErrInvalid)
	}
	e.TS = l.now().UTC()
	e.Payload = Redact(e.Payload)
	return e, nil
}

// Append validates, redacts and stores e, and returns it with its id and time.
func (l *Log) Append(ctx context.Context, e Event) (Event, error) {
	e, err := l.Prepare(e)
	if err != nil {
		return Event{}, err
	}
	id, err := l.backend.InsertEvent(ctx, e)
	if err != nil {
		return Event{}, fmt.Errorf("append event: %w", err)
	}
	e.ID = id
	return e, nil
}

// List returns events per q, with Limit clamped to 1..MaxLimit (default 50).
func (l *Log) List(ctx context.Context, q Query) ([]Event, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Limit > MaxLimit {
		q.Limit = MaxLimit
	}
	return l.backend.ListEvents(ctx, q)
}

// Redacted replaces every secret-shaped value.
const Redacted = "[redacted]"

var secretKeyRe = regexp.MustCompile(`(?i)(token|secret|password|passwd|api[_-]?key|authorization|cookie|private[_-]?key|credential)`)

// Redact returns a copy of payload with the values of secret-shaped keys
// replaced, recursively through nested objects and arrays.
func Redact(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	out := make(map[string]any, len(payload))
	for k, v := range payload {
		if secretKeyRe.MatchString(k) {
			out[k] = Redacted
			continue
		}
		out[k] = redactValue(v)
	}
	return out
}

func redactValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return Redact(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = redactValue(e)
		}
		return out
	}
	return v
}
