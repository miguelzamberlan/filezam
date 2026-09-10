// Package config parses Filezam configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SecureCookies controls the Secure attribute on the session cookie.
type SecureCookies int

const (
	SecureAuto SecureCookies = iota
	SecureOn
	SecureOff
)

// Config holds all runtime settings.
type Config struct {
	Root           string
	DataDir        string
	Listen         string
	TrustedProxies []netip.Prefix
	SecureCookies  SecureCookies
	PublicURL      string
	AdminUser      string
	AdminPassword  string
	SessionTTL     time.Duration
	SessionMaxTTL  time.Duration
	ChunkSize      int64
	BatchMaxFiles  int
	BatchMaxBytes  int64
	MaxParallel    int
	ShareMaxTTL    time.Duration
	Fsync          bool
	LogLevel       string
	UploadStaleAge time.Duration
	TrashRetention time.Duration // 0 = lixeira desativada (exclusão permanente)
	IndexInterval  time.Duration // varredura completa do índice de nomes; 0 = índice desativado
	MetricsToken   string        // token do /metrics; vazio = desativado
}

// resolve follows symlinks when the path exists; otherwise the absolute path is used as is.
func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// Load reads configuration from the environment and validates it.
func Load() (*Config, error) {
	c := &Config{
		Root:           env("FILEZAM_ROOT", "./data"),
		DataDir:        env("FILEZAM_DATA_DIR", "./config"),
		Listen:         env("FILEZAM_LISTEN", ":8080"),
		PublicURL:      strings.TrimRight(env("FILEZAM_PUBLIC_URL", ""), "/"),
		AdminUser:      env("FILEZAM_ADMIN_USER", "admin"),
		AdminPassword:  env("FILEZAM_ADMIN_PASSWORD", "admin"),
		LogLevel:       strings.ToLower(env("FILEZAM_LOG_LEVEL", "info")),
		MetricsToken:   env("FILEZAM_METRICS_TOKEN", ""),
		BatchMaxFiles:  200,
		BatchMaxBytes:  32 << 20,
		MaxParallel:    4,
		SessionMaxTTL:  30 * 24 * time.Hour,
		UploadStaleAge: 24 * time.Hour,
	}
	var err error
	if c.SessionTTL, err = durationEnv("FILEZAM_SESSION_TTL", 168*time.Hour); err != nil {
		return nil, err
	}
	if c.ShareMaxTTL, err = durationEnv("FILEZAM_SHARE_MAX_TTL", 720*time.Hour); err != nil {
		return nil, err
	}
	if env("FILEZAM_TRASH_RETENTION", "") == "0" {
		c.TrashRetention = 0
	} else if c.TrashRetention, err = durationEnv("FILEZAM_TRASH_RETENTION", 720*time.Hour); err != nil {
		return nil, err
	}
	if env("FILEZAM_INDEX_INTERVAL", "") == "0" {
		c.IndexInterval = 0
	} else if c.IndexInterval, err = durationEnv("FILEZAM_INDEX_INTERVAL", 6*time.Hour); err != nil {
		return nil, err
	}
	if c.ChunkSize, err = bytesEnv("FILEZAM_MAX_UPLOAD_CHUNK", 16<<20); err != nil {
		return nil, err
	}
	if c.ChunkSize < 1<<20 || c.ChunkSize > 1<<30 {
		return nil, errors.New("FILEZAM_MAX_UPLOAD_CHUNK must be between 1MiB and 1GiB")
	}
	if c.Fsync, err = boolEnv("FILEZAM_FSYNC", true); err != nil {
		return nil, err
	}
	switch strings.ToLower(env("FILEZAM_SECURE_COOKIES", "auto")) {
	case "auto":
		c.SecureCookies = SecureAuto
	case "true", "1", "yes":
		c.SecureCookies = SecureOn
	case "false", "0", "no":
		c.SecureCookies = SecureOff
	default:
		return nil, errors.New("FILEZAM_SECURE_COOKIES must be auto, true or false")
	}
	for _, p := range strings.Split(env("FILEZAM_TRUSTED_PROXIES", ""), ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.Contains(p, "/") {
			addr, err := netip.ParseAddr(p)
			if err != nil {
				return nil, fmt.Errorf("FILEZAM_TRUSTED_PROXIES: invalid entry %q", p)
			}
			c.TrustedProxies = append(c.TrustedProxies, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}
		pref, err := netip.ParsePrefix(p)
		if err != nil {
			return nil, fmt.Errorf("FILEZAM_TRUSTED_PROXIES: invalid entry %q", p)
		}
		c.TrustedProxies = append(c.TrustedProxies, pref)
	}
	if c.Root, err = filepath.Abs(c.Root); err != nil {
		return nil, err
	}
	if c.DataDir, err = filepath.Abs(c.DataDir); err != nil {
		return nil, err
	}
	if c.Root == string(filepath.Separator) {
		return nil, errors.New("FILEZAM_ROOT must not be the filesystem root")
	}
	// Compara os caminhos reais: um symlink /srv/config -> /srv/data/config colocaria o banco dentro da raiz.
	realRoot, realData := resolve(c.Root), resolve(c.DataDir)
	if realData == realRoot || strings.HasPrefix(realData, realRoot+string(filepath.Separator)) {
		return nil, errors.New("FILEZAM_DATA_DIR must not be inside FILEZAM_ROOT (the database would be exposed)")
	}
	if c.SessionTTL > c.SessionMaxTTL {
		c.SessionTTL = c.SessionMaxTTL
	}
	if c.AdminUser == "" || c.AdminPassword == "" {
		return nil, errors.New("FILEZAM_ADMIN_USER and FILEZAM_ADMIN_PASSWORD must not be empty")
	}
	return c, nil
}

// DBPath returns the SQLite database path.
func (c *Config) DBPath() string { return filepath.Join(c.DataDir, "filezam.db") }

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func durationEnv(key string, def time.Duration) (time.Duration, error) {
	v := env(key, "")
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s: invalid duration %q", key, v)
	}
	return d, nil
}

func boolEnv(key string, def bool) (bool, error) {
	v := env(key, "")
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: invalid boolean %q", key, v)
	}
	return b, nil
}

// bytesEnv parses values like "16MiB", "64M", "1073741824".
func bytesEnv(key string, def int64) (int64, error) {
	v := strings.TrimSpace(env(key, ""))
	if v == "" {
		return def, nil
	}
	up := strings.ToUpper(v)
	mult := int64(1)
	for _, s := range []struct {
		suf string
		m   int64
	}{{"GIB", 1 << 30}, {"GB", 1 << 30}, {"G", 1 << 30}, {"MIB", 1 << 20}, {"MB", 1 << 20}, {"M", 1 << 20}, {"KIB", 1 << 10}, {"KB", 1 << 10}, {"K", 1 << 10}} {
		if strings.HasSuffix(up, s.suf) {
			up = strings.TrimSuffix(up, s.suf)
			mult = s.m
			break
		}
	}
	n, err := strconv.ParseInt(strings.TrimSpace(up), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s: invalid size %q", key, v)
	}
	return n * mult, nil
}
