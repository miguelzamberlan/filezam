package vfs

import (
	"sort"
	"strings"
	"unicode"
)

// SortEntries orders a listing like the web UI does: directories first, then
// by key ("name" natural/case-insensitive, "size" or "mtime"), name as tie-break.
// desc inverts the key order but keeps directories first.
func SortEntries(entries []Entry, key string, desc bool) {
	less := func(a, b Entry) bool {
		ad, bd := a.Type == "dir", b.Type == "dir"
		if ad != bd {
			return ad
		}
		var c int
		switch key {
		case "size":
			c = cmpInt(a.Size, b.Size)
		case "mtime":
			c = cmpInt(a.Mtime, b.Mtime)
		case "type":
			c = strings.Compare(Ext(a.Name), Ext(b.Name))
		}
		if c == 0 {
			c = NaturalCompare(a.Name, b.Name)
			if key != "name" && c != 0 {
				return c < 0 // desempate por nome nunca inverte
			}
		}
		if desc {
			return c > 0
		}
		return c < 0
	}
	sort.SliceStable(entries, func(i, j int) bool { return less(entries[i], entries[j]) })
}

func cmpInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// NaturalCompare compares case-insensitively, treating digit runs as numbers
// ("img2" < "img10"), and falls back to byte order so the result is total.
func NaturalCompare(a, b string) int {
	ar, br := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	i, j := 0, 0
	for i < len(ar) && j < len(br) {
		if unicode.IsDigit(ar[i]) && unicode.IsDigit(br[j]) {
			si := i
			for i < len(ar) && unicode.IsDigit(ar[i]) {
				i++
			}
			sj := j
			for j < len(br) && unicode.IsDigit(br[j]) {
				j++
			}
			na, nb := strings.TrimLeft(string(ar[si:i]), "0"), strings.TrimLeft(string(br[sj:j]), "0")
			if len(na) != len(nb) {
				return cmpInt(int64(len(na)), int64(len(nb)))
			}
			if na != nb {
				return strings.Compare(na, nb)
			}
			continue
		}
		if ar[i] != br[j] {
			if ar[i] < br[j] {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	if c := cmpInt(int64(len(ar)-i), int64(len(br)-j)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// Ext returns the lowercase extension of a file name without the dot ("" when none or hidden-only).
func Ext(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 {
		return ""
	}
	return strings.ToLower(name[i+1:])
}
