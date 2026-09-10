package vfs

import (
	"context"
	"errors"
	"io/fs"
	"strings"
)

// Found is a search hit: Dir is the parent (root-relative, "" = root) and Entry the item.
type Found struct {
	Dir   string `json:"dir"`
	Entry Entry  `json:"entry"`
}

// SearchLimits bounds a Find call so huge trees never block a request.
type SearchLimits struct {
	MaxScan    int // entries visited before giving up (partial=true)
	MaxResults int // hits returned before stopping (partial=true)
}

// Find walks p (lstat only, symlinks never followed) and returns the entries
// whose name contains q, case-insensitively. Reserved names are skipped. It
// stops early at the limits or when ctx is done, reporting partial=true.
func (r *Root) Find(ctx context.Context, p, q string, lim SearchLimits) (hits []Found, partial bool, err error) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil, false, errors.New("empty query")
	}
	hits = []Found{}
	scanned := 0
	err = r.walk(ctx, p, func(path string, fi fs.FileInfo) error {
		if path == p {
			return nil // a própria pasta pesquisada não é resultado
		}
		scanned++
		if scanned > lim.MaxScan {
			return ErrScanLimit
		}
		name := fi.Name()
		if strings.HasPrefix(name, ReservedPrefix) {
			return nil
		}
		if !strings.Contains(strings.ToLower(name), q) {
			return nil
		}
		hits = append(hits, Found{Dir: Dir(path), Entry: entryFromInfo(r, Dir(path), fi)})
		if len(hits) >= lim.MaxResults {
			return ErrScanLimit
		}
		return nil
	})
	if errors.Is(err, ErrScanLimit) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return hits, true, nil
	}
	return hits, false, err
}

// WalkEntries calls fn for p itself (unless p is the root) and every descendant, as Entry
// values with root-relative paths. Lstat only: symlinks are reported but never followed;
// reserved names are skipped. Meant for background indexing (no limits besides ctx).
func (r *Root) WalkEntries(ctx context.Context, p string, fn func(path string, e Entry) error) error {
	return r.walk(ctx, p, func(path string, fi fs.FileInfo) error {
		if path == "" {
			return nil
		}
		return fn(path, entryFromInfo(r, Dir(path), fi))
	})
}
