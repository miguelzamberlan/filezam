// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package store

import (
	"context"
	"database/sql"
)

// JobRecord is a persisted background operation (progress snapshot or final state).
type JobRecord struct {
	ID         string `json:"id"`
	UserID     int64  `json:"-"`
	Type       string `json:"type"`
	Label      string `json:"label"`
	State      string `json:"state"`
	Done       int    `json:"done"`
	Total      int    `json:"total"`
	BytesDone  int64  `json:"bytesDone"`
	BytesTotal int64  `json:"bytesTotal"`
	Error      string `json:"error,omitempty"`
	Warnings   int    `json:"warnings"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt *int64 `json:"finishedAt"`
}

func (db *DB) UpsertJob(ctx context.Context, j *JobRecord) error {
	_, err := db.w.ExecContext(ctx, `INSERT INTO jobs(id, user_id, type, label, state, done, total, bytes_done, bytes_total, error, warnings, started_at, finished_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET state=excluded.state, done=excluded.done, total=excluded.total, bytes_done=excluded.bytes_done,
		bytes_total=excluded.bytes_total, error=excluded.error, warnings=excluded.warnings, finished_at=excluded.finished_at`,
		j.ID, j.UserID, j.Type, j.Label, j.State, j.Done, j.Total, j.BytesDone, j.BytesTotal, j.Error, j.Warnings, j.StartedAt, nullInt(j.FinishedAt))
	return err
}

// ListJobs returns the user's most recent records (userID<=0: everyone's).
func (db *DB) ListJobs(ctx context.Context, userID int64, limit int) ([]JobRecord, error) {
	q := `SELECT id, user_id, type, label, state, done, total, bytes_done, bytes_total, error, warnings, started_at, finished_at FROM jobs`
	var args []any
	if userID > 0 {
		q += ` WHERE user_id=?`
		args = append(args, userID)
	}
	q += ` ORDER BY started_at DESC, id LIMIT ?`
	args = append(args, limit)
	rows, err := db.r.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JobRecord{}
	for rows.Next() {
		var j JobRecord
		var fin sql.NullInt64
		if err := rows.Scan(&j.ID, &j.UserID, &j.Type, &j.Label, &j.State, &j.Done, &j.Total, &j.BytesDone, &j.BytesTotal, &j.Error, &j.Warnings, &j.StartedAt, &fin); err != nil {
			return nil, err
		}
		if fin.Valid {
			j.FinishedAt = &fin.Int64
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// MarkInterruptedJobs flags jobs left "running" by a previous process as failed.
func (db *DB) MarkInterruptedJobs(ctx context.Context) (int64, error) {
	res, err := db.w.ExecContext(ctx, `UPDATE jobs SET state='failed', error='interrupted by server restart', finished_at=? WHERE state='running'`, db.now())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PruneJobs deletes finished records older than the cutoff.
func (db *DB) PruneJobs(ctx context.Context, before int64) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM jobs WHERE state<>'running' AND started_at<?`, before)
	return err
}

// CountJobs returns how many records exist per state.
func (db *DB) CountJobs(ctx context.Context) (map[string]int64, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT state, COUNT(*) FROM jobs GROUP BY state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var st string
		var n int64
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}

// JobCleanup is something an interrupted job has to undo.
type JobCleanup struct {
	JobID string
	Path  string // base-relativo
	Mode  string // "temps" | "tree"
	// Do job, para a mensagem que fica no histórico.
	Type  string
	Done  int
	Total int
}

// AddJobCleanup records what to undo if the process dies while the job runs.
func (db *DB) AddJobCleanup(ctx context.Context, jobID, path, mode string) error {
	_, err := db.w.ExecContext(ctx, `INSERT OR IGNORE INTO job_cleanup(job_id, path, mode) VALUES(?,?,?)`, jobID, path, mode)
	return err
}

// DeleteJobCleanup forgets the job's cleanup rows (the job ended on its own).
func (db *DB) DeleteJobCleanup(ctx context.Context, jobID string) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM job_cleanup WHERE job_id=?`, jobID)
	return err
}

// DeleteJobCleanupPath forgets one row after it was dealt with.
func (db *DB) DeleteJobCleanupPath(ctx context.Context, jobID, path string) error {
	_, err := db.w.ExecContext(ctx, `DELETE FROM job_cleanup WHERE job_id=? AND path=?`, jobID, path)
	return err
}

// ListOrphanCleanup returns the rows of jobs that are no longer running: quem terminou apagou as
// suas, então o que sobra é de um processo que caiu.
func (db *DB) ListOrphanCleanup(ctx context.Context) ([]JobCleanup, error) {
	rows, err := db.r.QueryContext(ctx, `SELECT c.job_id, c.path, c.mode, j.type, j.done, j.total FROM job_cleanup c JOIN jobs j ON j.id=c.job_id WHERE j.state<>'running' ORDER BY c.job_id, c.path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JobCleanup{}
	for rows.Next() {
		var c JobCleanup
		if err := rows.Scan(&c.JobID, &c.Path, &c.Mode, &c.Type, &c.Done, &c.Total); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetJobError replaces the error text of a finished job.
func (db *DB) SetJobError(ctx context.Context, id, msg string) error {
	_, err := db.w.ExecContext(ctx, `UPDATE jobs SET error=? WHERE id=?`, msg, id)
	return err
}
