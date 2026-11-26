package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Szmelc-INC/Kombinat/core/events"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "sub dir", "k.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenMigratesAndUsesWAL(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if v, err := s.Version(ctx); err != nil || v != len(migrations) {
		t.Fatalf("version %d err %v", v, err)
	}
	var mode string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode %q err %v", mode, err)
	}
}

func TestReopenKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.InsertEvent(ctx, events.Event{TS: time.Now(), Actor: "cli", Action: "core.x"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	list, err := s.ListEvents(ctx, events.Query{Limit: 10})
	if err != nil || len(list) != 1 {
		t.Fatalf("after reopen: %v %v", list, err)
	}
}

func TestRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("err = %v", err)
	}
}

func TestEventsRoundTripAndOrder(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	for i, a := range []string{"core.a", "core.b", "core.c"} {
		e := events.Event{TS: time.Now(), Actor: "cli", Action: a, Subject: "s", Summary: "n"}
		if i == 1 {
			e.Payload = map[string]any{"k": "v"}
		}
		if _, err := s.InsertEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	tail, err := s.ListEvents(ctx, events.Query{Limit: 2})
	if err != nil || len(tail) != 2 || tail[0].Action != "core.b" || tail[1].Action != "core.c" {
		t.Fatalf("tail: %+v %v", tail, err)
	}
	if tail[0].Payload["k"] != "v" || tail[1].Payload != nil {
		t.Fatalf("payloads: %v %v", tail[0].Payload, tail[1].Payload)
	}
	after, err := s.ListEvents(ctx, events.Query{After: 1, Limit: 10})
	if err != nil || len(after) != 2 || after[0].ID != 2 {
		t.Fatalf("after: %+v %v", after, err)
	}
}

func TestEventsAreAppendOnly(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if _, err := s.InsertEvent(ctx, events.Event{TS: time.Now(), Actor: "cli", Action: "core.x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE events SET actor = 'evil'"); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("update: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM events"); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("delete: %v", err)
	}
}
