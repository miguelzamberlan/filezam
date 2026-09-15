// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package vfs

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Nomes equivalentes numa pasta. Windows e Samba não diferenciam maiúsculas ("C8347.MP4" e
// "C8347.mp4" são o mesmo arquivo), e o macOS grava letras acentuadas decompostas (NFD), que na
// tela são idênticas às compostas (NFC). No Linux, inclusive num NTFS montado pelo ntfs3 sem
// "nocase", os dois nomes convivem, e quem enxerga o disco pelo Samba vê dois itens iguais dos
// quais só um abre. Por isso o Filezam trata como ocupado todo nome equivalente a um que já
// existe, em qualquer sistema de arquivos. O nome é gravado como veio; só a comparação é dobrada.
//
// Acentos continuam contando: "relatório" e "relatorio" são arquivos diferentes em todo lugar.

// NameKey is the form two names in the same folder are compared in to decide a conflict.
func NameKey(name string) string {
	ascii := true
	for i := 0; i < len(name); i++ {
		if name[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToLower(name)
	}
	// ToUpper antes de ToLower junta as variantes que só se encontram na maiúscula, como o sigma
	// final (ς e σ viram Σ), do mesmo jeito que a tabela de maiúsculas do NTFS.
	return strings.Map(func(r rune) rune { return unicode.ToLower(unicode.ToUpper(r)) }, norm.NFC.String(name))
}

// NameTakenError reports that a name is taken by an entry with an equivalent name (Existing,
// que pode diferir do pedido só em maiúsculas ou na forma Unicode).
type NameTakenError struct{ Existing string }

func (e *NameTakenError) Error() string { return fmt.Sprintf("%s: %s", ErrExists, e.Existing) }

// Is makes errors.Is(err, ErrExists) hold.
func (e *NameTakenError) Is(target error) bool { return target == ErrExists }

func nameTaken(existing string) error { return &NameTakenError{Existing: existing} }

// Namer answers "is this name taken in that folder?" reading each folder at most once. Serve a
// uma requisição ou a um job: um lote de 200 arquivos numa pasta com 10 mil itens lê a pasta uma
// vez, não 200. O nome exato é sempre conferido no disco na hora; só as variantes vêm do retrato,
// que Add mantém em dia com o que a própria operação cria. Um nome equivalente criado por outro
// escritor depois do retrato escapa — janela aceita, a mesma de qualquer checagem antes de gravar.
type Namer struct {
	r    *Root
	dirs map[string]map[string]string // pasta → chave → nome no disco
}

// NewNamer starts an empty cache over r.
func (r *Root) NewNamer() *Namer { return &Namer{r: r, dirs: map[string]map[string]string{}} }

func (n *Namer) index(dir string) (map[string]string, error) {
	if m, ok := n.dirs[dir]; ok {
		return m, nil
	}
	m := map[string]string{}
	f, err := n.r.r.Open(osPath(dir))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// pasta ainda não existe: nada a comparar
	case err != nil:
		return nil, MapError(err)
	default:
		names, err := f.Readdirnames(-1)
		f.Close()
		if err != nil {
			return nil, MapError(err)
		}
		for _, name := range names {
			if strings.HasPrefix(name, ReservedPrefix) {
				continue
			}
			k := NameKey(name)
			if _, dup := m[k]; !dup {
				m[k] = name
			}
		}
	}
	n.dirs[dir] = m
	return m, nil
}

// Lookup returns the name on disk that occupies name's place in dir: name itself when it exists,
// else an equivalent one, else "".
func (n *Namer) Lookup(dir, name string) (string, error) {
	if _, err := n.r.r.Lstat(osPath(Join(dir, name))); err == nil {
		n.Add(dir, name)
		return name, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", MapError(err)
	}
	m, err := n.index(dir)
	if err != nil {
		return "", err
	}
	return m[NameKey(name)], nil
}

// Add records a name the operation just created in dir.
func (n *Namer) Add(dir, name string) {
	if m, ok := n.dirs[dir]; ok {
		if k := NameKey(name); m[k] == "" {
			m[k] = name
		}
	}
}

// Forget drops a name the operation removed from dir.
func (n *Namer) Forget(dir, name string) {
	if m, ok := n.dirs[dir]; ok {
		if k := NameKey(name); m[k] == name {
			delete(m, k)
		}
	}
}

// Unique returns name when nothing equivalent exists in dir, else the first free "name (n).ext".
func (n *Namer) Unique(dir, name string) (string, error) {
	if cur, err := n.Lookup(dir, name); err != nil || cur == "" {
		return name, err
	}
	return n.nextFree(dir, name)
}

func (n *Namer) nextFree(dir, name string) (string, error) {
	base, ext := SplitExt(name)
	for i := 1; i < 10000; i++ {
		cand := fmt.Sprintf("%s (%d)%s", base, i, ext)
		cur, err := n.Lookup(dir, cand)
		if err != nil {
			return "", err
		}
		if cur == "" {
			return cand, nil
		}
	}
	return "", fmt.Errorf("%w: no free name", ErrExists)
}

// ResolveDir maps each existing segment of p to the folder already on disk under an equivalent
// name, so that "fotos/2026" lands inside an existing "Fotos". Segmentos que ainda não existem
// ficam como vieram. Um segmento equivalente a um arquivo (e não a uma pasta) é conflito.
func (n *Namer) ResolveDir(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	out := ""
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		cur, err := n.Lookup(out, seg)
		if err != nil {
			return "", err
		}
		if cur == "" {
			return Join(append([]string{out}, segs[i:]...)...), nil
		}
		if cur != seg {
			fi, err := n.r.r.Stat(osPath(Join(out, cur)))
			if err != nil {
				return "", MapError(err)
			}
			if !fi.IsDir() {
				return "", nameTaken(Join(out, cur))
			}
		}
		out = Join(out, cur)
	}
	return out, nil
}

// MkdirAll creates p and its parents, reusing folders that exist under equivalent names, and
// returns the path actually on disk.
func (n *Namer) MkdirAll(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	for _, seg := range strings.Split(p, "/") {
		if err := ValidName(seg); err != nil {
			return "", err
		}
	}
	real, err := n.ResolveDir(p)
	if err != nil {
		return "", err
	}
	if err := n.r.r.MkdirAll(real, 0o755); err != nil {
		return "", MapError(err)
	}
	dir := ""
	for _, seg := range strings.Split(real, "/") {
		n.Add(dir, seg)
		dir = Join(dir, seg)
	}
	return real, nil
}
