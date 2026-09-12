package vfs

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// ExtractLimits caps what one extraction may do. Todos obrigatórios: o extrator recusa limites
// zerados, porque um arquivo compactado é conteúdo de terceiros e sem teto vira bomba.
type ExtractLimits struct {
	// MaxBytes é o total REALMENTE escrito no disco. O tamanho descomprimido declarado no
	// cabeçalho é escolhido por quem montou o arquivo e mente; só os bytes que passam pelo
	// io.Copy contam.
	MaxBytes int64
	// MaxEntries limita a contagem de entradas: a cota limita bytes, não inodes, e um arquivo
	// com um milhão de entradas vazias passaria em MaxBytes.
	MaxEntries int
}

// ExtractResult conta o que a extração fez. Skipped são entradas recusadas (nome inválido,
// symlink, cifrada, duplicada) que viraram aviso em vez de derrubar o trabalho inteiro.
type ExtractResult struct {
	Files   int
	Dirs    int
	Skipped int
	Bytes   int64
}

// ErrArchiveLimit indica que a extração passou de MaxBytes ou MaxEntries.
var ErrArchiveLimit = errors.New("archive limit exceeded")

// ErrBadArchive indica um arquivo que não é um formato suportado, ou que está corrompido.
var ErrBadArchive = errors.New("not a readable archive")

// zipMagic: assinatura de um arquivo zip (vazio ou não).
func isZip(head []byte) bool {
	return len(head) >= 4 && head[0] == 'P' && head[1] == 'K' &&
		(head[2] == 3 || head[2] == 5 || head[2] == 6 || head[2] == 7)
}

// ExtractZip unpacks src into dstDir, which must be an empty directory the caller just created.
//
// Regras que valem para toda entrada, e que são o que torna seguro descompactar conteúdo vindo
// de fora:
//   - o nome é normalizado SOZINHO, antes de ser juntado ao destino. Normalizar o caminho já
//     juntado deixaria "../x" virar um irmão da pasta de destino, porque o ".." seria colapsado
//     contra ela em vez de estourar;
//   - a extração acontece dentro de um os.Root aberto na própria pasta de destino, então nem um
//     erro de lógica aqui alcança o resto do escopo;
//   - só arquivo regular e diretório são criados. Nunca um symlink: ele permitiria que a entrada
//     seguinte escrevesse através dele para fora;
//   - o modo declarado no arquivo é ignorado (0644/0755 fixos), então setuid/setgid/sticky não
//     sobrevivem à extração;
//   - entrada cifrada é pulada. O archive/zip não valida esse bit: ele copiaria o texto cifrado
//     para o disco com nome legítimo e só acusaria erro de checksum no fim.
func (r *Root) ExtractZip(ctx context.Context, src, dstDir string, lim ExtractLimits, prog *Progress) (ExtractResult, error) {
	var res ExtractResult
	if lim.MaxBytes <= 0 || lim.MaxEntries <= 0 {
		return res, fmt.Errorf("%w: limits required", ErrArchiveLimit)
	}
	f, fi, err := r.OpenFile(src)
	if err != nil {
		return res, err
	}
	defer f.Close()
	head := make([]byte, 4)
	if _, err := io.ReadFull(f, head); err != nil || !isZip(head) {
		return res, ErrBadArchive
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return res, MapError(err)
	}
	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return res, fmt.Errorf("%w: %v", ErrBadArchive, err)
	}
	if len(zr.File) > lim.MaxEntries {
		return res, fmt.Errorf("%w: %d entries", ErrArchiveLimit, len(zr.File))
	}
	// A partir daqui tudo é relativo à pasta de destino, dentro do seu próprio os.Root.
	dst, err := r.Sub(dstDir)
	if err != nil {
		return res, err
	}
	defer dst.Close()
	depth := Depth(dstDir)

	for _, e := range zr.File {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		rel, isDir, ok := entryPath(e.Name, depth)
		if !ok {
			res.Skipped++
			prog.warn(e.Name, ErrInvalidName)
			continue
		}
		// Bit 0 do descritor geral: conteúdo cifrado.
		if e.Flags&0x1 != 0 {
			res.Skipped++
			prog.warn(e.Name, errors.New("encrypted"))
			continue
		}
		if isDir || e.FileInfo().IsDir() {
			if err := dst.MkdirAll(rel); err != nil {
				res.Skipped++
				prog.warn(e.Name, err)
				continue
			}
			res.Dirs++
			continue
		}
		if !e.FileInfo().Mode().IsRegular() {
			res.Skipped++
			prog.warn(e.Name, errors.New("not a regular file"))
			continue
		}
		prog.current(rel)
		n, err := dst.writeEntry(rel, e, lim.MaxBytes-res.Bytes)
		res.Bytes += n
		prog.add(0, n)
		switch {
		case errors.Is(err, ErrArchiveLimit):
			return res, err
		case err != nil:
			res.Skipped++
			prog.warn(e.Name, err)
		default:
			res.Files++
			prog.add(1, 0)
		}
	}
	return res, nil
}

// entryPath turns an archive entry name into a path relative to the destination, or reports that
// it must be skipped. depth é a profundidade da pasta de destino: o limite de MaxDepth vale para
// o caminho final, senão a extração criaria um arquivo que existe mas que a interface não
// consegue nem abrir nem apagar.
func entryPath(name string, depth int) (rel string, isDir bool, ok bool) {
	isDir = strings.HasSuffix(name, "/")
	if !utf8.ValidString(name) {
		name = cp437(name) // zips do Windows trazem os acentos em CP437
	}
	// Normalizado SOZINHO: é aqui que ".." estoura, porque a pilha começa vazia.
	rel, err := NormalizeWritable(name)
	if err != nil || rel == "" {
		return "", false, false
	}
	for _, seg := range strings.Split(rel, "/") {
		if err := ValidName(seg); err != nil {
			return "", false, false
		}
	}
	if depth+Depth(rel) > MaxDepth || len(rel) > maxPathLen {
		return "", false, false
	}
	return rel, isDir, true
}

// writeEntry copies one archive entry, refusing to go past budget bytes.
func (r *Root) writeEntry(rel string, e *zip.File, budget int64) (int64, error) {
	if dir := Dir(rel); dir != "" {
		if err := r.MkdirAll(dir); err != nil {
			return 0, err
		}
	}
	// O_EXCL não segue symlink e falha se o nome já existe: é o que garante que uma entrada
	// duplicada não sobrescreva a anterior, e que nada fora da pasta seja alcançado.
	out, err := r.r.OpenFile(osPath(rel), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, MapError(err)
	}
	in, err := e.Open()
	if err != nil {
		out.Close()
		_ = r.r.Remove(osPath(rel))
		return 0, err
	}
	defer in.Close()
	// budget+1 para distinguir "coube exatamente" de "passou do teto".
	n, err := io.Copy(out, io.LimitReader(in, budget+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > budget {
		err = fmt.Errorf("%w: %d bytes", ErrArchiveLimit, n)
	}
	if err != nil {
		// Erro no meio (checksum, método não suportado, teto) não deixa arquivo pela metade
		// com nome legítimo: some.
		_ = r.r.Remove(osPath(rel))
		return 0, err
	}
	if !e.Modified.IsZero() {
		_ = r.Chtimes(rel, e.Modified)
	}
	return n, nil
}
