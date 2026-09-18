// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package media

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Limites da travessia de IFDs. Um EXIF é escrito por quem produziu o arquivo e pode
// apontar para onde quiser, inclusive para si mesmo: sem tetos, uma cadeia circular de
// ponteiros roda para sempre.
const (
	maxIFDs       = 24
	maxIFDEntries = 512
	maxIFDDepth   = 3
	// maxASCII limita o que uma string EXIF pode ocupar: nome de câmera e de lente são
	// dezenas de bytes, não megabytes.
	maxASCII = 256
)

// Tags que interessam. O resto do EXIF é ignorado sem ser lido.
const (
	tagImageWidth       = 0x0100
	tagImageLength      = 0x0101
	tagMake             = 0x010f
	tagModel            = 0x0110
	tagOrientation      = 0x0112
	tagDateTime         = 0x0132
	tagSubIFDs          = 0x014a
	tagExifIFD          = 0x8769
	tagExposureTime     = 0x829a
	tagFNumber          = 0x829d
	tagISO              = 0x8827
	tagDateTimeOriginal = 0x9003
	tagFocalLength      = 0x920a
	tagPixelXDimension  = 0xa002
	tagPixelYDimension  = 0xa003
	tagLensModel        = 0xa434
)

// exifInfo is what the IFD walk collected.
type exifInfo struct {
	found       bool
	orientation int
	// width/height vêm de PixelXDimension quando existem; caso contrário, do par
	// ImageWidth/ImageLength do IFD de maior área — nos RAW o IFD0 costuma ser só a
	// miniatura de visualização, e o quadro cheio mora num SubIFD.
	width, height int
	area          int64
	exifW, exifH  int
	make_, model  string
	lens          string
	iso           int
	exposure      string
	fnumber       float64
	focal         float64
	taken         int64
}

// camera joins make and model without repeating the brand. A comparação é pela primeira
// palavra da marca porque o campo costuma vir inflado ("NIKON CORPORATION" + "NIKON D750"
// é uma câmera só, e quem agrupa por câmera espera ver "NIKON D750").
func (e exifInfo) camera() string {
	mk, md := printable(e.make_, 64), printable(e.model, 64)
	switch {
	case mk == "":
		return md
	case md == "":
		return mk
	}
	first, _, _ := strings.Cut(strings.ToUpper(mk), " ")
	if first != "" && strings.HasPrefix(strings.ToUpper(md), first) {
		return md
	}
	return mk + " " + md
}

// dims returns the picture size the EXIF describes, 0 when it does not say.
func (e exifInfo) dims() (int, int) {
	if e.exifW > 0 && e.exifH > 0 {
		return e.exifW, e.exifH
	}
	return e.width, e.height
}

// apply copies the photographic fields onto a Meta.
func (e exifInfo) apply(m *Meta) {
	m.Camera = e.camera()
	m.Lens = printable(e.lens, 64)
	m.ISO = e.iso
	m.Exposure = e.exposure
	m.FNumber = e.fnumber
	m.FocalLength = e.focal
	m.TakenAt = e.taken
}

// rotationFromOrientation converts the EXIF orientation to a clockwise rotation.
// Os valores espelhados (2, 4, 5, 7) entram pelo giro equivalente: para contar retrato e
// paisagem o que importa é se os lados trocam, não o espelhamento.
func rotationFromOrientation(o int) int {
	switch o {
	case 3, 4:
		return 180
	case 5, 6:
		return 90
	case 7, 8:
		return 270
	}
	return 0
}

// tiffReader walks a TIFF header (the EXIF block of a JPEG, or a TIFF/RAW file itself).
// Todo deslocamento é relativo a base e é conferido contra end antes de virar leitura.
type tiffReader struct {
	s    section
	base int64
	end  int64
	big  bool
}

func (t *tiffReader) bytesAt(off int64, n int) ([]byte, error) {
	abs := t.base + off
	if off < 0 || abs < t.base || abs+int64(n) > t.end {
		return nil, io.EOF
	}
	return t.s.exact(abs, n)
}

func (t *tiffReader) u16(off int64) (uint16, error) {
	b, err := t.bytesAt(off, 2)
	if err != nil {
		return 0, err
	}
	if t.big {
		return be16(b), nil
	}
	return le16(b), nil
}

func (t *tiffReader) u32(off int64) (uint32, error) {
	b, err := t.bytesAt(off, 4)
	if err != nil {
		return 0, err
	}
	if t.big {
		return be32(b), nil
	}
	return le32(b), nil
}

func (t *tiffReader) num16(b []byte) uint16 {
	if t.big {
		return be16(b)
	}
	return le16(b)
}

func (t *tiffReader) num32(b []byte) uint32 {
	if t.big {
		return be32(b)
	}
	return le32(b)
}

// ifdEntry is one 12-byte directory entry, already split.
type ifdEntry struct {
	tag   uint16
	typ   uint16
	count uint32
	val   []byte // os 4 bytes finais: o valor em si, ou o deslocamento para ele
}

var typeSize = [...]int{0, 1, 1, 2, 4, 8, 1, 1, 2, 4, 8, 4, 8}

func (e ifdEntry) size() int64 {
	if int(e.typ) >= len(typeSize) {
		return 0
	}
	return int64(typeSize[e.typ]) * int64(e.count)
}

// data returns the entry payload, inline when it fits in the four bytes.
func (t *tiffReader) data(e ifdEntry) ([]byte, error) {
	n := e.size()
	if n <= 0 || n > maxASCII*4 {
		return nil, errTooBig
	}
	if n <= 4 {
		return e.val[:n], nil
	}
	return t.bytesAt(int64(t.num32(e.val)), int(n))
}

// uint reads the first numeric value of an entry.
func (t *tiffReader) uint(e ifdEntry) (uint32, bool) {
	switch e.typ {
	case 1, 6, 7: // BYTE, SBYTE, UNDEFINED
		if e.count == 0 {
			return 0, false
		}
		return uint32(e.val[0]), true
	case 3, 8: // SHORT, SSHORT
		if e.count == 0 {
			return 0, false
		}
		return uint32(t.num16(e.val)), true
	case 4, 9: // LONG, SLONG
		if e.count == 0 {
			return 0, false
		}
		return t.num32(e.val), true
	}
	return 0, false
}

// rational reads the first RATIONAL value as numerator and denominator.
func (t *tiffReader) rational(e ifdEntry) (int64, int64, bool) {
	if e.typ != 5 && e.typ != 10 {
		return 0, 0, false
	}
	b, err := t.data(e)
	if err != nil || len(b) < 8 {
		return 0, 0, false
	}
	den := int64(int32(t.num32(b[4:])))
	if den == 0 {
		return 0, 0, false
	}
	return int64(int32(t.num32(b))), den, true
}

func (t *tiffReader) ascii(e ifdEntry) string {
	if e.typ != 2 {
		return ""
	}
	b, err := t.data(e)
	if err != nil {
		return ""
	}
	// A string do EXIF termina no primeiro zero. Cortar ali, e não confiar no comprimento
	// declarado, é o que impede que uma contagem esticada arraste o valor vizinho junto.
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return printable(string(b), maxASCII)
}

// parseExif walks the TIFF block at base and collects what exifInfo holds.
func parseExif(s section, base, end int64) (exifInfo, bool) {
	var info exifInfo
	hdr, err := s.exact(base, 8)
	if err != nil {
		return info, false
	}
	var t tiffReader
	switch {
	case hdr[0] == 'I' && hdr[1] == 'I':
		t = tiffReader{s: s, base: base, end: end, big: false}
	case hdr[0] == 'M' && hdr[1] == 'M':
		t = tiffReader{s: s, base: base, end: end, big: true}
	default:
		return info, false
	}
	if t.num16(hdr[2:4]) != 42 {
		return info, false
	}
	first := int64(t.num32(hdr[4:8]))
	seen := map[int64]bool{}
	budget := maxIFDs
	t.walk(first, 0, &budget, seen, &info)
	info.found = true
	return info, true
}

// walk reads one IFD and follows the pointers that matter (Exif IFD, SubIFDs and the
// next directory), never revisiting an offset and never past the budget.
func (t *tiffReader) walk(off int64, depth int, budget *int, seen map[int64]bool, info *exifInfo) {
	for off > 0 && depth <= maxIFDDepth {
		if *budget <= 0 || seen[off] {
			return
		}
		seen[off] = true
		*budget--
		n, err := t.u16(off)
		if err != nil || n == 0 {
			return
		}
		if n > maxIFDEntries {
			n = maxIFDEntries
		}
		var w, h int
		var subs []int64
		var exifOff int64
		for i := int64(0); i < int64(n); i++ {
			raw, err := t.bytesAt(off+2+i*12, 12)
			if err != nil {
				return
			}
			e := ifdEntry{tag: t.num16(raw), typ: t.num16(raw[2:]), count: t.num32(raw[4:]), val: raw[8:12]}
			switch e.tag {
			case tagImageWidth:
				if v, ok := t.uint(e); ok {
					w = int(v)
				}
			case tagImageLength:
				if v, ok := t.uint(e); ok {
					h = int(v)
				}
			case tagPixelXDimension:
				if v, ok := t.uint(e); ok && info.exifW == 0 {
					info.exifW = int(v)
				}
			case tagPixelYDimension:
				if v, ok := t.uint(e); ok && info.exifH == 0 {
					info.exifH = int(v)
				}
			case tagOrientation:
				if v, ok := t.uint(e); ok && info.orientation == 0 && v >= 1 && v <= 8 {
					info.orientation = int(v)
				}
			case tagMake:
				if info.make_ == "" {
					info.make_ = t.ascii(e)
				}
			case tagModel:
				if info.model == "" {
					info.model = t.ascii(e)
				}
			case tagLensModel:
				if info.lens == "" {
					info.lens = t.ascii(e)
				}
			case tagISO:
				if v, ok := t.uint(e); ok && info.iso == 0 && v < 10_000_000 {
					info.iso = int(v)
				}
			case tagExposureTime:
				if num, den, ok := t.rational(e); ok && info.exposure == "" {
					info.exposure = formatExposure(num, den)
				}
			case tagFNumber:
				if num, den, ok := t.rational(e); ok && info.fnumber == 0 {
					info.fnumber = round2(float64(num) / float64(den))
				}
			case tagFocalLength:
				if num, den, ok := t.rational(e); ok && info.focal == 0 {
					info.focal = round2(float64(num) / float64(den))
				}
			case tagDateTimeOriginal:
				if v := parseExifTime(t.ascii(e)); v != 0 {
					info.taken = v // o original sempre ganha do DateTime genérico
				}
			case tagDateTime:
				if v := parseExifTime(t.ascii(e)); v != 0 && info.taken == 0 {
					info.taken = v
				}
			case tagExifIFD:
				if v, ok := t.uint(e); ok {
					exifOff = int64(v)
				}
			case tagSubIFDs:
				subs = append(subs, t.subOffsets(e)...)
			}
		}
		// Nos RAW cada IFD descreve uma imagem diferente (miniatura, visualização, quadro
		// cheio): fica a de maior área.
		if w > 0 && h > 0 && int64(w)*int64(h) > info.area {
			info.width, info.height, info.area = w, h, int64(w)*int64(h)
		}
		if exifOff > 0 {
			t.walk(exifOff, depth+1, budget, seen, info)
		}
		for _, so := range subs {
			t.walk(so, depth+1, budget, seen, info)
		}
		next, err := t.u32(off + 2 + int64(n)*12)
		if err != nil || next == 0 {
			return
		}
		off = int64(next)
	}
}

// subOffsets reads the SubIFDs pointer list (one LONG, or several).
func (t *tiffReader) subOffsets(e ifdEntry) []int64 {
	if e.typ != 4 && e.typ != 13 {
		return nil
	}
	if e.count == 1 {
		return []int64{int64(t.num32(e.val))}
	}
	if e.count == 0 || e.count > 16 {
		return nil
	}
	b, err := t.bytesAt(int64(t.num32(e.val)), int(e.count)*4)
	if err != nil {
		return nil
	}
	out := make([]int64, 0, e.count)
	for i := 0; i+4 <= len(b); i += 4 {
		out = append(out, int64(t.num32(b[i:])))
	}
	return out
}

func round2(v float64) float64 {
	if v <= 0 || v > 1e9 {
		return 0
	}
	return float64(int64(v*100+0.5)) / 100
}

// formatExposure writes the shutter speed the way a camera does: "1/125" acima de um
// segundo de denominador, o valor decimal abaixo disso.
func formatExposure(num, den int64) string {
	if num <= 0 || den <= 0 {
		return ""
	}
	if num == 1 {
		return "1/" + strconv.FormatInt(den, 10)
	}
	v := float64(num) / float64(den)
	if v < 1 {
		return "1/" + strconv.FormatInt(int64(float64(den)/float64(num)+0.5), 10)
	}
	return strings.TrimSuffix(strconv.FormatFloat(v, 'f', 1, 64), ".0")
}

// parseExifTime reads "2026:03:14 09:41:07". O EXIF não diz o fuso, então o horário é
// lido como UTC e a interface o mostra em UTC: assim o relógio que aparece é o mesmo que
// estava na câmera, em vez de escorregar com o fuso de quem olha.
func parseExifTime(s string) int64 {
	s = strings.TrimSpace(s)
	if len(s) < 19 {
		return 0
	}
	var y, mo, d, h, mi, sec int
	if _, err := fmt.Sscanf(s[:19], "%4d:%2d:%2d %2d:%2d:%2d", &y, &mo, &d, &h, &mi, &sec); err != nil {
		return 0
	}
	if y < 1900 || y > 3000 || mo < 1 || mo > 12 || d < 1 || d > 31 {
		return 0
	}
	return time.Date(y, time.Month(mo), d, h, mi, sec, 0, time.UTC).UnixMilli()
}
