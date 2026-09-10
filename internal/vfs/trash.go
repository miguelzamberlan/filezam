package vfs

import (
	"context"
	"errors"
)

// TrashDirName is the reserved folder (at the scope root) that holds deleted items as <id>/<name>.
// The prefix keeps it out of listings, searches, copies, zips and client-addressable paths.
const TrashDirName = ReservedPrefix + "trash"

// MoveToTrash moves src into trashDir/id/<base of src>. Across devices it falls back to
// copy + delete. It never overwrites: the id folder is fresh.
func (r *Root) MoveToTrash(ctx context.Context, src, trashDir, id string) error {
	if src == "" || id == "" {
		return ErrRootOp
	}
	if err := r.MkdirAllReserved(Join(trashDir, id)); err != nil {
		return err
	}
	return r.moveOrCopy(ctx, src, Join(trashDir, id, Base(src)))
}

// RestoreFromTrash moves trashDir/id/name back to dst (whose parent must exist) and removes
// the now-empty id folder. dst must not exist (the caller picks a unique name).
func (r *Root) RestoreFromTrash(ctx context.Context, trashDir, id, name, dst string) error {
	if id == "" || dst == "" {
		return ErrRootOp
	}
	if err := r.moveOrCopy(ctx, Join(trashDir, id, name), dst); err != nil {
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

func (r *Root) moveOrCopy(ctx context.Context, src, dst string) error {
	_, err := r.MoveTo(src, dst, false)
	if err == nil || !errors.Is(err, ErrCrossDevice) {
		return err
	}
	if err := r.CopyTree(ctx, src, dst, ConflictSkip, nil); err != nil {
		return err
	}
	return r.RemoveTree(ctx, src, nil)
}

// MkdirAllReserved creates internal folders (reserved names allowed, unlike MkdirAll).
func (r *Root) MkdirAllReserved(p string) error {
	if p == "" {
		return nil
	}
	return MapError(r.r.MkdirAll(p, 0o755))
}
