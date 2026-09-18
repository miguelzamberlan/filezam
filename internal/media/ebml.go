// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package media

import (
	"math"
	"strings"
)

// Leitor de EBML: Matroska (.mkv, .mka) e WebM.
//
// Só o começo do segmento interessa — cabeçalho, Info e Tracks —, então a varredura para
// no primeiro Cluster, no teto de elementos ou depois de ebmlMaxScan bytes. Um arquivo que
// esconda Tracks depois de horas de vídeo fica sem detalhe, e isso é melhor do que ler o
// arquivo inteiro para preencher uma tabela.
const (
	ebmlMaxElems = 4096
	ebmlMaxDepth = 8
	ebmlMaxScan  = 32 << 20
	// ebmlEpoch converte o relógio do Matroska (nanossegundos desde 2001-01-01) para unix ms.
	ebmlEpoch = 978307200000
)

const (
	idEBML          = 0x1a45dfa3
	idDocType       = 0x4282
	idSegment       = 0x18538067
	idInfo          = 0x1549a966
	idTimecodeScale = 0x2ad7b1
	idDuration      = 0x4489
	idDateUTC       = 0x4461
	idTracks        = 0x1654ae6b
	idTrackEntry    = 0xae
	idTrackType     = 0x83
	idCodecID       = 0x86
	idDefaultDur    = 0x23e383
	idVideo         = 0xe0
	idPixelWidth    = 0xb0
	idPixelHeight   = 0xba
	idAudio         = 0xe1
	idSampleFreq    = 0xb5
	idChannels      = 0x9f
	idBitDepth      = 0x6264
	idCluster       = 0x1f43b675
)

type ebmlTrack struct {
	typ        int
	codec      string
	width      int
	height     int
	defaultDur int64 // nanossegundos por quadro
	channels   int
	sampleRate int
	bitDepth   int
}

type ebmlState struct {
	s     section
	elems int
	stop  bool
	// limit é até onde a varredura anda. O tamanho declarado continua sendo conferido
	// contra o arquivo inteiro: um segmento de meio gigabyte é legítimo, e recusá-lo por
	// causa da janela de leitura deixaria todo vídeo grande sem detalhe.
	limit int64

	docType  string
	scale    int64
	duration float64
	date     int64
	video    *ebmlTrack
	audio    *ebmlTrack
}

func probeEBML(s section, ext string) (Meta, error) {
	st := &ebmlState{s: s, scale: 1_000_000, limit: min(s.size, ebmlMaxScan)}
	header := false
	st.each(0, s.size, 0, func(id uint32, off, e int64) {
		switch id {
		case idEBML:
			header = true
			st.each(off, e, 1, func(id2 uint32, o2, e2 int64) {
				if id2 == idDocType {
					st.docType = printable(st.str(o2, e2), 32)
				}
			})
		case idSegment:
			st.parseSegment(off, e, 1)
		}
	})
	if !header {
		return Meta{}, ErrUnsupported
	}
	format := "matroska"
	if strings.EqualFold(st.docType, "webm") {
		format = "webm"
	}
	m := Meta{Format: format}
	dur := int64(0)
	if st.duration > 0 && st.scale > 0 {
		// Duration vem em unidades de TimecodeScale, que é dado em nanossegundos.
		ms := st.duration * float64(st.scale) / 1e6
		if ms > 0 && ms < 1e12 {
			dur = int64(ms)
		}
	}
	switch {
	case st.video != nil:
		m.Kind = KindVideo
		m.Width, m.Height = st.video.width, st.video.height
		m.Codec = st.video.codec
		// DefaultDuration é o tempo de um quadro em nanossegundos: mil quadros por segundo
		// vezes mil dá 1e12 dividido por ele.
		if d := st.video.defaultDur; d > 0 {
			if v := 1e12 / d; v > 0 && v <= 1_000_000 {
				m.FPSMilli = int(v)
			}
		}
	case st.audio != nil:
		m.Kind = KindAudio
		m.BitDepth = st.audio.bitDepth
	default:
		return Meta{}, ErrUnsupported
	}
	if st.audio != nil {
		m.AudioCodec = st.audio.codec
		m.Channels = st.audio.channels
		m.SampleRate = st.audio.sampleRate
	}
	m.DurationMS = dur
	m.Bitrate = bitrate(s.size, dur)
	if st.date != 0 {
		m.TakenAt = st.date
	}
	return m, nil
}

func (st *ebmlState) parseSegment(off, end int64, depth int) {
	st.each(off, end, depth, func(id uint32, o, e int64) {
		switch id {
		case idInfo:
			st.parseInfo(o, e, depth+1)
		case idTracks:
			st.parseTracks(o, e, depth+1)
		case idCluster:
			// Daqui para frente são só quadros: o que interessa já passou (ou não estava lá).
			st.stop = true
		}
	})
}

func (st *ebmlState) parseInfo(off, end int64, depth int) {
	st.each(off, end, depth, func(id uint32, o, e int64) {
		switch id {
		case idTimecodeScale:
			if v, ok := st.uint(o, e); ok && v > 0 {
				st.scale = int64(v)
			}
		case idDuration:
			if v, ok := st.float(o, e); ok {
				st.duration = v
			}
		case idDateUTC:
			if v, ok := st.int(o, e); ok {
				st.date = v/1e6 + ebmlEpoch
			}
		}
	})
}

func (st *ebmlState) parseTracks(off, end int64, depth int) {
	st.each(off, end, depth, func(id uint32, o, e int64) {
		if id != idTrackEntry {
			return
		}
		tr := &ebmlTrack{}
		st.each(o, e, depth+1, func(id2 uint32, o2, e2 int64) {
			switch id2 {
			case idTrackType:
				if v, ok := st.uint(o2, e2); ok {
					tr.typ = int(v)
				}
			case idCodecID:
				tr.codec = ebmlCodec(st.str(o2, e2))
			case idDefaultDur:
				if v, ok := st.uint(o2, e2); ok {
					tr.defaultDur = int64(v)
				}
			case idVideo:
				st.each(o2, e2, depth+2, func(id3 uint32, o3, e3 int64) {
					switch id3 {
					case idPixelWidth:
						if v, ok := st.uint(o3, e3); ok {
							tr.width = int(v)
						}
					case idPixelHeight:
						if v, ok := st.uint(o3, e3); ok {
							tr.height = int(v)
						}
					}
				})
			case idAudio:
				st.each(o2, e2, depth+2, func(id3 uint32, o3, e3 int64) {
					switch id3 {
					case idSampleFreq:
						if v, ok := st.float(o3, e3); ok && v > 0 && v < 1e9 {
							tr.sampleRate = int(v)
						}
					case idChannels:
						if v, ok := st.uint(o3, e3); ok {
							tr.channels = int(v)
						}
					case idBitDepth:
						if v, ok := st.uint(o3, e3); ok {
							tr.bitDepth = int(v)
						}
					}
				})
			}
		})
		switch tr.typ {
		case 1: // vídeo
			if tr.width > 0 && tr.height > 0 {
				cur := st.video
				if cur == nil || int64(tr.width)*int64(tr.height) > int64(cur.width)*int64(cur.height) {
					st.video = tr
				}
			}
		case 2: // áudio
			if st.audio == nil {
				st.audio = tr
			}
		}
	})
}

// each iterates the elements between off and end. Um elemento de tamanho desconhecido
// (o Segment quase sempre é) vale até o fim do pai.
func (st *ebmlState) each(off, end int64, depth int, fn func(id uint32, off, end int64)) {
	if depth > ebmlMaxDepth {
		return
	}
	for off < end && !st.stop {
		if st.elems >= ebmlMaxElems || off > st.limit {
			return
		}
		st.elems++
		id, idLen, ok := st.readID(off)
		if !ok {
			return
		}
		size, szLen, unknown, ok := st.readSize(off + idLen)
		if !ok {
			return
		}
		body := off + idLen + szLen
		stop := end
		if !unknown {
			// O tamanho é conferido contra o arquivo, não contra o pai: o Segment costuma ser
			// maior do que a janela de varredura, e recusá-lo por isso deixaria todo vídeo
			// grande sem detalhe. Quem limita a descida são o orçamento e st.limit.
			if size < 0 || body+size > st.s.size {
				return
			}
			stop = body + size
		}
		fn(id, body, stop)
		if unknown {
			return // sem tamanho não dá para saber onde o próximo começa
		}
		off = stop
	}
}

// readID reads an EBML element id, marker bit included.
func (st *ebmlState) readID(off int64) (uint32, int64, bool) {
	b, err := st.s.exact(off, 1)
	if err != nil {
		return 0, 0, false
	}
	n := leadingLen(b[0])
	if n == 0 || n > 4 {
		return 0, 0, false
	}
	buf, err := st.s.exact(off, n)
	if err != nil {
		return 0, 0, false
	}
	var v uint32
	for _, c := range buf {
		v = v<<8 | uint32(c)
	}
	return v, int64(n), true
}

// readSize reads an EBML data size, stripping the marker bit. Todos os bits em um
// significam "tamanho desconhecido".
func (st *ebmlState) readSize(off int64) (size, n int64, unknown, ok bool) {
	b, err := st.s.exact(off, 1)
	if err != nil {
		return 0, 0, false, false
	}
	l := leadingLen(b[0])
	if l == 0 || l > 8 {
		return 0, 0, false, false
	}
	buf, err := st.s.exact(off, l)
	if err != nil {
		return 0, 0, false, false
	}
	v := uint64(buf[0]) & (1<<(8-uint(l)) - 1)
	all := v == 1<<(8-uint(l))-1
	for _, c := range buf[1:] {
		v = v<<8 | uint64(c)
		all = all && c == 0xff
	}
	if all {
		return 0, int64(l), true, true
	}
	if v > uint64(st.s.size) {
		return 0, 0, false, false
	}
	return int64(v), int64(l), false, true
}

// leadingLen counts how many bytes the variable-length integer takes, from its first byte.
func leadingLen(b byte) int {
	for i := 0; i < 8; i++ {
		if b&(0x80>>uint(i)) != 0 {
			return i + 1
		}
	}
	return 0
}

func (st *ebmlState) raw(off, end int64) []byte {
	n := end - off
	if n <= 0 || n > 64 {
		return nil
	}
	b, err := st.s.exact(off, int(n))
	if err != nil {
		return nil
	}
	return b
}

func (st *ebmlState) uint(off, end int64) (uint64, bool) {
	b := st.raw(off, end)
	if len(b) == 0 || len(b) > 8 {
		return 0, false
	}
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v, true
}

func (st *ebmlState) int(off, end int64) (int64, bool) {
	b := st.raw(off, end)
	if len(b) == 0 || len(b) > 8 {
		return 0, false
	}
	v := int64(0)
	if b[0]&0x80 != 0 {
		v = -1
	}
	for _, c := range b {
		v = v<<8 | int64(c)
	}
	return v, true
}

func (st *ebmlState) float(off, end int64) (float64, bool) {
	b := st.raw(off, end)
	switch len(b) {
	case 4:
		v := float64(math.Float32frombits(be32(b)))
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return v, true
	case 8:
		v := math.Float64frombits(be64(b))
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

func (st *ebmlState) str(off, end int64) string {
	return strings.TrimRight(string(st.raw(off, end)), "\x00")
}

// ebmlCodec maps a Matroska codec id to the usual name.
func ebmlCodec(id string) string {
	u := strings.ToUpper(strings.TrimSpace(id))
	switch {
	case u == "V_MPEG4/ISO/AVC":
		return "H.264"
	case u == "V_MPEGH/ISO/HEVC":
		return "H.265"
	case u == "V_AV1":
		return "AV1"
	case u == "V_VP9":
		return "VP9"
	case u == "V_VP8":
		return "VP8"
	case u == "V_MPEG2":
		return "MPEG-2"
	case u == "V_PRORES":
		return "ProRes"
	case strings.HasPrefix(u, "V_MPEG4"):
		return "MPEG-4"
	case strings.HasPrefix(u, "A_AAC"):
		return "AAC"
	case u == "A_OPUS":
		return "Opus"
	case u == "A_VORBIS":
		return "Vorbis"
	case u == "A_AC3":
		return "AC-3"
	case u == "A_EAC3":
		return "E-AC-3"
	case u == "A_FLAC":
		return "FLAC"
	case u == "A_TRUEHD":
		return "TrueHD"
	case strings.HasPrefix(u, "A_DTS"):
		return "DTS"
	case strings.HasPrefix(u, "A_MPEG/L3"):
		return "MP3"
	case strings.HasPrefix(u, "A_PCM"):
		return "PCM"
	}
	return printable(id, 32)
}
