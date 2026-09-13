// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/miguelzamberlan/filezam/internal/jobs"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// Recuperação de jobs interrompidos. Um reinício mata as operações em andamento (decisão: não há
// retomada), mas não pode deixar lixo com cara de arquivo legítimo. Cada job grava em
// job_cleanup, antes de tocar o disco, o que precisaria desfazer; ao terminar por conta própria
// apaga as linhas. O que sobra é de um processo que caiu, e a manutenção consome.

const (
	// cleanupTemps apaga só os temporários de cópia com a marca do job: o que já terminou fica.
	cleanupTemps = "temps"
	// cleanupTree apaga o caminho inteiro. Só para o que o job criou do zero e que não vale nada
	// pela metade: a pasta de uma extração, o .zip temporário de uma compactação.
	cleanupTree = "tree"
)

// guardJob records what to undo at base-relative path if the process dies while j runs. Uma
// falha aqui não impede a operação: no pior caso sobra o que sobraria sem o registro.
func (s *Server) guardJob(j *jobs.Job, path, mode string) {
	if err := s.db.AddJobCleanup(context.Background(), j.ID(), path, mode); err != nil {
		s.log.Warn("job cleanup record", "job", j.ID(), "path", path, "err", err)
	}
}

// unguardJob forgets the job's records once it ended on its own.
func (s *Server) unguardJob(j *jobs.Job) {
	if err := s.db.DeleteJobCleanup(context.Background(), j.ID()); err != nil {
		s.log.Warn("job cleanup forget", "job", j.ID(), "err", err)
	}
}

// warnPartialCopy tells, on the job itself, that a multi-file copy stopped halfway: os arquivos
// que terminaram ficaram no destino, e o que estava pela metade foi apagado.
func warnPartialCopy(j *jobs.Job, err error) {
	if err == nil {
		return
	}
	v := j.Snapshot()
	if v.Total > 1 && v.Done > 0 {
		j.Warn(fmt.Sprintf("partial copy: %d of %d files copied; the finished ones were kept, the unfinished one was removed", v.Done, v.Total))
	}
}

// interruptedMessage is the history text of a job that a restart cut short, after its cleanup.
func interruptedMessage(typ string, total int) string {
	const head = "interrupted by server restart; "
	switch typ {
	case "copy":
		// done vem do último retrato persistido (no máximo 1 s antes da queda) e pode estar
		// atrasado, então decide pelo total: com vários arquivos, pode ter sobrado parte.
		if total > 1 {
			return head + fmt.Sprintf("partial copy of %d files: the finished ones were kept, the unfinished one was removed", total)
		}
		return head + "the unfinished copy was removed"
	case "move":
		return head + "items already moved were kept at the destination, the unfinished copy was removed"
	case "extract":
		return head + "the partial extraction was removed"
	case "archive":
		return head + "the unfinished .zip was removed"
	}
	return head + "leftovers were removed"
}

// recoverInterruptedJobs consumes the cleanup rows of jobs that are no longer running. Um caminho
// cuja pasta-mãe não existe fica para a próxima rodada: é o caso de um disco que ainda não foi
// montado, e apagar a linha agora deixaria temporários esquecidos para sempre quando ele voltar.
func (s *Server) recoverInterruptedJobs(ctx context.Context) {
	rows, err := s.db.ListOrphanCleanup(ctx)
	if err != nil {
		s.log.Warn("job recovery", "err", err)
		return
	}
	pending := map[string]bool{}
	for _, c := range rows {
		if err := ctx.Err(); err != nil {
			return
		}
		if parent := vfs.Dir(c.Path); parent != "" {
			if _, err := s.base.StatReserved(vfs.Dir(parent), vfs.Base(parent)); err != nil {
				pending[c.JobID] = true
				continue
			}
		}
		switch c.Mode {
		case cleanupTree:
			err = s.base.RemoveTree(ctx, c.Path, nil)
			if errors.Is(err, vfs.ErrNotFound) {
				err = nil
			}
		default:
			_, err = s.base.RemoveTemps(ctx, c.Path, c.JobID)
		}
		if err != nil {
			s.log.Warn("job recovery", "job", c.JobID, "path", c.Path, "err", err)
			pending[c.JobID] = true
			continue
		}
		_ = s.db.DeleteJobCleanupPath(ctx, c.JobID, c.Path)
		if c.Mode == cleanupTree {
			s.indexRemove(c.Path)
		} else {
			s.indexTree(c.Path)
		}
	}
	done := map[string]bool{}
	for _, c := range rows {
		if pending[c.JobID] || done[c.JobID] {
			continue
		}
		done[c.JobID] = true
		if err := s.db.SetJobError(ctx, c.JobID, interruptedMessage(c.Type, c.Total)); err != nil {
			s.log.Warn("job recovery message", "job", c.JobID, "err", err)
		}
		s.log.Info("cleaned up after interrupted job", "job", c.JobID, "type", c.Type)
	}
}
