package vfs

import "unicode/utf8"

func validUTF8(s string) bool { return utf8.ValidString(s) }
