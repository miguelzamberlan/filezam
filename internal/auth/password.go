// Package auth implements password hashing, session tokens and rate limiting.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16

	// MinPasswordLen is the minimum accepted password length in characters.
	MinPasswordLen = 8
	// MaxPasswordLen bounds Argon2 input.
	MaxPasswordLen = 256
)

// ErrWeakPassword is returned by CheckPolicy.
var ErrWeakPassword = errors.New("password does not meet policy")

// CheckPolicy validates a candidate password.
func CheckPolicy(pw string) error {
	n := utf8.RuneCountInString(pw)
	if n < MinPasswordLen {
		return fmt.Errorf("%w: minimum %d characters", ErrWeakPassword, MinPasswordLen)
	}
	if n > MaxPasswordLen {
		return fmt.Errorf("%w: maximum %d characters", ErrWeakPassword, MaxPasswordLen)
	}
	return nil
}

// readableAlphabet leaves out 0/o/1/l/i: a senha inicial costuma ser lida de um log e digitada
// à mão, ou ditada para quem está instalando.
const readableAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// NewReadablePassword returns a random password like "k7f3-9qzp-2m4x-r8dw": 16 caracteres do
// alfabeto acima (≈79 bits) em grupos de quatro. Serve para a senha inicial do administrador
// quando ninguém escolheu uma — um padrão fixo como "admin" fica válido desde que o serviço sobe
// até alguém entrar pela primeira vez, e nesse intervalo quem chegar antes leva a conta.
func NewReadablePassword() (string, error) {
	const groups, per = 4, 4
	// Amostragem por rejeição: `byte % 31` daria a oito letras do alfabeto uma chance a mais que
	// às outras. O desvio é pequeno, mas não há motivo para aceitá-lo numa senha.
	const limit = 256 - 256%len(readableAlphabet)
	var sb strings.Builder
	buf := make([]byte, 1)
	for i := 0; i < groups*per; i++ {
		if i > 0 && i%per == 0 {
			sb.WriteByte('-')
		}
		for {
			if _, err := rand.Read(buf); err != nil {
				return "", err
			}
			if int(buf[0]) < limit {
				sb.WriteByte(readableAlphabet[int(buf[0])%len(readableAlphabet)])
				break
			}
		}
	}
	return sb.String(), nil
}

// HashPassword returns a PHC-formatted argon2id hash.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks pw against a PHC hash in constant time.
func VerifyPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var mem, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, mem, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
