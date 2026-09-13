// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

// Command filezam is the Filezam web file manager server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/miguelzamberlan/filezam/internal/auth"
	"github.com/miguelzamberlan/filezam/internal/config"
	"github.com/miguelzamberlan/filezam/internal/server"
	"github.com/miguelzamberlan/filezam/internal/store"
	"github.com/miguelzamberlan/filezam/internal/vfs"
)

var version = "dev"

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "healthcheck":
		err = healthcheck()
	case "reset-admin":
		err = resetAdmin(os.Args[2:])
	case "version":
		fmt.Println(version)
	default:
		err = fmt.Errorf("unknown command %q (serve | healthcheck | reset-admin | version)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "filezam:", err)
		os.Exit(1)
	}
}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv}))
}

func openAll(cfg *config.Config) (*store.DB, *vfs.Root, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return nil, nil, fmt.Errorf("create data dir: %w", err)
	}
	if err := os.MkdirAll(cfg.Root, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create root dir: %w", err)
	}
	if err := server.CheckWritable(cfg.DataDir, cfg.Root); err != nil {
		return nil, nil, err
	}
	db, err := store.Open(cfg.DBPath())
	if err != nil {
		return nil, nil, err
	}
	root, err := vfs.Open(cfg.Root)
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("open root: %w", err)
	}
	return db, root, nil
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)
	db, root, err := openAll(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	defer root.Close()
	if cfg.SecureCookies == config.SecureAuto && len(cfg.TrustedProxies) == 0 {
		log.Warn("FILEZAM_SECURE_COOKIES=auto without FILEZAM_TRUSTED_PROXIES: cookies will not be Secure behind a proxy; set FILEZAM_TRUSTED_PROXIES or FILEZAM_SECURE_COOKIES=true")
	}
	srv, err := server.New(cfg, db, root, log, version)
	if err != nil {
		return err
	}
	srv.StartBackground()
	hs := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	log.Info("filezam listening", "addr", ln.Addr().String(), "root", cfg.Root, "data", cfg.DataDir, "version", version,
		"license", "AGPL-3.0-only", "source", "https://github.com/miguelzamberlan/filezam")
	errCh := make(chan error, 1)
	go func() { errCh <- hs.Serve(ln) }()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case sig := <-stop:
		log.Info("shutting down", "signal", sig.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := hs.Shutdown(ctx); err != nil {
		log.Warn("http shutdown", "err", err)
	}
	srv.Shutdown(30 * time.Second)
	return nil
}

func healthcheck() error {
	listen := os.Getenv("FILEZAM_LISTEN")
	if listen == "" {
		listen = ":8080"
	}
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		port = strings.TrimPrefix(listen, ":")
	}
	c := &http.Client{Timeout: 4 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/api/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	return nil
}

// resetAdmin sets the password of the admin user (FILEZAM_ADMIN_USER) from the first argument or
// FILEZAM_ADMIN_PASSWORD, re-enables it, and forces a change at next login. Sem nenhum dos dois,
// sorteia uma senha e a imprime: quem roda isto perdeu o acesso e precisa de algo que funcione
// agora, não de uma mensagem dizendo qual variável faltou.
func resetAdmin(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pw, generated := cfg.AdminPassword, false
	if len(args) > 0 {
		pw = args[0]
	}
	if pw == "" {
		if pw, err = auth.NewReadablePassword(); err != nil {
			return err
		}
		generated = true
	}
	if err := auth.CheckPolicy(pw); err != nil {
		return err
	}
	db, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	ctx := context.Background()
	u, err := db.GetUserByName(ctx, cfg.AdminUser)
	if errors.Is(err, store.ErrNotFound) {
		hash, err := auth.HashPassword(pw)
		if err != nil {
			return err
		}
		if _, err := db.CreateUser(ctx, &store.User{Username: cfg.AdminUser, PasswordHash: hash, Role: "admin", MustChangePassword: true}); err != nil {
			return err
		}
		fmt.Println("admin user created:", cfg.AdminUser)
		printGenerated(generated, pw)
		return nil
	}
	if err != nil {
		return err
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	u.PasswordHash = hash
	u.Role = "admin"
	u.Disabled = false
	u.MustChangePassword = true
	if err := db.UpdateUser(ctx, u); err != nil {
		return err
	}
	_ = db.DeleteUserSessions(ctx, u.ID, "")
	_ = db.RecordLoginSuccess(ctx, u.ID)
	// Quem chega aqui perdeu o acesso: sem limpar o 2FA, um admin sem autenticador e sem
	// códigos de recuperação continuaria trancado fora mesmo com shell no servidor.
	if u.TOTPEnabled() {
		_ = db.SetTOTP(ctx, u.ID, "", nil, "")
		fmt.Println("two-factor authentication cleared for:", cfg.AdminUser)
	}
	fmt.Println("admin password reset for:", cfg.AdminUser)
	printGenerated(generated, pw)
	return nil
}

// printGenerated mostra a senha sorteada. Só é impressa quando foi este comando que a criou:
// uma senha escolhida por quem chamou não precisa voltar pela tela.
func printGenerated(generated bool, pw string) {
	if generated {
		fmt.Println("generated password (change it at first login):", pw)
	}
}
