package vfs

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ReservedPrefix marks internal files (upload parts) hidden from users.
const ReservedPrefix = ".filezam-"

const maxPathLen = 4096
const maxNameLen = 255

// MaxDepth caps how many segments an addressable path may have. Every os.Root operation
// resolves a path component by component, so recursive operations over a chain of depth d
// cost O(d²) syscalls; without a cap a user could build chains that keep the indexer,
// search, quota scans and zips busy for hours (measured: depth 2000 ≈ 100 s per walk).
const MaxDepth = 128

// WalkMaxDepth is where recursive read-only walks (index, search, scan, zip) stop
// descending. Moves can nest existing chains past MaxDepth; anything deeper than this
// is skipped rather than walked, which bounds the cost of every shared background task.
const WalkMaxDepth = 256

// Normalize cleans a user-supplied, slash-separated relative path.
// The result has no leading or trailing slash; "" denotes the root.
func Normalize(p string) (string, error) {
	if len(p) > maxPathLen {
		return "", fmt.Errorf("%w: too long", ErrInvalidPath)
	}
	if strings.IndexByte(p, 0) >= 0 {
		return "", fmt.Errorf("%w: contains NUL", ErrInvalidPath)
	}
	if !utf8.ValidString(p) {
		return "", fmt.Errorf("%w: invalid UTF-8", ErrInvalidPath)
	}
	var segs []string
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "", ".":
			continue
		case "..":
			if len(segs) == 0 {
				return "", fmt.Errorf("%w: traversal", ErrInvalidPath)
			}
			segs = segs[:len(segs)-1]
			continue
		}
		if len(seg) > maxNameLen {
			return "", fmt.Errorf("%w: segment too long", ErrInvalidPath)
		}
		segs = append(segs, seg)
	}
	if len(segs) > MaxDepth {
		return "", fmt.Errorf("%w: too deep", ErrInvalidPath)
	}
	return strings.Join(segs, "/"), nil
}

// NormalizeWritable is Normalize plus a check that no segment is reserved.
func NormalizeWritable(p string) (string, error) {
	c, err := Normalize(p)
	if err != nil {
		return "", err
	}
	if IsReserved(c) {
		return "", fmt.Errorf("%w: reserved name", ErrInvalidPath)
	}
	return c, nil
}

// IsReserved reports whether any segment uses the reserved prefix.
func IsReserved(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(seg, ReservedPrefix) {
			return true
		}
	}
	return false
}

// ValidName validates a single file or directory name for create/rename.
func ValidName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("%w: empty or dot", ErrInvalidName)
	}
	if len(name) > maxNameLen {
		return fmt.Errorf("%w: too long", ErrInvalidName)
	}
	if strings.ContainsAny(name, "/\x00") {
		return fmt.Errorf("%w: contains slash or NUL", ErrInvalidName)
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("%w: invalid UTF-8", ErrInvalidName)
	}
	if strings.HasPrefix(name, ReservedPrefix) {
		return fmt.Errorf("%w: reserved prefix", ErrInvalidName)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: control character", ErrInvalidName)
		}
	}
	return nil
}

// Join joins normalized components.
func Join(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "/")
}

// Depth returns the number of segments in a normalized path (0 for the root).
func Depth(p string) int {
	if p == "" {
		return 0
	}
	return strings.Count(p, "/") + 1
}

// Base returns the last element of a normalized path ("" for root).
func Base(p string) string {
	if p == "" {
		return ""
	}
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// Dir returns the parent of a normalized path ("" for top-level entries).
func Dir(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

// IsWithin reports whether child equals or is nested under parent.
func IsWithin(parent, child string) bool {
	if parent == "" {
		return true
	}
	return child == parent || strings.HasPrefix(child, parent+"/")
}

// osPath converts a normalized path into an os.Root argument.
func osPath(p string) string {
	if p == "" {
		return "."
	}
	return p
}

// SplitExt splits "name.tar.gz" into ("name.tar", ".gz"); dotfiles keep their name.
func SplitExt(name string) (string, string) {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 {
		return name, ""
	}
	return name[:i], name[i:]
}
