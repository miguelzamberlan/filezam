package vfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
)

// TrashDirName is the reserved folder (at the scope root) that holds deleted items as <id>/<name>.
// The prefix keeps it out of listings, searches, copies, zips and client-addressable paths.
const TrashDirName = ReservedPrefix + "trash"

// ErrSourceNotRemoved wraps a failure after the item was copied whole into its destination but
// before the source was fully removed (copy + delete across devices).
var ErrSourceNotRemoved = errors.New("copied, but the source was not fully removed")

// MoveToTrash moves src into trashDir/id/<base of src>. Across devices it falls back to
// copy + delete, with temporaries tagged id. It never overwrites: the id folder is fresh.
func (r *Root) MoveToTrash(ctx context.Context, src, trashDir, id string) error {
	if src == "" || id == "" {
		return ErrRootOp
	}
	if err := r.MkdirAllReserved(Join(trashDir, id)); err != nil {
		return err
	}
	return r.moveOrCopy(ctx, src, Join(trashDir, id, Base(src)), id)
}

// ResumeMoveToTrash finishes a MoveToTrash that a crash cut short. Como a cópia só publica
// arquivos inteiros, o que já está na lixeira é bom: apaga os temporários da operação, copia o que
// falta pulando o que existe e remove a origem. Sem nada na lixeira ainda, é o movimento normal.
func (r *Root) ResumeMoveToTrash(ctx context.Context, src, trashDir, id string) error {
	if src == "" || id == "" {
		return ErrRootOp
	}
	dst := Join(trashDir, id, Base(src))
	if _, err := r.RemoveTemps(ctx, dst, id); err != nil {
		return err
	}
	dfi, err := r.r.Lstat(dst)
	if errors.Is(err, fs.ErrNotExist) {
		return r.MoveToTrash(ctx, src, trashDir, id)
	}
	if err != nil {
		return MapError(err)
	}
	if dfi.IsDir() {
		if err := r.CopyTree(ctx, src, dst, ConflictSkip, &Progress{Tag: id}); err != nil {
			return err
		}
	}
	// Arquivo solto já publicado na lixeira está inteiro: falta só tirar a origem.
	return r.RemoveTree(ctx, src, nil)
}

// RestoreFromTrash moves trashDir/id/name back to dst (whose parent must exist) and removes
// the now-empty id folder. dst must not exist (the caller picks a unique name).
func (r *Root) RestoreFromTrash(ctx context.Context, trashDir, id, name, dst string) error {
	if id == "" || dst == "" {
		return ErrRootOp
	}
	if err := r.moveOrCopy(ctx, Join(trashDir, id, name), dst, id); err != nil {
		return err
	}
	return r.Remove(Join(trashDir, id))
}

// RemoveTrashItem deletes trashDir/id permanently.
func (r *Root) RemoveTrashItem(ctx context.Context, trashDir, id string) error {
	if id == "" {
		return ErrRootOp
	}
	return r.RemoveTree(ctx, Join(trashDir, id), nil)
}

func (r *Root) moveOrCopy(ctx context.Context, src, dst, tag string) error {
	_, err := r.MoveTo(src, dst, false)
	if err == nil || !errors.Is(err, ErrCrossDevice) {
		return err
	}
	if err := r.CopyTree(ctx, src, dst, ConflictSkip, &Progress{Tag: tag}); err != nil {
		return err
	}
	if err := r.RemoveTree(ctx, src, nil); err != nil {
		return fmt.Errorf("%w: %w", ErrSourceNotRemoved, err)
	}
	return nil
}

// MkdirAllReserved creates internal folders (reserved names allowed, unlike MkdirAll).
func (r *Root) MkdirAllReserved(p string) error {
	if p == "" {
		return nil
	}
	return MapError(r.r.MkdirAll(p, 0o755))
}
