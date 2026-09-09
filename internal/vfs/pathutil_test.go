package vfs

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"", "", true},
		{"/", "", true},
		{".", "", true},
		{"./", "", true},
		{"a", "a", true},
		{"/a", "a", true},
		{"a/", "a", true},
		{"a/b/c", "a/b/c", true},
		{"a//b", "a/b", true},
		{"a/./b", "a/b", true},
		{"a/b/../c", "a/c", true},
		{"a/../b", "b", true},
		{"..", "", false},
		{"../", "", false},
		{"/..", "", false},
		{"../a", "", false},
		{"a/../../b", "", false},
		{"a/../..", "", false},
		{"....", "....", true},
		{"..a", "..a", true},
		{"a..", "a..", true},
		{"a\\..\\b", "a\\..\\b", true},
		{"a\x00b", "", false},
		{"\xff", "", false},
		{"ção/ünicode", "ção/ünicode", true},
		{string(make([]byte, 5000)), "", false},
	}
	for _, c := range cases {
		got, err := Normalize(c.in)
		if (err == nil) != c.ok {
			t.Errorf("Normalize(%q): ok=%v want %v (err=%v)", c.in, err == nil, c.ok, err)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("Normalize(%q)=%q want %q", c.in, got, c.want)
		}
	}
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'x'
	}
	if _, err := Normalize(string(long)); err == nil {
		t.Error("300-char segment accepted")
	}
	if _, err := NormalizeWritable("a/.filezam-upload-x.part"); err == nil {
		t.Error("reserved name accepted for write")
	}
	if _, err := Normalize("a/.filezam-upload-x.part"); err != nil {
		t.Error("reserved name should normalize for read")
	}
}

func TestValidName(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "a/b", "a\x00", ".filezam-x", "\xff"} {
		if ValidName(bad) == nil {
			t.Errorf("ValidName(%q) accepted", bad)
		}
	}
	for _, good := range []string{"a", "..a", "a b", "ção", ".hidden", "x.tar.gz"} {
		if err := ValidName(good); err != nil {
			t.Errorf("ValidName(%q) rejected: %v", good, err)
		}
	}
}

func TestPathHelpers(t *testing.T) {
	if Join("", "a", "", "b") != "a/b" {
		t.Error("Join")
	}
	if Base("a/b/c") != "c" || Base("a") != "a" || Base("") != "" {
		t.Error("Base")
	}
	if Dir("a/b/c") != "a/b" || Dir("a") != "" {
		t.Error("Dir")
	}
	if !IsWithin("a", "a/b") || !IsWithin("a", "a") || IsWithin("a", "ab") || !IsWithin("", "x") {
		t.Error("IsWithin")
	}
	b, e := SplitExt("x.tar.gz")
	if b != "x.tar" || e != ".gz" {
		t.Error("SplitExt")
	}
	b, e = SplitExt(".bashrc")
	if b != ".bashrc" || e != "" {
		t.Error("SplitExt dotfile")
	}
}
