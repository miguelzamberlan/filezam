package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miguelzamberlan/filezam/internal/vfs"
)

// Simula um banco já existente (só a migração 001) e confere que 002 sobe sem perder dados.
func TestUpgradeFrom001(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := migrationFS.ReadFile("migrations/001_init.sql")
	if _, err := raw.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL); INSERT INTO schema_migrations VALUES(1, 0);
		INSERT INTO users(id, username, password_hash, role, created_at, updated_at) VALUES(1,'x','h','admin',0,0);
		INSERT INTO shares(token_hash, path, name, created_by, created_at, expires_at) VALUES('h1','a','a',1,0,?)`, time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	shares, err := db.ListShares(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(shares) != 1 || shares[0].TokenHash != "h1" || shares[0].Token != "" || shares[0].Kind != "dir" || shares[0].PasswordHash != "" {
		t.Fatalf("shares after upgrade: %+v", shares)
	}
	if shares[0].Slug != "" || shares[0].Mode != "read" {
		t.Fatalf("share defaults after 011: %+v", shares[0])
	}
}

// O índice de apelido é parcial: links sem apelido guardam ” e precisam coexistir, enquanto
// dois apelidos iguais têm de colidir mesmo com um deles já revogado.
func TestSlugUniqueIndexIsPartial(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.w.ExecContext(ctx, `INSERT INTO users(id, username, password_hash, role, created_at, updated_at) VALUES(1,'x','h','admin',0,0)`); err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(time.Hour).Unix()
	mk := func(hash, slug string) error {
		_, err := db.CreateShare(ctx, &Share{TokenHash: hash, Slug: slug, Path: "a", Name: "a", CreatedBy: 1, ExpiresAt: exp})
		return err
	}
	if err := mk("h1", ""); err != nil {
		t.Fatal(err)
	}
	if err := mk("h2", ""); err != nil {
		t.Fatalf("two empty slugs must coexist: %v", err)
	}
	if err := mk("h3", "vendas"); err != nil {
		t.Fatal(err)
	}
	if err := mk("h4", "vendas"); err != ErrConflict {
		t.Fatalf("duplicate slug: want ErrConflict, got %v", err)
	}
	sh, err := db.GetActiveShareBySlug(ctx, "vendas")
	if err != nil || sh.TokenHash != "h3" {
		t.Fatalf("lookup by slug: %+v %v", sh, err)
	}
	// Revogado some da consulta pública mas continua reservando o apelido.
	if err := db.RevokeShare(ctx, sh.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetActiveShareBySlug(ctx, "vendas"); err != ErrNotFound {
		t.Fatalf("revoked slug still resolves: %v", err)
	}
	if taken, _ := db.SlugTaken(ctx, "vendas"); !taken {
		t.Fatal("revoked slug returned to the pool")
	}
	if err := mk("h5", "vendas"); err != ErrConflict {
		t.Fatalf("revoked slug reclaimed: %v", err)
	}
}

// Um banco de antes da 016 guarda o nome só em minúsculas: ao abrir, as linhas são redobradas
// e a pesquisa sem acento já acha o nome acentuado, sem esperar a próxima varredura.
func TestUpgradeRefoldsIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for v := 1; v <= 15; v++ {
		names, _ := migrationFS.ReadDir("migrations")
		for _, n := range names {
			if strings.HasPrefix(n.Name(), fmt.Sprintf("%03d_", v)) {
				body, _ := migrationFS.ReadFile("migrations/" + n.Name())
				if _, err := raw.Exec(string(body)); err != nil {
					t.Fatalf("%s: %v", n.Name(), err)
				}
				raw.Exec(`INSERT INTO schema_migrations VALUES(?, 0)`, v)
			}
		}
	}
	// 2 500 linhas: passa de um lote da redobra
	for i := range 2500 {
		name := fmt.Sprintf("arquivo %d.txt", i)
		if i == 2400 {
			name = "Relatório de AÇÃO.pdf"
		}
		if _, err := raw.Exec(`INSERT INTO file_index(path, parent, name, name_lc, type, size, mtime, gen) VALUES(?, '', ?, ?, 'file', 1, 0, 1)`, name, name, strings.ToLower(name)); err != nil {
			t.Fatal(err)
		}
	}
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, q := range []string{"relatorio de acao", "AÇÃO", "ação"} {
		if got, err := db.SearchIndex(ctx, "", q, 10); err != nil || len(got) != 1 || got[0].Name != "Relatório de AÇÃO.pdf" {
			t.Fatalf("search %q after upgrade: %+v %v", q, got, err)
		}
	}
	var fold int64
	if err := db.r.QueryRowContext(ctx, `SELECT fold FROM index_state WHERE id=1`).Scan(&fold); err != nil || fold != vfs.FoldVersion {
		t.Fatalf("fold version: %d %v", fold, err)
	}
}

func TestIndexLikeEscape(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	rows := []IndexRow{{Path: "a/x_y.txt", Name: "x_y.txt", Type: "file"}, {Path: "a/xzy.txt", Name: "xzy.txt", Type: "file"}, {Path: "b/100%.txt", Name: "100%.txt", Type: "file"}, {Path: "b/100pct.txt", Name: "100pct.txt", Type: "file"}}
	if err := db.UpsertIndexBatch(ctx, rows, 1); err != nil {
		t.Fatal(err)
	}
	got, _ := db.SearchIndex(ctx, "", "x_y", 10)
	if len(got) != 1 || got[0].Name != "x_y.txt" {
		t.Fatalf("underscore treated as wildcard: %+v", got)
	}
	got, _ = db.SearchIndex(ctx, "", "100%", 10)
	if len(got) != 1 || got[0].Name != "100%.txt" {
		t.Fatalf("percent treated as wildcard: %+v", got)
	}
	got, _ = db.SearchIndex(ctx, "b", "1", 10)
	if len(got) != 2 {
		t.Fatalf("prefix filter: %+v", got)
	}
	if err := db.DeleteIndexTree(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.IndexCount(ctx); n != 2 {
		t.Fatalf("delete tree: %d", n)
	}
	if err := db.DeleteIndexNotGen(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.IndexCount(ctx); n != 0 {
		t.Fatalf("delete not gen: %d", n)
	}
}
