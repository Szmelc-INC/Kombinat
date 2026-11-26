package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Szmelc-INC/Kombinat/core/events"
)

// InsertEvent appends e and returns its id (events.Backend).
func (s *Store) InsertEvent(ctx context.Context, e events.Event) (int64, error) {
	var payload sql.NullString
	if e.Payload != nil {
		b, err := json.Marshal(e.Payload)
		if err != nil {
			return 0, fmt.Errorf("encode payload: %w", err)
		}
		payload = sql.NullString{String: string(b), Valid: true}
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO events (ts, actor, action, subject, summary, payload) VALUES (?, ?, ?, ?, ?, ?)`,
		e.TS.UTC().Format(time.RFC3339Nano), e.Actor, e.Action, e.Subject, e.Summary, payload)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListEvents returns events per q, oldest first (events.Backend).
func (s *Store) ListEvents(ctx context.Context, q events.Query) ([]events.Event, error) {
	const cols = `id, ts, actor, action, subject, summary, payload`
	var (
		rows *sql.Rows
		err  error
	)
	if q.After > 0 {
		rows, err = s.db.QueryContext(ctx,
			`SELECT `+cols+` FROM events WHERE id > ? ORDER BY id LIMIT ?`, q.After, q.Limit)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT `+cols+` FROM (SELECT `+cols+` FROM events ORDER BY id DESC LIMIT ?) ORDER BY id`, q.Limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []events.Event{}
	for rows.Next() {
		var (
			e       events.Event
			ts      string
			payload sql.NullString
		)
		if err := rows.Scan(&e.ID, &ts, &e.Actor, &e.Action, &e.Subject, &e.Summary, &payload); err != nil {
			return nil, err
		}
		if e.TS, err = time.Parse(time.RFC3339Nano, ts); err != nil {
			return nil, fmt.Errorf("event %d: bad ts %q: %w", e.ID, ts, err)
		}
		if payload.Valid {
			if err := json.Unmarshal([]byte(payload.String), &e.Payload); err != nil {
				return nil, fmt.Errorf("event %d: bad payload: %w", e.ID, err)
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
