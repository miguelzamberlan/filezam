// Package index keeps a SQLite table of file names so search can answer without
// walking the disk. A periodic full scan is the source of truth; the server nudges it
// after its own mutations so results stay fresh between scans.
package index

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zamberlan/filezam/internal/store"
	"github.com/zamberlan/filezam/internal/vfs"
)

const batchSize = 500

type Indexer struct {
	db       *store.DB
	base     *vfs.Root
	log      *slog.Logger
	Interval time.Duration

	scanMu  sync.Mutex // one full scan at a time
	running atomic.Bool
	ready   atomic.Bool
}

func New(db *store.DB, base *vfs.Root, log *slog.Logger, interval time.Duration) *Indexer {
	ix := &Indexer{db: db, base: base, log: log, Interval: interval}
	if st, err := db.GetIndexState(context.Background()); err == nil && st.LastFullAt != nil {
		ix.ready.Store(true) // um scan anterior sobreviveu ao reinício
	}
	return ix
}

// Ready reports whether at least one full scan has completed (now or in a previous run).
func (ix *Indexer) Ready() bool { return ix.ready.Load() }

// Running reports whether a full scan is in progress.
func (ix *Indexer) Running() bool { return ix.running.Load() }

type Status struct {
	Ready      bool   `json:"ready"`
	Running    bool   `json:"running"`
	Entries    int64  `json:"entries"`
	LastFullAt *int64 `json:"lastFullAt"`
	Interval   int64  `json:"interval"` // seconds
}

func (ix *Indexer) Status(ctx context.Context) Status {
	st := Status{Ready: ix.Ready(), Running: ix.Running(), Interval: int64(ix.Interval.Seconds())}
	if s, err := ix.db.GetIndexState(ctx); err == nil {
		st.LastFullAt = s.LastFullAt
	}
	st.Entries, _ = ix.db.IndexCount(ctx)
	return st
}

// FullScan rebuilds the whole index. Returns false if another scan was already running.
func (ix *Indexer) FullScan(ctx context.Context) (bool, error) {
	if !ix.scanMu.TryLock() {
		return false, nil
	}
	defer ix.scanMu.Unlock()
	ix.running.Store(true)
	defer ix.running.Store(false)
	start := time.Now()
	st, err := ix.db.GetIndexState(ctx)
	if err != nil {
		return true, err
	}
	gen := st.Gen + 1
	var batch []store.IndexRow
	var n int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := ix.db.UpsertIndexBatch(ctx, batch, gen); err != nil {
			return err
		}
		n += int64(len(batch))
		batch = batch[:0]
		return nil
	}
	err = ix.base.WalkEntries(ctx, "", func(path string, e vfs.Entry) error {
		batch = append(batch, store.IndexRow{Path: path, Name: e.Name, Type: e.Type, Size: e.Size, Mtime: e.Mtime})
		if len(batch) >= batchSize {
			return flush()
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	if err != nil {
		ix.log.Warn("index full scan aborted", "err", err, "entries", n)
		return true, err
	}
	if err := ix.db.DeleteIndexNotGen(ctx, gen); err != nil {
		return true, err
	}
	if err := ix.db.SetIndexState(ctx, time.Now().Unix(), n, gen); err != nil {
		return true, err
	}
	ix.ready.Store(true)
	ix.log.Info("index full scan", "entries", n, "ms", time.Since(start).Milliseconds())
	return true, nil
}

func (ix *Indexer) gen(ctx context.Context) int64 {
	if st, err := ix.db.GetIndexState(ctx); err == nil {
		return st.Gen
	}
	return 0
}

// ReindexTree drops and re-walks a subtree (a new folder, a copy/move destination).
func (ix *Indexer) ReindexTree(ctx context.Context, path string) error {
	if err := ix.db.DeleteIndexTree(ctx, path); err != nil {
		return err
	}
	gen := ix.gen(ctx)
	var batch []store.IndexRow
	err := ix.base.WalkEntries(ctx, path, func(p string, e vfs.Entry) error {
		batch = append(batch, store.IndexRow{Path: p, Name: e.Name, Type: e.Type, Size: e.Size, Mtime: e.Mtime})
		if len(batch) >= batchSize {
			if err := ix.db.UpsertIndexBatch(ctx, batch, gen); err != nil {
				return err
			}
			batch = batch[:0]
		}
		return nil
	})
	if err != nil {
		return err
	}
	return ix.db.UpsertIndexBatch(ctx, batch, gen)
}

// Touch refreshes single entries (upload finished, folder created); missing ones are removed.
func (ix *Indexer) Touch(ctx context.Context, paths ...string) error {
	gen := ix.gen(ctx)
	var rows []store.IndexRow
	for _, p := range paths {
		if p == "" {
			continue
		}
		e, err := ix.base.Stat(p)
		if err != nil {
			_ = ix.db.DeleteIndexTree(ctx, p)
			continue
		}
		rows = append(rows, store.IndexRow{Path: p, Name: e.Name, Type: e.Type, Size: e.Size, Mtime: e.Mtime})
	}
	return ix.db.UpsertIndexBatch(ctx, rows, gen)
}

// RemoveTree forgets a path and everything below it.
func (ix *Indexer) RemoveTree(ctx context.Context, path string) error {
	return ix.db.DeleteIndexTree(ctx, path)
}

// Search answers from the index: rows under prefix whose name contains q. more=true when
// there were more rows than limit.
func (ix *Indexer) Search(ctx context.Context, prefix, q string, limit int) (rows []store.IndexRow, more bool, err error) {
	rows, err = ix.db.SearchIndex(ctx, prefix, q, limit+1)
	if err != nil {
		return nil, false, err
	}
	if len(rows) > limit {
		return rows[:limit], true, nil
	}
	return rows, false, nil
}
