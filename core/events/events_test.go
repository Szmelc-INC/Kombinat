package events

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memBackend struct{ list []Event }

func (m *memBackend) InsertEvent(_ context.Context, e Event) (int64, error) {
	e.ID = int64(len(m.list) + 1)
	m.list = append(m.list, e)
	return e.ID, nil
}

func (m *memBackend) ListEvents(_ context.Context, q Query) ([]Event, error) {
	if q.Limit < len(m.list) {
		return m.list[len(m.list)-q.Limit:], nil
	}
	return m.list, nil
}

func TestAppendValidates(t *testing.T) {
	l := New(&memBackend{})
	ctx := context.Background()
	for _, e := range []Event{
		{Actor: "", Action: "core.x"},
		{Actor: "cli", Action: "nodot"},
		{Actor: "cli", Action: "Core.x"},
		{Actor: "cli", Action: "core."},
		{Actor: "cli", Action: "core.x", Summary: string(make([]byte, 1001))},
	} {
		if _, err := l.Append(ctx, e); !errors.Is(err, ErrInvalid) {
			t.Errorf("Append(%+v) err = %v, want ErrInvalid", e.Action, err)
		}
	}
	e, err := l.Append(ctx, Event{Actor: "cli", Action: "core.module.enable", Summary: "ok"})
	if err != nil || e.ID != 1 || e.TS.IsZero() || e.TS.Location() != time.UTC {
		t.Fatalf("Append: %+v %v", e, err)
	}
}

func TestRedact(t *testing.T) {
	in := map[string]any{
		"port":     1,
		"apiToken": "s3cret",
		"nested":   map[string]any{"Password": "x", "ok": "y"},
		"list":     []any{map[string]any{"api_key": "k"}, "plain"},
	}
	out := Redact(in)
	if out["apiToken"] != Redacted || out["port"] != 1 {
		t.Fatalf("top level: %v", out)
	}
	if n := out["nested"].(map[string]any); n["Password"] != Redacted || n["ok"] != "y" {
		t.Fatalf("nested: %v", n)
	}
	if l := out["list"].([]any); l[0].(map[string]any)["api_key"] != Redacted || l[1] != "plain" {
		t.Fatalf("list: %v", l)
	}
	if in["apiToken"] != "s3cret" {
		t.Fatal("Redact must not modify its input")
	}
}

func TestListClampsLimit(t *testing.T) {
	b := &memBackend{}
	l := New(b)
	for i := 0; i < 3; i++ {
		if _, err := l.Append(context.Background(), Event{Actor: "cli", Action: "core.x"}); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := l.List(context.Background(), Query{Limit: 0})
	if len(got) != 3 {
		t.Fatalf("default limit: %d", len(got))
	}
}
