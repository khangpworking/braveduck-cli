package store

import (
	"context"
	"errors"
	"time"
)

// ErrWatchUnsupported is returned by Watch on an in-memory store, which no
// other connection or process can write to.
var ErrWatchUnsupported = errors.New("store: watch needs a file database")

// Watch reports when the database changes — from this process or any other
// (the CLI, `braveduck mcp`, a second board). It polls SQLite's PRAGMA
// data_version on a dedicated connection that never writes; that value moves
// whenever any other connection commits. The channel gets one value per
// detected change (coalesced if the reader is slow) and is closed when ctx
// ends or the connection fails.
func (s *Store) Watch(ctx context.Context, every time.Duration) (<-chan struct{}, error) {
	if s.memory {
		return nil, ErrWatchUnsupported
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	version := func() (int64, error) {
		var v int64
		err := conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&v)
		return v, err
	}
	last, err := version()
	if err != nil {
		conn.Close()
		return nil, err
	}

	ch := make(chan struct{}, 1)
	go func() {
		defer close(ch)
		defer conn.Close()
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			v, err := version()
			if err != nil {
				return
			}
			if v != last {
				last = v
				select {
				case ch <- struct{}{}:
				default: // a change is already pending; coalesce
				}
			}
		}
	}()
	return ch, nil
}
