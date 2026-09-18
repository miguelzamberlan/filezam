// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

// Package media reads technical metadata from photo and video files: dimensions,
// duration, codec, frame rate and camera data.
//
// Só cabeçalho de contêiner é lido — nenhum pixel é decodificado, nenhum quadro é
// reconstruído — então o pacote não precisa de ffmpeg nem de CGO, e continua valendo
// na imagem distroless. O preço é o que dá para ler assim: resolução, duração, codec,
// taxa de quadros e EXIF, nunca qualidade percebida.
//
// Tudo aqui é conteúdo de terceiros. Nenhum tamanho declarado dentro do arquivo vira
// alocação: toda leitura passa por section.at, que confere o pedido contra o tamanho
// real do arquivo e contra um teto fixo, e toda descida tem limite de profundidade e
// de número de caixas visitadas.
package media

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
)

// ErrUnsupported: a extensão não tem leitor, ou o arquivo não abre como o formato que promete.
var ErrUnsupported = errors.New("unsupported media file")

// Kinds of media. Um arquivo reconhecido sempre cai em um destes.
const (
	KindImage = "image"
	KindVideo = "video"
	KindAudio = "audio"
)

// Orientations, derivadas das dimensões já rotacionadas.
const (
	Portrait  = "portrait"
	Landscape = "landscape"
	Square    = "square"
)

// Meta is what a probe could read. Campos que o formato não traz ficam zerados: o leitor
// devolve o que encontrou em vez de falhar por causa de um campo ausente.
type Meta struct {
	Kind   string `json:"kind"`
	Format string `json:"format"` // jpeg, png, mp4, mov, matroska, avi, heic...

	// Width e Height já vêm com a rotação aplicada: é o tamanho como a imagem ou o vídeo
	// aparece na tela, que é o que interessa para contar retrato e paisagem. Rotation
	// guarda o giro que estava declarado (matriz do tkhd, irot ou orientação EXIF).
	Width    int `json:"width,omitempty"`
	Height   int `json:"height,omitempty"`
	Rotation int `json:"rotation,omitempty"` // 0, 90, 180 ou 270

	DurationMS int64  `json:"durationMs,omitempty"`
	Codec      string `json:"codec,omitempty"`
	AudioCodec string `json:"audioCodec,omitempty"`
	// FPSMilli é a taxa de quadros vezes mil (29970 = 29,97): inteiro para agrupar sem
	// que 29,969999 e 29,970001 virem duas faixas diferentes.
	FPSMilli   int   `json:"fpsMilli,omitempty"`
	Bitrate    int64 `json:"bitrate,omitempty"` // bits por segundo, média sobre o arquivo inteiro
	Channels   int   `json:"channels,omitempty"`
	SampleRate int   `json:"sampleRate,omitempty"`
	BitDepth   int   `json:"bitDepth,omitempty"`

	TakenAt     int64   `json:"takenAt,omitempty"` // unix ms; 0 = o formato não disse
	Camera      string  `json:"camera,omitempty"`
	Lens        string  `json:"lens,omitempty"`
	ISO         int     `json:"iso,omitempty"`
	Exposure    string  `json:"exposure,omitempty"` // "1/125", "2.5"
	FNumber     float64 `json:"fNumber,omitempty"`
	FocalLength float64 `json:"focalLength,omitempty"`
}

// Pixels is width × height, 0 when the size is unknown.
func (m Meta) Pixels() int64 { return int64(m.Width) * int64(m.Height) }

// Orientation classifies the displayed shape; "" when there are no dimensions.
func (m Meta) Orientation() string {
	switch {
	case m.Width <= 0 || m.Height <= 0:
		return ""
	case m.Width > m.Height:
		return Landscape
	case m.Width < m.Height:
		return Portrait
	}
	return Square
}

// family is the reader that handles a given extension.
type family int

const (
	famNone family = iota
	famImage
	famTIFF  // TIFF e os RAW que são TIFF por baixo
	famISO   // ISO base media: MP4, MOV, HEIC, AVIF, CR3
	famEBML  // Matroska e WebM
	famRIFF  // AVI e WAV
	famAudio // áudio reconhecido pela extensão, sem leitor de cabeçalho
)

// families mapeia extensão → leitor. Formatos que exigiriam decodificar fluxo
// (MTS/M2TS, MXF, R3D, BRAW) ficam de fora de propósito: ver docs/10-roadmap.md.
var families = map[string]family{
	".jpg": famImage, ".jpeg": famImage, ".jpe": famImage, ".png": famImage,
	".gif": famImage, ".webp": famImage, ".bmp": famImage,

	".tif": famTIFF, ".tiff": famTIFF,
	// RAW baseados em TIFF: as dimensões saem do mesmo IFD do EXIF.
	".dng": famTIFF, ".cr2": famTIFF, ".nef": famTIFF, ".nrw": famTIFF, ".arw": famTIFF,
	".sr2": famTIFF, ".srf": famTIFF, ".orf": famTIFF, ".rw2": famTIFF, ".pef": famTIFF,
	".srw": famTIFF, ".erf": famTIFF, ".mef": famTIFF, ".3fr": famTIFF, ".kdc": famTIFF,
	".dcr": famTIFF, ".iiq": famTIFF,

	".mp4": famISO, ".m4v": famISO, ".mov": famISO, ".qt": famISO, ".m4a": famISO,
	".3gp": famISO, ".3g2": famISO, ".heic": famISO, ".heif": famISO, ".hif": famISO,
	".avif": famISO, ".cr3": famISO,

	".mkv": famEBML, ".webm": famEBML, ".mka": famEBML,

	".avi": famRIFF, ".wav": famRIFF, ".wave": famRIFF,

	".mp3": famAudio, ".flac": famAudio, ".ogg": famAudio, ".oga": famAudio,
	".opus": famAudio, ".aac": famAudio, ".wma": famAudio, ".aiff": famAudio, ".aif": famAudio,
}

// audioByExt classifica os arquivos de famAudio, onde o formato é a própria extensão.
func audioByExt(ext string) string { return strings.TrimPrefix(ext, ".") }

func familyOf(name string) family {
	return families[strings.ToLower(filepath.Ext(name))]
}

// Supported reports whether a name looks like something Probe can read.
func Supported(name string) bool { return familyOf(name) != famNone }

// IsVideoExt reports whether the extension is one of the video containers. Serve à
// interface, que precisa saber o que esperar antes de qualquer leitura.
func IsVideoExt(name string) bool {
	switch familyOf(name) {
	case famEBML:
		return true
	case famISO, famRIFF:
		ext := strings.ToLower(filepath.Ext(name))
		return ext != ".m4a" && ext != ".wav" && ext != ".wave" && ext != ".heic" &&
			ext != ".heif" && ext != ".hif" && ext != ".avif" && ext != ".cr3"
	}
	return false
}

// Probe reads metadata from an open file. It never decodes pixels and never seeks past
// the declared size. Um arquivo que a extensão promete mas o conteúdo desmente devolve
// ErrUnsupported; um arquivo legível com campos faltando devolve o que deu para ler.
func Probe(r io.ReaderAt, name string, size int64) (Meta, error) {
	if size <= 0 {
		return Meta{}, ErrUnsupported
	}
	s := section{r: r, size: size}
	ext := strings.ToLower(filepath.Ext(name))
	switch families[ext] {
	case famImage:
		return probeImage(s, ext)
	case famTIFF:
		return probeTIFF(s, ext)
	case famISO:
		return probeISO(s, ext)
	case famEBML:
		return probeEBML(s, ext)
	case famRIFF:
		return probeRIFF(s, ext)
	case famAudio:
		return Meta{Kind: KindAudio, Format: audioByExt(ext)}, nil
	}
	return Meta{}, ErrUnsupported
}

// maxRead is the largest single read a parser may ask for. Nenhum tamanho declarado
// dentro do arquivo passa disso, então um campo de comprimento adulterado não vira
// uma alocação de gigabytes.
const maxRead = 1 << 20

var errTooBig = errors.New("declared length above the read limit")

// section is a bounded, random-access view over the file.
type section struct {
	r    io.ReaderAt
	size int64
}

// at reads n bytes at off, truncating at the end of the file. Pedidos fora do arquivo,
// negativos ou acima de maxRead falham em vez de alocar.
func (s section) at(off int64, n int) ([]byte, error) {
	if off < 0 || n <= 0 || off >= s.size {
		return nil, io.EOF
	}
	if n > maxRead {
		return nil, errTooBig
	}
	if int64(n) > s.size-off {
		n = int(s.size - off)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(io.NewSectionReader(s.r, off, int64(n)), b); err != nil {
		return nil, err
	}
	return b, nil
}

// exact reads exactly n bytes at off, failing when the file is shorter.
func (s section) exact(off int64, n int) ([]byte, error) {
	b, err := s.at(off, n)
	if err != nil {
		return nil, err
	}
	if len(b) < n {
		return nil, io.ErrUnexpectedEOF
	}
	return b, nil
}

func be16(b []byte) uint16 { return uint16(b[0])<<8 | uint16(b[1]) }
func be32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
func be64(b []byte) uint64 { return uint64(be32(b))<<32 | uint64(be32(b[4:])) }

func le16(b []byte) uint16 { return uint16(b[1])<<8 | uint16(b[0]) }
func le32(b []byte) uint32 {
	return uint32(b[3])<<24 | uint32(b[2])<<16 | uint32(b[1])<<8 | uint32(b[0])
}

// fps converts a frame count and a duration into FPSMilli, refusing the absurd.
func fpsMilli(frames int64, durMS int64) int {
	if frames <= 0 || durMS <= 0 {
		return 0
	}
	v := frames * 1000 * 1000 / durMS
	if v <= 0 || v > 1_000_000 {
		return 0
	}
	return int(v)
}

// bitrate is the average over the whole file, the only one a header can tell.
func bitrate(size int64, durMS int64) int64 {
	if size <= 0 || durMS <= 0 {
		return 0
	}
	return size * 8 * 1000 / durMS
}

// applyRotation swaps the sides when the declared rotation is a quarter turn.
func applyRotation(m *Meta, rot int) {
	rot = ((rot % 360) + 360) % 360
	m.Rotation = rot
	if rot == 90 || rot == 270 {
		m.Width, m.Height = m.Height, m.Width
	}
}

// printable keeps only what is safe to show and to group by: o EXIF de uma câmera
// desconhecida pode trazer qualquer byte, e essas strings vão para a interface.
func printable(s string, max int) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s))
	if len(s) > max {
		s = strings.TrimSpace(s[:max])
	}
	return s
}
