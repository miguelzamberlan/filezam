// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package media

import (
	"bufio"
	"image"
	"io"
	"strings"

	_ "image/gif" // decodificadores registrados por efeito colateral: só o cabeçalho é lido
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// configBuf é quanto se bufferiza para ler um cabeçalho de imagem. DecodeConfig lê só até
// achar as dimensões; o buffer existe para isso não virar dezenas de leituras de disco.
const configBuf = 64 << 10

// decodeConfig reads width, height and format without decoding a single pixel.
func decodeConfig(s section) (image.Config, string, error) {
	r := bufio.NewReaderSize(io.NewSectionReader(s.r, 0, s.size), configBuf)
	return image.DecodeConfig(r)
}

// probeImage handles the raster formats with a registered decoder.
func probeImage(s section, ext string) (Meta, error) {
	cfg, format, err := decodeConfig(s)
	if err != nil {
		return Meta{}, ErrUnsupported
	}
	m := Meta{Kind: KindImage, Format: format, Width: cfg.Width, Height: cfg.Height}
	if format == "jpeg" || format == "tiff" {
		if info, ok := readImageExif(s, format); ok {
			info.apply(&m)
			applyRotation(&m, rotationFromOrientation(info.orientation))
		}
	}
	if m.Width <= 0 || m.Height <= 0 {
		return Meta{}, ErrUnsupported
	}
	return m, nil
}

// probeTIFF handles TIFF and the raw formats built on it. O decodificador de TIFF só
// entende o IFD0, que num RAW costuma ser a miniatura — então quem manda aqui é a
// travessia de IFDs, e o decodificador entra só como reserva para um TIFF comum.
func probeTIFF(s section, ext string) (Meta, error) {
	format := strings.TrimPrefix(ext, ".")
	if format == "tif" {
		format = "tiff"
	}
	m := Meta{Kind: KindImage, Format: format}
	info, ok := parseExif(s, 0, s.size)
	if ok {
		info.apply(&m)
		m.Width, m.Height = info.dims()
	}
	if m.Width <= 0 || m.Height <= 0 {
		cfg, f, err := decodeConfig(s)
		if err != nil {
			if !ok {
				return Meta{}, ErrUnsupported
			}
			return m, nil // o EXIF abriu, as dimensões é que não estavam lá
		}
		m.Width, m.Height = cfg.Width, cfg.Height
		if format == "tiff" {
			m.Format = f
		}
	}
	if ok {
		applyRotation(&m, rotationFromOrientation(info.orientation))
	}
	return m, nil
}

// readImageExif locates the EXIF block: dentro do segmento APP1 num JPEG, no próprio
// cabeçalho quando o arquivo já é um TIFF.
func readImageExif(s section, format string) (exifInfo, bool) {
	if format == "tiff" {
		return parseExif(s, 0, s.size)
	}
	off, end, ok := jpegExifBlock(s)
	if !ok {
		return exifInfo{}, false
	}
	return parseExif(s, off, end)
}

// maxJPEGMarkers limita a varredura de segmentos: o EXIF vem nos primeiros, e um arquivo
// que só tem 0xFF repetido não pode render um laço longo.
const maxJPEGMarkers = 64

// jpegExifBlock walks the JPEG segments up to the scan and returns where the TIFF header
// of the APP1/Exif segment begins and ends.
func jpegExifBlock(s section) (start, end int64, ok bool) {
	hdr, err := s.exact(0, 2)
	if err != nil || hdr[0] != 0xff || hdr[1] != 0xd8 {
		return 0, 0, false
	}
	off := int64(2)
	for i := 0; i < maxJPEGMarkers; i++ {
		h, err := s.exact(off, 4)
		if err != nil {
			return 0, 0, false
		}
		if h[0] != 0xff {
			return 0, 0, false
		}
		marker := h[1]
		if marker == 0xd8 || marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
			off += 2
			continue
		}
		if marker == 0xd9 || marker == 0xda { // fim da imagem, ou começo dos dados
			return 0, 0, false
		}
		size := int64(be16(h[2:4]))
		if size < 2 || off+2+size > s.size {
			return 0, 0, false
		}
		if marker == 0xe1 {
			tag, err := s.at(off+4, 6)
			if err == nil && len(tag) == 6 && string(tag[:4]) == "Exif" && tag[4] == 0 {
				return off + 10, off + 2 + size, true
			}
		}
		off += 2 + size
	}
	return 0, 0, false
}
