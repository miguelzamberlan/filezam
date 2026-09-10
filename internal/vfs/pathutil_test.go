package vfs

import (
	"strings"
	"testing"
)

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
	for _, bad := range []string{"", ".", "..", "a/b", "a\x00", ".filezam-x", "\xff", "a\nb", "a\rb", "tab\there", "del\x7f"} {
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

func TestSortEntries(t *testing.T) {
	if NaturalCompare("img2.jpg", "img10.jpg") >= 0 || NaturalCompare("B", "a") <= 0 || NaturalCompare("x", "x") != 0 {
		t.Fatal("natural compare")
	}
	es := []Entry{{Name: "b.txt", Type: "file", Size: 5, Mtime: 3}, {Name: "Z", Type: "dir"}, {Name: "a10", Type: "file", Size: 1, Mtime: 9}, {Name: "a9", Type: "file", Size: 1, Mtime: 1}, {Name: "c", Type: "dir"}}
	SortEntries(es, "name", false)
	if got := names(es); got != "c Z a9 a10 b.txt" {
		t.Fatalf("name asc: %s", got)
	}
	SortEntries(es, "name", true)
	if got := names(es); got != "Z c b.txt a10 a9" {
		t.Fatalf("name desc: %s", got)
	}
	SortEntries(es, "size", true)
	if got := names(es); got != "c Z b.txt a9 a10" {
		t.Fatalf("size desc: %s", got)
	}
	SortEntries(es, "mtime", false)
	if got := names(es); got != "c Z a9 b.txt a10" {
		t.Fatalf("mtime asc: %s", got)
	}
}

func names(es []Entry) string {
	out := ""
	for i, e := range es {
		if i > 0 {
			out += " "
		}
		out += e.Name
	}
	return out
}

func TestNormalizeDepth(t *testing.T) {
	ok := strings.Repeat("d/", MaxDepth-1) + "d"
	if _, err := Normalize(ok); err != nil {
		t.Fatalf("depth %d should be accepted: %v", MaxDepth, err)
	}
	if _, err := Normalize(ok + "/d"); err == nil {
		t.Fatalf("depth %d should be refused", MaxDepth+1)
	}
	if Depth("") != 0 || Depth("a") != 1 || Depth("a/b/c") != 3 {
		t.Fatal("Depth")
	}
}
