package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP (RFC 6238 sobre HOTP RFC 4226): 6 dígitos, SHA-1, período de 30 s, como os apps
// autenticadores esperam por padrão. Tudo com a biblioteca padrão.

const (
	totpPeriod = 30
	totpDigits = 6
	totpSkew   = 1 // aceita o intervalo anterior e o seguinte (±30 s de relógio)
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns 20 random bytes in base32 (the form apps scan/type).
func NewTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b32.EncodeToString(b), nil
}

// TOTPURI builds the otpauth:// URI for QR codes.
func TOTPURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{"secret": {secret}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {fmt.Sprint(totpDigits)}, "period": {fmt.Sprint(totpPeriod)}}
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// hotp computes the RFC 4226 code for a counter.
func hotp(secret string, counter uint64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.ReplaceAll(secret, " ", "")))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := (binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff) % 1000000
	return fmt.Sprintf("%06d", code), nil
}

// TOTPCounter is the 30-second interval index for t.
func TOTPCounter(t time.Time) uint64 { return uint64(t.Unix() / totpPeriod) }

// VerifyTOTP checks code against the current interval ±skew and refuses intervals already
// used (lastCounter) so a captured code cannot be replayed. Returns the accepted counter.
func VerifyTOTP(secret, code string, now time.Time, lastCounter uint64) (bool, uint64) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return false, 0
	}
	base := TOTPCounter(now)
	for d := -totpSkew; d <= totpSkew; d++ {
		c := uint64(int64(base) + int64(d))
		if c <= lastCounter {
			continue
		}
		want, err := hotp(secret, c)
		if err != nil {
			return false, 0
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return true, c
		}
	}
	return false, 0
}

// --- códigos de recuperação: 10 códigos de uso único, guardados como SHA-256 ---

const recoveryCount = 10

// NewRecoveryCodes returns codes like "k7f3-9qzp-2m4x" and their hashes for storage.
func NewRecoveryCodes() (codes []string, hashes []string, err error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789" // sem 0/o/1/l/i para ditar por telefone
	for i := 0; i < recoveryCount; i++ {
		b := make([]byte, 12)
		if _, err := rand.Read(b); err != nil {
			return nil, nil, err
		}
		var sb strings.Builder
		for j, x := range b {
			if j > 0 && j%4 == 0 {
				sb.WriteByte('-')
			}
			sb.WriteByte(alphabet[int(x)%len(alphabet)])
		}
		codes = append(codes, sb.String())
		hashes = append(hashes, HashRecoveryCode(sb.String()))
	}
	return codes, hashes, nil
}

// HashRecoveryCode normalises (lowercase, no dashes/spaces) and hashes a code.
func HashRecoveryCode(code string) string {
	n := strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
	sum := sha256.Sum256([]byte("filezam-recovery:" + n))
	return hex.EncodeToString(sum[:])
}

// UseRecoveryCode returns the remaining hashes without the one matching code (ok=false if none matched).
func UseRecoveryCode(hashes []string, code string) (remaining []string, ok bool) {
	h := HashRecoveryCode(code)
	for i, x := range hashes {
		if subtle.ConstantTimeCompare([]byte(x), []byte(h)) == 1 {
			return append(append([]string{}, hashes[:i]...), hashes[i+1:]...), true
		}
	}
	return hashes, false
}

// TOTPCode returns the code for time t (tests and tooling).
func TOTPCode(secret string, t time.Time) (string, error) { return hotp(secret, TOTPCounter(t)) }
