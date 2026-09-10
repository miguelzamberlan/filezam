package config

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setEnv isolates the FILEZAM_* variables of a test and points root/data at temp folders.
func setEnv(t *testing.T, vars map[string]string) (root, data string) {
	t.Helper()
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "FILEZAM_") {
			t.Setenv(k, "")
		}
	}
	base := t.TempDir()
	root, data = filepath.Join(base, "data"), filepath.Join(base, "config")
	os.MkdirAll(root, 0o755)
	os.MkdirAll(data, 0o755)
	t.Setenv("FILEZAM_ROOT", root)
	t.Setenv("FILEZAM_DATA_DIR", data)
	for k, v := range vars {
		t.Setenv(k, v)
	}
	return root, data
}

func TestDefaults(t *testing.T) {
	setEnv(t, nil)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.ChunkSize != 16<<20 || c.UploadReserve != 100<<30 || c.TrashRetention != 720*time.Hour || c.IndexInterval != 6*time.Hour ||
		c.SessionTTL != 168*time.Hour || c.SecureCookies != SecureAuto || len(c.TrustedProxies) != 0 || !c.Fsync {
		t.Fatalf("defaults: %+v", c)
	}
}

func TestParsing(t *testing.T) {
	setEnv(t, map[string]string{
		"FILEZAM_MAX_UPLOAD_CHUNK":    "32MiB",
		"FILEZAM_UPLOAD_MAX_RESERVED": "0",
		"FILEZAM_TRASH_RETENTION":     "0",
		"FILEZAM_INDEX_INTERVAL":      "0",
		"FILEZAM_TRUSTED_PROXIES":     "127.0.0.1, 172.16.0.0/12,::1",
		"FILEZAM_SECURE_COOKIES":      "false",
		"FILEZAM_SESSION_TTL":         "10000h", // acima do teto de 30 dias
		"FILEZAM_PUBLIC_URL":          "https://files.example.com/",
	})
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.ChunkSize != 32<<20 || c.UploadReserve != 0 || c.TrashRetention != 0 || c.IndexInterval != 0 || c.SecureCookies != SecureOff ||
		c.SessionTTL != c.SessionMaxTTL || c.PublicURL != "https://files.example.com" {
		t.Fatalf("parsed: %+v", c)
	}
	want := []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("::1/128")}
	if len(c.TrustedProxies) != 3 {
		t.Fatalf("proxies: %v", c.TrustedProxies)
	}
	for i := range want {
		if c.TrustedProxies[i] != want[i] {
			t.Fatalf("proxy %d: %v want %v", i, c.TrustedProxies[i], want[i])
		}
	}
	setEnv(t, map[string]string{"FILEZAM_UPLOAD_MAX_RESERVED": "2TiB"})
	if c, err := Load(); err != nil || c.UploadReserve != 2<<40 {
		t.Fatalf("2TiB: %v %v", c, err)
	}
}

func TestInvalidValuesAreRejected(t *testing.T) {
	for k, v := range map[string]string{
		"FILEZAM_MAX_UPLOAD_CHUNK":    "512K", // abaixo de 1 MiB
		"FILEZAM_UPLOAD_MAX_RESERVED": "-5G",
		"FILEZAM_TRASH_RETENTION":     "sempre",
		"FILEZAM_TRUSTED_PROXIES":     "10.0.0.0/33",
		"FILEZAM_SECURE_COOKIES":      "talvez",
		"FILEZAM_SECRET_KEY":          "abcd",
		"FILEZAM_FSYNC":               "x",
	} {
		setEnv(t, map[string]string{k: v})
		if _, err := Load(); err == nil {
			t.Errorf("%s=%q accepted", k, v)
		}
	}
	setEnv(t, map[string]string{"FILEZAM_ROOT": "/"})
	if _, err := Load(); err == nil {
		t.Error("filesystem root accepted as FILEZAM_ROOT")
	}
}

// O banco nunca pode ficar dentro da raiz servida, nem por caminho direto nem por symlink.
func TestDataDirInsideRootIsRejected(t *testing.T) {
	root, _ := setEnv(t, nil)
	t.Setenv("FILEZAM_DATA_DIR", filepath.Join(root, "config"))
	if _, err := Load(); err == nil {
		t.Fatal("data dir inside root accepted")
	}
	t.Setenv("FILEZAM_DATA_DIR", root)
	if _, err := Load(); err == nil {
		t.Fatal("data dir equal to root accepted")
	}
	// /tmp/x/config -> /tmp/x/data/cfg: o caminho parece fora, o destino real está dentro
	inside := filepath.Join(root, "cfg")
	os.MkdirAll(inside, 0o755)
	link := filepath.Join(filepath.Dir(root), "config-link")
	if err := os.Symlink(inside, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FILEZAM_DATA_DIR", link)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "FILEZAM_DATA_DIR") {
		t.Fatalf("data dir symlinked into root accepted: %v", err)
	}
	// e o contrário vale: um prefixo de nome parecido não é "dentro"
	sibling := root + "-config"
	os.MkdirAll(sibling, 0o755)
	t.Setenv("FILEZAM_DATA_DIR", sibling)
	if _, err := Load(); err != nil {
		t.Fatalf("sibling with the root as name prefix refused: %v", err)
	}
}
