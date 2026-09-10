package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
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
}
