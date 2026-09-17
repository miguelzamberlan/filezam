// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

package uploads

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/miguelzamberlan/filezam/internal/store"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// newService devolve o serviço com um usuário (id 1) e um link de envio (id 1) já gravados:
// as sessões apontam para os dois por chave estrangeira.
func newService(t *testing.T) (*Service, *vfs.Root) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "filezam.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if u, err := db.CreateUser(context.Background(), &store.User{Username: "bob", PasswordHash: "x", Role: "user"}); err != nil || u.ID != 1 {
		t.Fatalf("usuário de teste: %v", err)
	}
	if sh, err := db.CreateShare(context.Background(), &store.Share{TokenHash: "t", Mode: "drop", Path: "caixa", Name: "caixa", CreatedBy: 1, ExpiresAt: 1 << 40}); err != nil || sh.ID != 1 {
		t.Fatalf("link de teste: %v", err)
	}
	root, err := vfs.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return New(db, 1<<20, false, 0, slog.New(slog.NewTextHandler(io.Discard, nil))), root
}

// Uma sessão que está recebendo um bloco agora (outra aba do mesmo usuário) não é descartada
// por quem cria uma sessão para o mesmo destino, nem que seja para outro arquivo: os bytes que
// já subiram continuariam chegando numa parte apagada.
func TestSupersedeSparesSessionReceivingChunk(t *testing.T) {
	s, root := newService(t)
	ctx := context.Background()
	mtime := int64(1700000000000)
	opts := func(size int64, mt int64) CreateOpts {
		return CreateOpts{Dir: "", Name: "video.mp4", UserID: 1, Size: size, Mtime: &mt}
	}
	first, err := s.Create(ctx, root, opts(3<<20, mtime))
	if err != nil {
		t.Fatal(err)
	}
	done := s.beginWrite(first.ID) // um PUT de bloco em voo
	if _, err := s.Create(ctx, root, opts(5<<20, mtime+60000)); !errors.Is(err, ErrInProgress) {
		t.Fatalf("sessão recebendo bytes deveria ser preservada: %v", err)
	}
	done()
	second, err := s.Create(ctx, root, opts(5<<20, mtime+60000))
	if err != nil {
		t.Fatalf("terminado o bloco, a sessão parada sai do caminho: %v", err)
	}
	if _, err := s.Get(ctx, SessionRef{ID: first.ID, UserID: 1}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("sessão antiga continua aberta: %v", err)
	}
	if _, err := root.StatReserved("", vfs.PartName(first.ID)); err == nil {
		t.Fatal("parte da sessão antiga ficou no disco")
	}
	if _, err := s.Get(ctx, SessionRef{ID: second.ID, UserID: 1}); err != nil {
		t.Fatal(err)
	}
}

// Sessão de outro dono (ou, num link de recebimento, de outro remetente) nunca é descartada,
// mesmo com o mesmo destino: quem abriu aquela sessão não é quem está criando esta.
func TestSupersedeOnlyOwnSession(t *testing.T) {
	s, root := newService(t)
	ctx := context.Background()
	mtime := int64(1700000000000)
	base := CreateOpts{Dir: "", Name: "video.mp4", UserID: 1, Size: 3 << 20, Mtime: &mtime, ShareID: 1, Sender: "alice"}
	if _, err := s.Create(ctx, root, base); err != nil {
		t.Fatal(err)
	}
	other := base
	other.Sender = "bruno"
	other.Size = 5 << 20
	if _, err := s.Create(ctx, root, other); !errors.Is(err, ErrInProgress) {
		t.Fatalf("sessão de outro remetente: %v", err)
	}
	mine := base
	mine.Size = 5 << 20
	if _, err := s.Create(ctx, root, mine); err != nil {
		t.Fatalf("o próprio remetente reenvia outro arquivo: %v", err)
	}
}
