// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package vfs

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

// MaxZipCentralDir limita o tamanho do diretório central que o archive/zip pode carregar. Ele lê
// o índice inteiro para a memória antes de qualquer filtro, e cada entrada vira uma struct de
// algumas centenas de bytes além do nome: um .zip só de cabeçalhos, pequeno em disco, faria o
// servidor alocar muito mais do que o arquivo pesa. 64 MiB de índice cobrem com folga o teto de
// entradas de fábrica (50 000, ~7 MB com nomes de 100 bytes) e o rígido (500 000).
const MaxZipCentralDir = 64 << 20

// zipDir describes where the central directory is, as the end records declare it.
type zipDir struct {
	entries uint64
	offset  int64 // início do diretório central
	size    int64 // bytes do diretório central
	end     int64 // início do primeiro registro final (zip64 ou comum): nada útil mora entre offset+size e end
}

// zipDirectory reads the end-of-central-directory record (and its zip64 variant) without loading
// the directory: lendo no máximo alguns kilobytes do fim do arquivo, diz quantas entradas e quantos
// bytes de índice o archive/zip vai alocar.
func zipDirectory(r io.ReaderAt, size int64) (zipDir, error) {
	const eocdLen = 22
	const maxComment = 1 << 16
	if size < eocdLen {
		return zipDir{}, ErrBadArchive
	}
	tail := min(size, eocdLen+maxComment)
	buf := make([]byte, tail)
	if _, err := r.ReadAt(buf, size-tail); err != nil && err != io.EOF {
		return zipDir{}, fmt.Errorf("%w: %v", ErrBadArchive, err)
	}
	at := -1
	for i := len(buf) - eocdLen; i >= 0; i-- {
		if binary.LittleEndian.Uint32(buf[i:]) == 0x06054b50 {
			at = i
			break
		}
	}
	if at < 0 {
		return zipDir{}, ErrBadArchive
	}
	e := buf[at:]
	d := zipDir{
		entries: uint64(binary.LittleEndian.Uint16(e[10:])),
		size:    int64(binary.LittleEndian.Uint32(e[12:])),
		offset:  int64(binary.LittleEndian.Uint32(e[16:])),
		end:     size - tail + int64(at),
	}
	// Valores saturados mandam procurar o registro zip64, apontado pelo localizador logo antes.
	if d.entries == 0xffff || d.size == 0xffffffff || d.offset == 0xffffffff {
		loc := make([]byte, 20)
		if d.end >= 20 {
			if _, err := r.ReadAt(loc, d.end-20); err == nil && binary.LittleEndian.Uint32(loc) == 0x07064b50 {
				recAt := int64(binary.LittleEndian.Uint64(loc[8:]))
				rec := make([]byte, 56)
				if recAt < 0 || recAt > d.end-20-56 {
					return zipDir{}, ErrBadArchive
				}
				if _, err := r.ReadAt(rec, recAt); err != nil || binary.LittleEndian.Uint32(rec) != 0x06064b50 {
					return zipDir{}, ErrBadArchive
				}
				d.entries = binary.LittleEndian.Uint64(rec[32:])
				d.size = int64(binary.LittleEndian.Uint64(rec[40:]))
				d.offset = int64(binary.LittleEndian.Uint64(rec[48:]))
				d.end = recAt
			}
		}
	}
	if d.size < 0 || d.offset < 0 || d.offset > d.end || d.size > d.end-d.offset {
		return zipDir{}, ErrBadArchive
	}
	return d, nil
}

// countZipHeaders walks the central directory headers declared by d, without allocating anything
// per entry, and stops as soon as there are more than max. É a contagem que vale — o número do
// registro final é escolhido por quem montou o arquivo.
func countZipHeaders(r io.ReaderAt, d zipDir, max int) (int, error) {
	br := bufio.NewReaderSize(io.NewSectionReader(r, d.offset, d.size), 64<<10)
	hdr := make([]byte, 46)
	n := 0
	for {
		if _, err := io.ReadFull(br, hdr); err != nil {
			if err == io.EOF {
				return n, nil
			}
			return n, fmt.Errorf("%w: truncated central directory", ErrBadArchive)
		}
		if binary.LittleEndian.Uint32(hdr) != 0x02014b50 {
			return n, fmt.Errorf("%w: bad central directory header", ErrBadArchive)
		}
		n++
		if n > max {
			return n, fmt.Errorf("%w: more than %d entries", ErrArchiveLimit, max)
		}
		skip := int64(binary.LittleEndian.Uint16(hdr[28:])) + int64(binary.LittleEndian.Uint16(hdr[30:])) + int64(binary.LittleEndian.Uint16(hdr[32:]))
		if _, err := br.Discard(int(skip)); err != nil {
			return n, fmt.Errorf("%w: truncated central directory", ErrBadArchive)
		}
	}
}

// boundedZip hides from the archive/zip reader whatever lies between the end of the declared
// central directory and the end records. O leitor da biblioteca não para no tamanho declarado:
// segue lendo cabeçalhos enquanto encontra a assinatura. Com essa faixa zerada, ele vê exatamente
// os cabeçalhos que countZipHeaders contou, e um índice que mente o tamanho não passa do teto.
type boundedZip struct {
	r       io.ReaderAt
	gapFrom int64
	gapTo   int64
}

func (b boundedZip) ReadAt(p []byte, off int64) (int, error) {
	n, err := b.r.ReadAt(p, off)
	lo, hi := max(off, b.gapFrom), min(off+int64(n), b.gapTo)
	for i := lo; i < hi; i++ {
		p[i-off] = 0
	}
	return n, err
}
