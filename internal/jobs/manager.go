// Package jobs runs long file operations in the background with progress.
package jobs

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/zamberlan/filezam/internal/auth"
)

// State of a job.
const (
	StateRunning   = "running"
	StateDone      = "done"
	StateFailed    = "failed"
	StateCancelled = "cancelled"
)

// View is the JSON snapshot of a job.
type View struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	State      string   `json:"state"`
	Done       int      `json:"done"`
	Total      int      `json:"total"`
	BytesDone  int64    `json:"bytesDone"`
	BytesTotal int64    `json:"bytesTotal"`
	Current    string   `json:"current,omitempty"`
	Error      string   `json:"error,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
	StartedAt  int64    `json:"startedAt"`
	FinishedAt int64    `json:"finishedAt,omitempty"`
	Dirs       []string `json:"dirs,omitempty"` // affected directories for client cache invalidation
}

// Job is a running or finished background operation.
type Job struct {
	id     string
	userID int64
	typ    string
	cancel context.CancelFunc
	done   chan struct{}

	mu   sync.Mutex
	view View
}

// Add increments progress counters.
func (j *Job) Add(files int, bytes int64) {
	j.mu.Lock()
	j.view.Done += files
	j.view.BytesDone += bytes
	j.mu.Unlock()
}

// SetTotals sets the expected totals.
func (j *Job) SetTotals(files int, bytes int64) {
	j.mu.Lock()
	j.view.Total, j.view.BytesTotal = files, bytes
	j.mu.Unlock()
}

// SetCurrent records the item being processed.
func (j *Job) SetCurrent(p string) {
	j.mu.Lock()
	j.view.Current = p
	j.mu.Unlock()
}

// Warn appends a non-fatal warning (bounded).
func (j *Job) Warn(msg string) {
	j.mu.Lock()
	if len(j.view.Warnings) < 100 {
		j.view.Warnings = append(j.view.Warnings, msg)
	}
	j.mu.Unlock()
}

// AddDir records an affected directory.
func (j *Job) AddDir(d string) {
	j.mu.Lock()
	for _, x := range j.view.Dirs {
		if x == d {
			j.mu.Unlock()
			return
		}
	}
	j.view.Dirs = append(j.view.Dirs, d)
	j.mu.Unlock()
}

// Snapshot returns a copy of the view.
func (j *Job) Snapshot() View {
	j.mu.Lock()
	defer j.mu.Unlock()
	v := j.view
	v.Warnings = append([]string(nil), j.view.Warnings...)
	v.Dirs = append([]string(nil), j.view.Dirs...)
	return v
}

// ID returns the job id.
func (j *Job) ID() string { return j.id }

// Wait blocks up to d for completion; returns true if finished.
func (j *Job) Wait(d time.Duration) bool {
	select {
	case <-j.done:
		return true
	case <-time.After(d):
		return false
	}
}

// Manager holds jobs in memory.
type Manager struct {
	mu   sync.Mutex
	jobs map[string]*Job
	ctx  context.Context
	wg   sync.WaitGroup
}

// New creates a manager whose jobs are cancelled when ctx ends.
func New(ctx context.Context) *Manager {
	return &Manager{jobs: map[string]*Job{}, ctx: ctx}
}

// Start launches fn in a goroutine and returns the job.
func (m *Manager) Start(userID int64, typ string, dirs []string, fn func(ctx context.Context, j *Job) error) *Job {
	id, _ := auth.NewID(8)
	ctx, cancel := context.WithCancel(m.ctx)
	j := &Job{id: id, userID: userID, typ: typ, cancel: cancel, done: make(chan struct{})}
	j.view = View{ID: id, Type: typ, State: StateRunning, StartedAt: time.Now().Unix(), Dirs: dirs}
	m.mu.Lock()
	m.jobs[id] = j
	m.prune()
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		err := fn(ctx, j)
		j.mu.Lock()
		j.view.FinishedAt = time.Now().Unix()
		j.view.Current = ""
		switch {
		case err == nil:
			j.view.State = StateDone
		case errors.Is(err, context.Canceled):
			j.view.State = StateCancelled
		default:
			j.view.State = StateFailed
			j.view.Error = err.Error()
		}
		j.mu.Unlock()
		cancel()
		close(j.done)
	}()
	return j
}

// Get returns a job owned by userID (userID<=0: any).
func (m *Manager) Get(id string, userID int64) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok || (userID > 0 && j.userID != userID) {
		return nil, false
	}
	return j, true
}

// List returns the user's jobs, newest first.
func (m *Manager) List(userID int64) []View {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []View
	for _, j := range m.jobs {
		if j.userID == userID {
			out = append(out, j.Snapshot())
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].StartedAt > out[b].StartedAt })
	if len(out) > 50 {
		out = out[:50]
	}
	if out == nil {
		out = []View{}
	}
	return out
}

// Cancel stops a job.
func (m *Manager) Cancel(id string, userID int64) bool {
	j, ok := m.Get(id, userID)
	if !ok {
		return false
	}
	j.cancel()
	return true
}

// prune drops finished jobs older than an hour (caller holds mu).
func (m *Manager) prune() {
	cutoff := time.Now().Add(-time.Hour).Unix()
	for id, j := range m.jobs {
		v := j.Snapshot()
		if v.State != StateRunning && v.FinishedAt < cutoff {
			delete(m.jobs, id)
		}
	}
}

// Wait blocks until all jobs finish or d elapses.
func (m *Manager) Wait(d time.Duration) {
	ch := make(chan struct{})
	go func() { m.wg.Wait(); close(ch) }()
	select {
	case <-ch:
	case <-time.After(d):
	}
}
