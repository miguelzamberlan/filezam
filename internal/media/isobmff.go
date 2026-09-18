// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package media

import (
	"strings"
)

// Leitor do ISO base media file format: MP4, MOV, M4A, 3GP e também HEIC, AVIF e CR3,
// que são o mesmo contêiner com outra carga.
//
// A travessia tem orçamento: no máximo maxBoxes caixas visitadas e maxBoxDepth níveis.
// O tamanho declarado de cada caixa é conferido contra o que resta do arquivo antes de
// virar deslocamento, então um campo adulterado para de andar em vez de girar em falso.
const (
	maxBoxes    = 8192
	maxBoxDepth = 12
	// isoEpoch é a diferença entre 1904-01-01 e 1970-01-01, em segundos: o contêiner conta
	// o tempo a partir de 1904.
	isoEpoch = 2082844800
)

type isoTrack struct {
	kind      string // vide | soun
	timescale uint32
	duration  uint64
	// width/height são as dimensões codificadas, lidas do stsd: é o número que um
	// reprodutor mostra. O tkhd traz as de exibição (dispW/dispH), que num vídeo
	// anamórfico são outras — ficam de reserva, para quando o stsd não disser nada.
	width      int
	height     int
	dispW      int
	dispH      int
	rotation   int
	codec      string
	samples    int64
	channels   int
	sampleRate int
	bitDepth   int
}

// durMS is the track duration in milliseconds.
func (t isoTrack) durMS() int64 {
	if t.timescale == 0 || t.duration == 0 {
		return 0
	}
	return int64(t.duration) * 1000 / int64(t.timescale)
}

type isoState struct {
	s      section
	boxes  int
	qt     bool // marca "qt  ": no QuickTime a caixa meta não é FullBox
	brand  string
	scale  uint32
	dur    uint64
	create int64

	video *isoTrack
	audio *isoTrack

	ispeW, ispeH int
	irot         int
}

// eachBox iterates the boxes between off and end, calling fn with the payload bounds.
func (st *isoState) eachBox(off, end int64, depth int, fn func(typ string, boff, bend int64)) {
	if depth > maxBoxDepth {
		return
	}
	for off+8 <= end {
		if st.boxes >= maxBoxes {
			return
		}
		st.boxes++
		hdr, err := st.s.exact(off, 8)
		if err != nil {
			return
		}
		size := int64(be32(hdr))
		typ := string(hdr[4:8])
		head := int64(8)
		switch size {
		case 1:
			ext, err := st.s.exact(off+8, 8)
			if err != nil {
				return
			}
			v := be64(ext)
			// Um tamanho de 64 bits acima do que o arquivo tem é adulteração, não um arquivo grande.
			if v > uint64(end-off) {
				return
			}
			size = int64(v)
			head = 16
		case 0:
			size = end - off // "até o fim do pai", só válido na última caixa
		}
		if size < head || off+size > end {
			return
		}
		fn(typ, off+head, off+size)
		off += size
	}
}

func probeISO(s section, ext string) (Meta, error) {
	st := &isoState{s: s}
	st.eachBox(0, s.size, 0, func(typ string, boff, bend int64) {
		switch typ {
		case "ftyp":
			if b, err := s.exact(boff, 8); err == nil {
				st.brand = string(b[:4])
				st.qt = st.brand == "qt  "
			}
		case "moov":
			st.parseMoov(boff, bend, 1)
		case "meta":
			// HEIC e AVIF guardam o tamanho da imagem nas propriedades do item, não numa trilha.
			st.parseMeta(boff, bend, 1)
		}
	})
	m := Meta{Format: isoFormat(st.brand, ext)}
	switch m.Format {
	case "heic", "avif", "cr3":
		m.Kind = KindImage
		m.Width, m.Height = st.ispeW, st.ispeH
		applyRotation(&m, st.irot)
		if st.create > 0 {
			m.TakenAt = st.create * 1000
		}
		if m.Width <= 0 && m.Format == "cr3" {
			// CR3 guarda o quadro num fluxo proprietário: sobra o contêiner, sem dimensões.
			return m, nil
		}
		if m.Width <= 0 || m.Height <= 0 {
			return Meta{}, ErrUnsupported
		}
		return m, nil
	}

	dur := int64(0)
	if st.scale > 0 && st.dur > 0 {
		dur = int64(st.dur) * 1000 / int64(st.scale)
	}
	switch {
	case st.video != nil:
		m.Kind = KindVideo
		m.Width, m.Height = st.video.width, st.video.height
		m.Codec = st.video.codec
		if d := st.video.durMS(); d > dur {
			dur = d
		}
		m.FPSMilli = fpsMilli(st.video.samples, st.video.durMS())
		applyRotation(&m, st.video.rotation)
	case st.audio != nil:
		m.Kind = KindAudio
		if d := st.audio.durMS(); d > dur {
			dur = d
		}
	default:
		return Meta{}, ErrUnsupported
	}
	if st.audio != nil {
		m.AudioCodec = st.audio.codec
		m.Channels = st.audio.channels
		m.SampleRate = st.audio.sampleRate
		if m.Kind == KindAudio {
			m.BitDepth = st.audio.bitDepth
		}
	}
	m.DurationMS = dur
	m.Bitrate = bitrate(s.size, dur)
	if st.create > 0 {
		m.TakenAt = st.create * 1000
	}
	return m, nil
}

// isoFormat names the flavour from the ftyp brand, falling back to the extension.
func isoFormat(brand, ext string) string {
	b := strings.TrimSpace(strings.ToLower(brand))
	switch b {
	case "qt":
		return "mov"
	case "crx":
		return "cr3"
	case "m4a":
		return "m4a"
	case "m4v":
		return "m4v"
	case "avif", "avis":
		return "avif"
	case "heic", "heix", "hevc", "hevx", "heim", "heis", "hevm", "hevs", "mif1", "msf1":
		return "heic"
	}
	if strings.HasPrefix(b, "3g") {
		return "3gp"
	}
	switch ext {
	case ".mov", ".qt":
		return "mov"
	case ".heic", ".heif", ".hif":
		return "heic"
	case ".avif":
		return "avif"
	case ".cr3":
		return "cr3"
	case ".m4a":
		return "m4a"
	case ".m4v":
		return "m4v"
	case ".3gp", ".3g2":
		return "3gp"
	}
	return "mp4"
}

func (st *isoState) parseMoov(off, end int64, depth int) {
	st.eachBox(off, end, depth, func(typ string, boff, bend int64) {
		switch typ {
		case "mvhd":
			st.parseMvhd(boff, bend)
		case "trak":
			tr := &isoTrack{}
			st.parseTrak(tr, boff, bend, depth+1)
			st.keepTrack(tr)
		}
	})
}

// keepTrack remembers the best video and audio track: a de maior área, depois a mais longa.
func (st *isoState) keepTrack(tr *isoTrack) {
	switch tr.kind {
	case "vide":
		if tr.width <= 0 || tr.height <= 0 {
			tr.width, tr.height = tr.dispW, tr.dispH
		}
		if tr.width <= 0 || tr.height <= 0 {
			return
		}
		cur := st.video
		if cur == nil || int64(tr.width)*int64(tr.height) > int64(cur.width)*int64(cur.height) {
			st.video = tr
		}
	case "soun":
		if st.audio == nil || tr.durMS() > st.audio.durMS() {
			st.audio = tr
		}
	}
}

func (st *isoState) parseMvhd(off, end int64) {
	b, err := st.s.at(off, 32)
	if err != nil || len(b) < 20 {
		return
	}
	if b[0] == 1 {
		if len(b) < 32 {
			return
		}
		st.create = int64(be64(b[4:])) - isoEpoch
		st.scale = be32(b[20:])
		st.dur = be64(b[24:])
		return
	}
	st.create = int64(be32(b[4:])) - isoEpoch
	st.scale = be32(b[12:])
	st.dur = uint64(be32(b[16:]))
}

func (st *isoState) parseTrak(tr *isoTrack, off, end int64, depth int) {
	st.eachBox(off, end, depth, func(typ string, boff, bend int64) {
		switch typ {
		case "tkhd":
			st.parseTkhd(tr, boff)
		case "mdia":
			st.parseMdia(tr, boff, bend, depth+1)
		}
	})
}

func (st *isoState) parseTkhd(tr *isoTrack, off int64) {
	b, err := st.s.at(off, 96)
	if err != nil || len(b) < 84 {
		return
	}
	base := int64(40)
	if b[0] == 1 {
		if len(b) < 96 {
			return
		}
		base = 52
	}
	tr.rotation = matrixRotation(b[base : base+36])
	tr.dispW = int(be32(b[base+36:]) >> 16)
	tr.dispH = int(be32(b[base+40:]) >> 16)
}

// matrixRotation reads the quarter turn out of the 3×3 display matrix. Só os quatro giros
// retos são reconhecidos; qualquer outra transformação conta como nenhuma.
func matrixRotation(m []byte) int {
	if len(m) < 20 {
		return 0
	}
	const one = 1 << 16
	a, b := int32(be32(m)), int32(be32(m[4:]))
	c, d := int32(be32(m[12:])), int32(be32(m[16:]))
	switch {
	case a == 0 && b == one && c == -one && d == 0:
		return 90
	case a == -one && b == 0 && c == 0 && d == -one:
		return 180
	case a == 0 && b == -one && c == one && d == 0:
		return 270
	}
	return 0
}

func (st *isoState) parseMdia(tr *isoTrack, off, end int64, depth int) {
	st.eachBox(off, end, depth, func(typ string, boff, bend int64) {
		switch typ {
		case "mdhd":
			if b, err := st.s.at(boff, 32); err == nil && len(b) >= 20 {
				if b[0] == 1 {
					if len(b) >= 32 {
						tr.timescale = be32(b[20:])
						tr.duration = be64(b[24:])
					}
				} else {
					tr.timescale = be32(b[12:])
					tr.duration = uint64(be32(b[16:]))
				}
			}
		case "hdlr":
			if b, err := st.s.at(boff, 12); err == nil && len(b) >= 12 {
				tr.kind = string(b[8:12])
			}
		case "minf":
			st.eachBox(boff, bend, depth+1, func(t2 string, o2, e2 int64) {
				if t2 == "stbl" {
					st.parseStbl(tr, o2, e2, depth+2)
				}
			})
		}
	})
}

func (st *isoState) parseStbl(tr *isoTrack, off, end int64, depth int) {
	st.eachBox(off, end, depth, func(typ string, boff, bend int64) {
		switch typ {
		case "stsd":
			st.parseStsd(tr, boff, bend)
		case "stsz":
			// A contagem de amostras da trilha de vídeo é a contagem de quadros, e com a
			// duração dá a taxa: sai de três campos, sem percorrer a tabela inteira.
			if b, err := st.s.at(boff, 12); err == nil && len(b) >= 12 {
				tr.samples = int64(be32(b[8:]))
			}
		case "stco", "co64":
			// nada: as tabelas de deslocamento não dizem nada sobre o formato
		}
	})
}

func (st *isoState) parseStsd(tr *isoTrack, off, end int64) {
	b, err := st.s.at(off, 8)
	if err != nil || len(b) < 8 || be32(b[4:]) == 0 {
		return
	}
	entry := off + 8
	if entry+16 > end {
		return
	}
	e, err := st.s.at(entry, 40)
	if err != nil || len(e) < 16 {
		return
	}
	tr.codec = codecName(string(e[4:8]))
	switch tr.kind {
	case "vide":
		if len(e) >= 36 {
			if w, h := int(be16(e[32:])), int(be16(e[34:])); w > 0 && h > 0 {
				tr.width, tr.height = w, h
			}
		}
	case "soun":
		if len(e) >= 36 {
			tr.channels = int(be16(e[24:]))
			tr.bitDepth = int(be16(e[26:]))
			tr.sampleRate = int(be32(e[32:]) >> 16)
		}
	}
}

// codecName turns a four character code into the name people use, keeping the raw code
// when it is not one we know.
func codecName(fourcc string) string {
	switch strings.ToLower(strings.TrimSpace(fourcc)) {
	case "avc1", "avc3", "h264":
		return "H.264"
	case "hvc1", "hev1", "hvc2", "dvh1", "dvhe":
		return "H.265"
	case "av01":
		return "AV1"
	case "vp09":
		return "VP9"
	case "vp08":
		return "VP8"
	case "mp4v":
		return "MPEG-4"
	case "mjpg", "jpeg":
		return "MJPEG"
	case "apch", "apcn", "apcs", "apco", "ap4h", "ap4x":
		return "ProRes"
	case "dvcp", "dvc", "dv5p", "dv5n":
		return "DV"
	case "mp4a":
		return "AAC"
	case "alac":
		return "ALAC"
	case "opus":
		return "Opus"
	case ".mp3", "mp3 ":
		return "MP3"
	case "ac-3":
		return "AC-3"
	case "ec-3":
		return "E-AC-3"
	case "twos", "sowt", "lpcm", "in24", "in32", "fl32":
		return "PCM"
	}
	return printable(fourcc, 16)
}

// parseMeta descends the item property boxes of a HEIC or AVIF file.
func (st *isoState) parseMeta(off, end int64, depth int) {
	// No ISO a caixa meta é FullBox e começa com quatro bytes de versão e sinalizadores;
	// no QuickTime não. Pular os quatro bytes no arquivo errado desalinharia a travessia,
	// então quem decide é a marca do ftyp.
	if !st.qt {
		off += 4
	}
	st.eachBox(off, end, depth, func(typ string, boff, bend int64) {
		if typ == "iprp" {
			st.eachBox(boff, bend, depth+1, func(t2 string, o2, e2 int64) {
				if t2 == "ipco" {
					st.parseIpco(o2, e2, depth+2)
				}
			})
		}
	})
}

func (st *isoState) parseIpco(off, end int64, depth int) {
	st.eachBox(off, end, depth, func(typ string, boff, bend int64) {
		switch typ {
		case "ispe":
			// Miniatura e quadro cheio têm cada um o seu ispe: fica o de maior área.
			if b, err := st.s.at(boff, 12); err == nil && len(b) >= 12 {
				w, h := int(be32(b[4:])), int(be32(b[8:]))
				if w > 0 && h > 0 && int64(w)*int64(h) > int64(st.ispeW)*int64(st.ispeH) {
					st.ispeW, st.ispeH = w, h
				}
			}
		case "irot":
			if b, err := st.s.at(boff, 1); err == nil && len(b) == 1 && st.irot == 0 {
				// O campo conta quartos de volta no sentido anti-horário.
				st.irot = (360 - int(b[0]&0x3)*90) % 360
			}
		}
	})
}
