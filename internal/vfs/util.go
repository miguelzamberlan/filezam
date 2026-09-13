package vfs

import (
	"crypto/rand"
	"encoding/hex"
	"unicode/utf8"
)

func validUTF8(s string) bool { return utf8.ValidString(s) }

// randHex returns 2n random hex characters, for temporary names that must not collide.
func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
