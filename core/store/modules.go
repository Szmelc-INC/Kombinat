package store

import (
	"context"
	"fmt"
	"time"

	"github.com/Szmelc-INC/Kombinat/core/events"
)

// ModuleState is the stored lifecycle state of one module.
type ModuleState struct {
	Name      string
	Enabled   bool
	Running   bool
	UpdatedAt time.Time
}

// ModuleStates returns every stored module state by name.
func (s *Store) ModuleStates(ctx context.Context) (map[string]ModuleState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, enabled, running, updated_at FROM module_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ModuleState{}
	for rows.Next() {
		var (
			st ModuleState
			ts string
		)
		if err := rows.Scan(&st.Name, &st.Enabled, &st.Running, &ts); err != nil {
			return nil, err
		}
		if st.UpdatedAt, err = time.Parse(time.RFC3339Nano, ts); err != nil {
			return nil, fmt.Errorf("module %s: bad updated_at %q: %w", st.Name, ts, err)
		}
		out[st.Name] = st
	}
	return out, rows.Err()
}

// SetModuleState writes st and appends the prepared event e in one
// transaction, so the log never disagrees with the state. It returns the event id.
func (s *Store) SetModuleState(ctx context.Context, st ModuleState, e events.Event) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // a committed tx makes this a no-op
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO module_state (name, enabled, running, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET enabled = excluded.enabled, running = excluded.running, updated_at = excluded.updated_at`,
		st.Name, st.Enabled, st.Running, e.TS.UTC().Format(time.RFC3339Nano)); err != nil {
		return 0, fmt.Errorf("set module state: %w", err)
	}
	id, err := insertEvent(ctx, tx, e)
	if err != nil {
		return 0, fmt.Errorf("append event: %w", err)
	}
	return id, tx.Commit()
}
