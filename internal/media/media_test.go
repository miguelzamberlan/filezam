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
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"testing"
)

// probe é o atalho dos testes: monta a seção sobre os bytes e chama Probe.
func probe(t *testing.T, name string, b []byte) Meta {
	t.Helper()
	m, err := Probe(bytes.NewReader(b), name, int64(len(b)))
	if err != nil {
		t.Fatalf("Probe(%s): %v", name, err)
	}
	return m
}

func probeErr(t *testing.T, name string, b []byte) error {
	t.Helper()
	_, err := Probe(bytes.NewReader(b), name, int64(len(b)))
	return err
}

// ---- imagens ----

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{1, 2, 3, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestProbePNG(t *testing.T) {
	m := probe(t, "foto.png", pngBytes(t, 640, 480))
	if m.Kind != KindImage || m.Format != "png" || m.Width != 640 || m.Height != 480 {
		t.Fatalf("png: %+v", m)
	}
	if m.Orientation() != Landscape {
		t.Fatalf("orientação: %q", m.Orientation())
	}
}

func TestProbeJPEGWithoutExif(t *testing.T) {
	m := probe(t, "foto.jpg", jpegBytes(t, 320, 200))
	if m.Width != 320 || m.Height != 200 || m.Format != "jpeg" {
		t.Fatalf("jpeg: %+v", m)
	}
}

// ---- montagem de EXIF ----

type tent struct {
	tag  uint16
	typ  uint16
	cnt  uint32
	data []byte
}

func short(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }
func long(v uint32) []byte  { return binary.LittleEndian.AppendUint32(nil, v) }
func ratio(n, d uint32) []byte {
	return append(binary.LittleEndian.AppendUint32(nil, n), binary.LittleEndian.AppendUint32(nil, d)...)
}
func ascii(s string) []byte { return append([]byte(s), 0) }

// buildTIFF lays out a little-endian TIFF: cabeçalho, IFD0, IFD do Exif e a área de
// valores que não cabem nos quatro bytes da entrada.
func buildTIFF(ifd0, exif []tent) []byte {
	const hdr = 8
	ifd0Len := 2 + 12*len(ifd0) + 4
	exifLen := 0
	if len(exif) > 0 {
		exifLen = 2 + 12*len(exif) + 4
		ifd0 = append(ifd0, tent{tag: tagExifIFD, typ: 4, cnt: 1, data: long(uint32(hdr + ifd0Len + 12))})
		ifd0Len += 12
	}
	exifOff := hdr + ifd0Len
	dataOff := exifOff + exifLen

	var data []byte
	emit := func(ents []tent) []byte {
		out := short(uint16(len(ents)))
		for _, e := range ents {
			out = append(out, short(e.tag)...)
			out = append(out, short(e.typ)...)
			out = append(out, long(e.cnt)...)
			if len(e.data) <= 4 {
				v := make([]byte, 4)
				copy(v, e.data)
				out = append(out, v...)
			} else {
				out = append(out, long(uint32(dataOff+len(data)))...)
				data = append(data, e.data...)
				if len(data)%2 == 1 {
					data = append(data, 0)
				}
			}
		}
		return append(out, long(0)...)
	}
	b := []byte{'I', 'I', 42, 0}
	b = append(b, long(hdr)...)
	b = append(b, emit(ifd0)...)
	if len(exif) > 0 {
		b = append(b, emit(exif)...)
	}
	return append(b, data...)
}

// jpegWithExif splices an APP1 Exif segment right after the start of image marker.
func jpegWithExif(t *testing.T, w, h int, tiff []byte) []byte {
	t.Helper()
	base := jpegBytes(t, w, h)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xff, 0xe1}
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(payload)+2))
	seg = append(seg, payload...)
	out := append([]byte{}, base[:2]...)
	out = append(out, seg...)
	return append(out, base[2:]...)
}

func TestProbeJPEGExifRotatesAndReadsCamera(t *testing.T) {
	tiff := buildTIFF([]tent{
		{tag: tagOrientation, typ: 3, cnt: 1, data: short(6)}, // 90° no sentido horário
		{tag: tagMake, typ: 2, cnt: 6, data: ascii("Canon")},
		{tag: tagModel, typ: 2, cnt: 13, data: ascii("Canon EOS R5")},
	}, []tent{
		{tag: tagDateTimeOriginal, typ: 2, cnt: 20, data: ascii("2026:03:14 09:41:07")},
		{tag: tagISO, typ: 3, cnt: 1, data: short(800)},
		{tag: tagFNumber, typ: 5, cnt: 1, data: ratio(28, 10)},
		{tag: tagExposureTime, typ: 5, cnt: 1, data: ratio(1, 125)},
		{tag: tagFocalLength, typ: 5, cnt: 1, data: ratio(500, 10)},
		{tag: tagLensModel, typ: 2, cnt: 7, data: ascii("RF50mm")},
	})
	m := probe(t, "dsc_0001.jpg", jpegWithExif(t, 400, 300, tiff))
	if m.Width != 300 || m.Height != 400 {
		t.Fatalf("a orientação 6 devia trocar os lados: %dx%d", m.Width, m.Height)
	}
	if m.Rotation != 90 || m.Orientation() != Portrait {
		t.Fatalf("rotação/orientação: %d %q", m.Rotation, m.Orientation())
	}
	if m.Camera != "Canon EOS R5" {
		t.Fatalf("câmera: %q", m.Camera) // a marca não se repete no modelo
	}
	if got := (exifInfo{make_: "NIKON CORPORATION", model: "NIKON D750"}).camera(); got != "NIKON D750" {
		t.Fatalf("marca inflada: %q", got)
	}
	if got := (exifInfo{make_: "samsung", model: "Galaxy S25 FE"}).camera(); got != "samsung Galaxy S25 FE" {
		t.Fatalf("marca e modelo distintos: %q", got)
	}
	if m.Lens != "RF50mm" || m.ISO != 800 || m.Exposure != "1/125" || m.FNumber != 2.8 || m.FocalLength != 50 {
		t.Fatalf("exif: %+v", m)
	}
	if m.TakenAt == 0 {
		t.Fatal("DateTimeOriginal não foi lido")
	}
}

func TestProbeRawUsesLargestIFD(t *testing.T) {
	// Um RAW típico: o IFD0 descreve a miniatura, o SubIFD o quadro cheio.
	sub := buildTIFF([]tent{
		{tag: tagImageWidth, typ: 4, cnt: 1, data: long(160)},
		{tag: tagImageLength, typ: 4, cnt: 1, data: long(120)},
	}, nil)
	_ = sub
	tiff := buildTIFF([]tent{
		{tag: tagImageWidth, typ: 4, cnt: 1, data: long(160)},
		{tag: tagImageLength, typ: 4, cnt: 1, data: long(120)},
		{tag: tagMake, typ: 2, cnt: 6, data: ascii("NIKON")},
	}, []tent{
		{tag: tagPixelXDimension, typ: 4, cnt: 1, data: long(8256)},
		{tag: tagPixelYDimension, typ: 4, cnt: 1, data: long(5504)},
	})
	m := probe(t, "dsc_0001.nef", tiff)
	if m.Width != 8256 || m.Height != 5504 {
		t.Fatalf("o quadro cheio devia ganhar da miniatura: %dx%d", m.Width, m.Height)
	}
	if m.Format != "nef" || m.Kind != KindImage {
		t.Fatalf("raw: %+v", m)
	}
}

func TestExifPointerLoopStops(t *testing.T) {
	// IFD que aponta para si mesmo: sem o orçamento de travessia isso não terminaria.
	b := []byte{'I', 'I', 42, 0}
	b = append(b, long(8)...)
	b = append(b, short(1)...)
	b = append(b, short(tagOrientation)...)
	b = append(b, short(3)...)
	b = append(b, long(1)...)
	b = append(b, short(1)...)
	b = append(b, 0, 0)
	b = append(b, long(8)...) // próximo IFD = ele mesmo
	if _, err := Probe(bytes.NewReader(b), "x.dng", int64(len(b))); err != nil && err != ErrUnsupported {
		t.Fatalf("erro inesperado: %v", err)
	}
}

// ---- ISO base media ----

func box(typ string, parts ...[]byte) []byte {
	var body []byte
	for _, p := range parts {
		body = append(body, p...)
	}
	out := binary.BigEndian.AppendUint32(nil, uint32(len(body)+8))
	out = append(out, typ...)
	return append(out, body...)
}

func b32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
func b16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }

// identityMatrix é a matriz de exibição sem giro nenhum.
func identityMatrix() []byte {
	m := make([]byte, 36)
	copy(m[0:], b32(1<<16))
	copy(m[16:], b32(1<<16))
	copy(m[32:], b32(1<<30))
	return m
}

// rotate90Matrix é o giro de um quarto de volta no sentido horário.
func rotate90Matrix() []byte {
	neg := int32(-1 << 16)
	m := make([]byte, 36)
	copy(m[4:], b32(1<<16))        // b = 1
	copy(m[12:], b32(uint32(neg))) // c = -1
	copy(m[32:], b32(1<<30))
	return m
}

func tkhd(w, h uint16, matrix []byte) []byte {
	p := make([]byte, 0, 84)
	p = append(p, 0, 0, 0, 0)          // versão 0 e sinalizadores
	p = append(p, make([]byte, 20)...) // criação, modificação, id, reservado, duração
	p = append(p, make([]byte, 16)...) // reservado, camada, grupo, volume, reservado
	p = append(p, matrix...)
	p = append(p, b32(uint32(w)<<16)...)
	p = append(p, b32(uint32(h)<<16)...)
	return box("tkhd", p)
}

func mdhd(timescale, duration uint32) []byte {
	p := append([]byte{0, 0, 0, 0}, make([]byte, 8)...)
	p = append(p, b32(timescale)...)
	p = append(p, b32(duration)...)
	p = append(p, 0, 0, 0, 0)
	return box("mdhd", p)
}

func hdlr(kind string) []byte {
	p := append([]byte{0, 0, 0, 0}, 0, 0, 0, 0)
	p = append(p, kind...)
	p = append(p, make([]byte, 12)...)
	return box("hdlr", p)
}

func visualStsd(fourcc string, w, h uint16) []byte {
	e := b32(86)
	e = append(e, fourcc...)
	e = append(e, make([]byte, 24)...) // reservado, índice, pré-definidos
	e = append(e, b16(w)...)
	e = append(e, b16(h)...)
	e = append(e, make([]byte, 50)...)
	return box("stsd", append([]byte{0, 0, 0, 0}, append(b32(1), e...)...))
}

func stsz(count uint32) []byte {
	return box("stsz", append([]byte{0, 0, 0, 0}, append(b32(0), b32(count)...)...))
}

func mvhd(timescale, duration uint32) []byte {
	p := append([]byte{0, 0, 0, 0}, make([]byte, 8)...)
	p = append(p, b32(timescale)...)
	p = append(p, b32(duration)...)
	p = append(p, make([]byte, 80)...)
	return box("mvhd", p)
}

// mp4File builds a one video track file: 300 quadros em 10 segundos, 3840×2160.
func mp4File(matrix []byte, fourcc string, w, h uint16, frames uint32) []byte {
	stbl := box("stbl", visualStsd(fourcc, w, h), stsz(frames))
	minf := box("minf", stbl)
	mdia := box("mdia", mdhd(600, 6000), hdlr("vide"), minf)
	trak := box("trak", tkhd(w, h, matrix), mdia)
	moov := box("moov", mvhd(600, 6000), trak)
	ftyp := box("ftyp", []byte("isom"), b32(512), []byte("isomavc1"))
	return append(append(ftyp, moov...), box("mdat", make([]byte, 64))...)
}

func TestProbeMP4(t *testing.T) {
	m := probe(t, "clipe.mp4", mp4File(identityMatrix(), "avc1", 3840, 2160, 300))
	if m.Kind != KindVideo || m.Format != "mp4" {
		t.Fatalf("mp4: %+v", m)
	}
	if m.Width != 3840 || m.Height != 2160 {
		t.Fatalf("dimensões: %dx%d", m.Width, m.Height)
	}
	if m.DurationMS != 10000 {
		t.Fatalf("duração: %d", m.DurationMS)
	}
	if m.Codec != "H.264" {
		t.Fatalf("codec: %q", m.Codec)
	}
	if m.FPSMilli != 30000 {
		t.Fatalf("taxa de quadros: %d", m.FPSMilli)
	}
	if ResolutionKey(m.Width, m.Height) != Res4K {
		t.Fatalf("faixa: %q", ResolutionKey(m.Width, m.Height))
	}
	if m.Bitrate <= 0 {
		t.Fatal("taxa de bits não calculada")
	}
}

func TestProbeMP4RotatedSwapsSides(t *testing.T) {
	m := probe(t, "celular.mp4", mp4File(rotate90Matrix(), "hvc1", 1920, 1080, 250))
	if m.Width != 1080 || m.Height != 1920 {
		t.Fatalf("o giro devia trocar os lados: %dx%d", m.Width, m.Height)
	}
	if m.Rotation != 90 || m.Orientation() != Portrait {
		t.Fatalf("rotação: %d %q", m.Rotation, m.Orientation())
	}
	if m.Codec != "H.265" {
		t.Fatalf("codec: %q", m.Codec)
	}
	if ResolutionKey(m.Width, m.Height) != Res1080 {
		t.Fatalf("faixa: %q", ResolutionKey(m.Width, m.Height))
	}
}

func TestProbeMOVBrand(t *testing.T) {
	f := mp4File(identityMatrix(), "apcn", 1920, 1080, 120)
	copy(f[8:12], "qt  ")
	m := probe(t, "captura.mov", f)
	if m.Format != "mov" || m.Codec != "ProRes" {
		t.Fatalf("mov: %+v", m)
	}
}

func TestProbeHEIC(t *testing.T) {
	ispeSmall := box("ispe", append([]byte{0, 0, 0, 0}, append(b32(320), b32(240)...)...))
	ispeBig := box("ispe", append([]byte{0, 0, 0, 0}, append(b32(4032), b32(3024)...)...))
	irot := box("irot", []byte{1}) // um quarto de volta anti-horário
	ipco := box("ipco", ispeSmall, ispeBig, irot)
	iprp := box("iprp", ipco)
	meta := box("meta", append([]byte{0, 0, 0, 0}, iprp...))
	ftyp := box("ftyp", []byte("heic"), b32(0), []byte("mif1heic"))
	m := probe(t, "IMG_0001.heic", append(ftyp, meta...))
	if m.Format != "heic" || m.Kind != KindImage {
		t.Fatalf("heic: %+v", m)
	}
	if m.Width != 3024 || m.Height != 4032 {
		t.Fatalf("o maior ispe com o giro: %dx%d", m.Width, m.Height)
	}
}

func TestISOTruncatedBoxDoesNotPanic(t *testing.T) {
	f := mp4File(identityMatrix(), "avc1", 1920, 1080, 100)
	for _, cut := range []int{1, 9, 17, 40, len(f) / 3, len(f) / 2, len(f) - 1} {
		if cut <= 0 || cut >= len(f) {
			continue
		}
		_, _ = Probe(bytes.NewReader(f[:cut]), "x.mp4", int64(cut))
	}
}

func TestISOLyingBoxSizeIsRefused(t *testing.T) {
	f := mp4File(identityMatrix(), "avc1", 1920, 1080, 100)
	// moov diz ser bem maior do que o arquivo: a travessia tem de parar, não alocar.
	off := bytes.Index(f, []byte("moov"))
	if off < 4 {
		t.Fatal("moov não encontrado")
	}
	binary.BigEndian.PutUint32(f[off-4:], 0x7fffffff)
	if err := probeErr(t, "x.mp4", f); err != ErrUnsupported {
		t.Fatalf("esperava ErrUnsupported, veio %v", err)
	}
}

// ---- Matroska ----

func vint(v uint64) []byte {
	// Um tamanho EBML de quatro bytes serve para tudo que estes testes montam.
	b := binary.BigEndian.AppendUint32(nil, uint32(v))
	b[0] |= 0x10
	return b
}

func elem(id uint32, body []byte) []byte {
	var head []byte
	switch {
	case id > 0xffffff:
		head = binary.BigEndian.AppendUint32(nil, id)
	case id > 0xffff:
		head = []byte{byte(id >> 16), byte(id >> 8), byte(id)}
	case id > 0xff:
		head = []byte{byte(id >> 8), byte(id)}
	default:
		head = []byte{byte(id)}
	}
	return append(append(head, vint(uint64(len(body)))...), body...)
}

func euint(v uint64) []byte {
	b := binary.BigEndian.AppendUint64(nil, v)
	i := 0
	for i < 7 && b[i] == 0 {
		i++
	}
	return b[i:]
}

func efloat(v float64) []byte {
	return binary.BigEndian.AppendUint64(nil, math.Float64bits(v))
}

func TestProbeMatroska(t *testing.T) {
	head := elem(idEBML, elem(idDocType, []byte("matroska")))
	info := elem(idInfo, append(elem(idTimecodeScale, euint(1_000_000)), elem(idDuration, efloat(12_000))...))
	video := elem(idVideo, append(elem(idPixelWidth, euint(2560)), elem(idPixelHeight, euint(1440))...))
	entry := elem(idTrackEntry, bytes.Join([][]byte{
		elem(idTrackType, euint(1)),
		elem(idCodecID, []byte("V_MPEGH/ISO/HEVC")),
		elem(idDefaultDur, euint(40_000_000)), // 40 ms por quadro = 25 q/s
		video,
	}, nil))
	tracks := elem(idTracks, entry)
	seg := elem(idSegment, append(info, tracks...))
	m := probe(t, "filme.mkv", append(head, seg...))
	if m.Format != "matroska" || m.Kind != KindVideo {
		t.Fatalf("mkv: %+v", m)
	}
	if m.Width != 2560 || m.Height != 1440 {
		t.Fatalf("dimensões: %dx%d", m.Width, m.Height)
	}
	if m.DurationMS != 12000 {
		t.Fatalf("duração: %d", m.DurationMS)
	}
	if m.Codec != "H.265" || m.FPSMilli != 25000 {
		t.Fatalf("codec/taxa: %q %d", m.Codec, m.FPSMilli)
	}
	if ResolutionKey(m.Width, m.Height) != Res2K {
		t.Fatalf("faixa: %q", ResolutionKey(m.Width, m.Height))
	}
}

func TestProbeWebMDocType(t *testing.T) {
	head := elem(idEBML, elem(idDocType, []byte("webm")))
	video := elem(idVideo, append(elem(idPixelWidth, euint(1280)), elem(idPixelHeight, euint(720))...))
	entry := elem(idTrackEntry, append(append(elem(idTrackType, euint(1)), elem(idCodecID, []byte("V_VP9"))...), video...))
	seg := elem(idSegment, elem(idTracks, entry))
	m := probe(t, "clipe.webm", append(head, seg...))
	if m.Format != "webm" || m.Codec != "VP9" {
		t.Fatalf("webm: %+v", m)
	}
}

func TestEBMLWithoutHeaderIsRefused(t *testing.T) {
	if err := probeErr(t, "x.mkv", []byte("not matroska at all")); err != ErrUnsupported {
		t.Fatalf("esperava ErrUnsupported, veio %v", err)
	}
}

// ---- RIFF ----

func chunk(id string, body []byte) []byte {
	out := append([]byte(id), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...)
	out = append(out, body...)
	if len(body)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

func list(kind string, parts ...[]byte) []byte {
	body := append([]byte(kind), bytes.Join(parts, nil)...)
	return chunk("LIST", body)
}

func l32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
func l16(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }

func TestProbeAVI(t *testing.T) {
	avih := make([]byte, 56)
	copy(avih[0:], l32(33_333)) // microssegundos por quadro ≈ 30 q/s
	copy(avih[16:], l32(900))   // 900 quadros = 30 s
	copy(avih[32:], l32(1920))
	copy(avih[36:], l32(1080))
	strh := make([]byte, 56)
	copy(strh[0:], []byte("vids"))
	copy(strh[20:], l32(1))
	copy(strh[24:], l32(30))
	copy(strh[32:], l32(900))
	strf := make([]byte, 40)
	copy(strf[0:], l32(40))
	copy(strf[4:], l32(1920))
	copy(strf[8:], l32(1080))
	copy(strf[16:], []byte("H264"))
	hdrl := list("hdrl", chunk("avih", avih), list("strl", chunk("strh", strh), chunk("strf", strf)))
	body := append([]byte("AVI "), hdrl...)
	body = append(body, chunk("movi", make([]byte, 128))...)
	f := chunk("RIFF", body)

	m := probe(t, "captura.avi", f)
	if m.Format != "avi" || m.Kind != KindVideo {
		t.Fatalf("avi: %+v", m)
	}
	if m.Width != 1920 || m.Height != 1080 {
		t.Fatalf("dimensões: %dx%d", m.Width, m.Height)
	}
	if m.DurationMS != 29999 && m.DurationMS != 30000 {
		t.Fatalf("duração: %d", m.DurationMS)
	}
	if m.Codec != "H.264" {
		t.Fatalf("codec: %q", m.Codec)
	}
}

func TestProbeWAV(t *testing.T) {
	fmtc := make([]byte, 16)
	copy(fmtc[0:], l16(1))
	copy(fmtc[2:], l16(2))
	copy(fmtc[4:], l32(48000))
	copy(fmtc[8:], l32(48000*2*2))
	copy(fmtc[14:], l16(16))
	body := append([]byte("WAVE"), chunk("fmt ", fmtc)...)
	body = append(body, chunk("data", make([]byte, 48000*2*2))...) // um segundo
	m := probe(t, "trilha.wav", chunk("RIFF", body))
	if m.Kind != KindAudio || m.Format != "wav" {
		t.Fatalf("wav: %+v", m)
	}
	if m.DurationMS != 1000 || m.Channels != 2 || m.SampleRate != 48000 || m.BitDepth != 16 {
		t.Fatalf("wav: %+v", m)
	}
}

// ---- extensões e limites ----

func TestSupportedAndVideoExt(t *testing.T) {
	for _, n := range []string{"a.MP4", "b.jpeg", "c.CR2", "d.mkv", "e.wav", "f.heic"} {
		if !Supported(n) {
			t.Fatalf("%s devia ter leitor", n)
		}
	}
	for _, n := range []string{"a.txt", "b.zip", "c.mts", "d.braw", "e"} {
		if Supported(n) {
			t.Fatalf("%s não devia ter leitor", n)
		}
	}
	if !IsVideoExt("x.mov") || !IsVideoExt("x.webm") || IsVideoExt("x.m4a") || IsVideoExt("x.heic") {
		t.Fatal("IsVideoExt classificou errado")
	}
}

func TestEmptyAndGarbage(t *testing.T) {
	if err := probeErr(t, "x.mp4", nil); err != ErrUnsupported {
		t.Fatalf("vazio: %v", err)
	}
	if err := probeErr(t, "x.jpg", bytes.Repeat([]byte{0xff}, 4096)); err != ErrUnsupported {
		t.Fatalf("lixo: %v", err)
	}
	if err := probeErr(t, "x.txt", []byte("oi")); err != ErrUnsupported {
		t.Fatalf("sem leitor: %v", err)
	}
}

func TestAudioByExtension(t *testing.T) {
	m := probe(t, "trilha.mp3", []byte("ID3\x04\x00\x00\x00\x00\x00\x00"))
	if m.Kind != KindAudio || m.Format != "mp3" {
		t.Fatalf("mp3: %+v", m)
	}
}
