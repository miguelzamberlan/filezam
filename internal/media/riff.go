// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package media

// Leitor de RIFF: AVI e WAV. Os dois são a mesma estrutura de pedaços, tudo em
// little-endian, com os cabeçalhos no começo do arquivo.
const (
	riffMaxChunks = 2048
	riffMaxDepth  = 4
)

type riffState struct {
	s      section
	chunks int

	// AVI
	microPerFrame int64
	totalFrames   int64
	width, height int
	vcodec        string
	acodec        string
	channels      int
	sampleRate    int
	bitDepth      int
	streamDurMS   int64

	// WAV
	avgBytesPerSec int64
	dataBytes      int64

	// curStream é o tipo do último strh lido: é ele que diz se o strf seguinte é um
	// cabeçalho de imagem ou de som, porque os dois podem ter o mesmo tamanho.
	curStream string
}

func probeRIFF(s section, ext string) (Meta, error) {
	hdr, err := s.exact(0, 12)
	if err != nil || string(hdr[:4]) != "RIFF" {
		return Meta{}, ErrUnsupported
	}
	form := string(hdr[8:12])
	// O tamanho anunciado pelo RIFF não manda em nada: vale o que o arquivo tem mesmo.
	end := s.size
	st := &riffState{s: s}
	st.each(12, end, 0, func(id string, off, e int64) {
		switch id {
		case "LIST":
			st.parseList(off, e, 1)
		case "fmt ":
			st.parseWaveFmt(off, e)
		case "data":
			st.dataBytes = e - off
		}
	})
	switch form {
	case "AVI ":
		return st.avi(s.size)
	case "WAVE":
		return st.wav()
	}
	return Meta{}, ErrUnsupported
}

func (st *riffState) avi(size int64) (Meta, error) {
	m := Meta{Kind: KindVideo, Format: "avi", Width: st.width, Height: st.height, Codec: st.vcodec}
	dur := st.totalFrames * st.microPerFrame / 1000
	if dur <= 0 {
		dur = st.streamDurMS
	}
	if dur < 0 || dur > 1e12 {
		dur = 0
	}
	m.DurationMS = dur
	if st.microPerFrame > 0 {
		if v := 1_000_000_000 / st.microPerFrame; v > 0 && v <= 1_000_000 {
			m.FPSMilli = int(v)
		}
	}
	m.AudioCodec = st.acodec
	m.Channels = st.channels
	m.SampleRate = st.sampleRate
	m.Bitrate = bitrate(size, dur)
	if m.Width <= 0 || m.Height <= 0 {
		return Meta{}, ErrUnsupported
	}
	return m, nil
}

func (st *riffState) wav() (Meta, error) {
	m := Meta{Kind: KindAudio, Format: "wav", Channels: st.channels, SampleRate: st.sampleRate, BitDepth: st.bitDepth}
	m.AudioCodec = st.acodec
	if st.avgBytesPerSec > 0 && st.dataBytes > 0 {
		m.DurationMS = st.dataBytes * 1000 / st.avgBytesPerSec
		m.Bitrate = st.avgBytesPerSec * 8
	}
	if m.SampleRate <= 0 {
		return Meta{}, ErrUnsupported
	}
	return m, nil
}

// each iterates RIFF chunks. Cada pedaço é preenchido para tamanho par, e o tamanho
// declarado é conferido contra o que resta antes de virar deslocamento.
func (st *riffState) each(off, end int64, depth int, fn func(id string, off, end int64)) {
	if depth > riffMaxDepth {
		return
	}
	for off+8 <= end {
		if st.chunks >= riffMaxChunks {
			return
		}
		st.chunks++
		hdr, err := st.s.exact(off, 8)
		if err != nil {
			return
		}
		id := string(hdr[:4])
		size := int64(le32(hdr[4:]))
		if size < 0 || off+8+size > end {
			return
		}
		fn(id, off+8, off+8+size)
		off += 8 + size
		if size%2 == 1 {
			off++
		}
	}
}

func (st *riffState) parseList(off, end int64, depth int) {
	b, err := st.s.exact(off, 4)
	if err != nil {
		return
	}
	switch string(b) {
	case "hdrl", "strl":
		st.each(off+4, end, depth, func(id string, o, e int64) {
			switch id {
			case "avih":
				st.parseAvih(o, e)
			case "strh":
				st.parseStrh(o, e)
			case "strf":
				st.parseStrf(o, e)
			case "LIST":
				st.parseList(o, e, depth+1)
			}
		})
	}
}

func (st *riffState) parseAvih(off, end int64) {
	b, err := st.s.at(off, 40)
	if err != nil || len(b) < 40 {
		return
	}
	st.microPerFrame = int64(le32(b))
	st.totalFrames = int64(le32(b[16:]))
	if w, h := int(le32(b[32:])), int(le32(b[36:])); w > 0 && h > 0 {
		st.width, st.height = w, h
	}
}

// parseStrh reads a stream header. Guarda o tipo do fluxo, que decide como ler o strf
// seguinte, e a duração do fluxo de vídeo, reserva para quando o cabeçalho principal não
// traz a contagem de quadros.
func (st *riffState) parseStrh(off, end int64) {
	b, err := st.s.at(off, 40)
	if err != nil || len(b) < 40 {
		return
	}
	st.curStream = string(b[:4])
	if st.curStream != "vids" {
		return
	}
	scale, rate, length := int64(le32(b[20:])), int64(le32(b[24:])), int64(le32(b[32:]))
	if rate > 0 && scale > 0 && length > 0 {
		st.streamDurMS = length * scale * 1000 / rate
	}
}

// parseStrf reads the format of the stream declared by the strh before it:
// BITMAPINFOHEADER for video, WAVEFORMATEX for audio.
func (st *riffState) parseStrf(off, end int64) {
	switch st.curStream {
	case "vids":
		b, err := st.s.at(off, 40)
		if err != nil || len(b) < 40 || st.vcodec != "" {
			return
		}
		if w, h := int(int32(le32(b[4:]))), int(int32(le32(b[8:]))); w > 0 && st.width <= 0 {
			st.width = w
			if h < 0 {
				h = -h // altura negativa só diz que as linhas vêm de cima para baixo
			}
			st.height = h
		}
		st.vcodec = codecName(string(b[16:20]))
	case "auds":
		st.parseWaveFmt(off, end)
	}
}

func (st *riffState) parseWaveFmt(off, end int64) {
	b, err := st.s.at(off, 16)
	if err != nil || len(b) < 16 {
		return
	}
	st.acodec = waveCodec(le16(b))
	st.channels = int(le16(b[2:]))
	st.sampleRate = int(le32(b[4:]))
	st.avgBytesPerSec = int64(le32(b[8:]))
	st.bitDepth = int(le16(b[14:]))
}

// waveCodec names the few wave format tags that turn up in these files.
func waveCodec(tag uint16) string {
	switch tag {
	case 0x0001, 0xfffe:
		return "PCM"
	case 0x0003:
		return "PCM"
	case 0x0002:
		return "ADPCM"
	case 0x0055:
		return "MP3"
	case 0x0050:
		return "MP2"
	case 0x00ff, 0x1601:
		return "AAC"
	case 0x2000:
		return "AC-3"
	case 0x0161, 0x0162, 0x0163:
		return "WMA"
	case 0xf1ac:
		return "FLAC"
	}
	return ""
}
