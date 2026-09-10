package auth

import (
	"testing"
	"time"
)

func TestHOTPVectors(t *testing.T) {
	// RFC 4226 apêndice D: segredo "12345678901234567890"
	secret := b32.EncodeToString([]byte("12345678901234567890"))
	for i, want := range []string{"755224", "287082", "359152", "969429", "338314"} {
		got, err := hotp(secret, uint64(i))
		if err != nil || got != want {
			t.Fatalf("counter %d: %s %v (want %s)", i, got, err, want)
		}
	}
}

func TestVerifyTOTPWindowAndReplay(t *testing.T) {
	secret, _ := NewTOTPSecret()
	now := time.Unix(1_700_000_000, 0)
	code, _ := hotp(secret, TOTPCounter(now))
	ok, c := VerifyTOTP(secret, code, now, 0)
	if !ok || c != TOTPCounter(now) {
		t.Fatal("current code rejected")
	}
	if ok, _ := VerifyTOTP(secret, code, now, c); ok {
		t.Fatal("replay accepted")
	}
	prev, _ := hotp(secret, TOTPCounter(now)-1)
	if ok, _ := VerifyTOTP(secret, prev, now, 0); !ok {
		t.Fatal("previous interval rejected")
	}
	old, _ := hotp(secret, TOTPCounter(now)-2)
	if ok, _ := VerifyTOTP(secret, old, now, 0); ok {
		t.Fatal("stale code accepted")
	}
	if ok, _ := VerifyTOTP(secret, "12345", now, 0); ok {
		t.Fatal("short code accepted")
	}
}

func TestRecoveryAndSeal(t *testing.T) {
	codes, hashes, err := NewRecoveryCodes()
	if err != nil || len(codes) != 10 {
		t.Fatal(err)
	}
	rest, ok := UseRecoveryCode(hashes, "  "+codes[3]+" ")
	if !ok || len(rest) != 9 {
		t.Fatal("recovery code not accepted")
	}
	if _, ok := UseRecoveryCode(rest, codes[3]); ok {
		t.Fatal("recovery code reused")
	}
	key := make([]byte, 32)
	sealed, err := Seal(key, "SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if pt, err := Open(key, sealed); err != nil || pt != "SECRET" {
		t.Fatal(pt, err)
	}
	key[0] ^= 1
	if _, err := Open(key, sealed); err == nil {
		t.Fatal("opened with wrong key")
	}
}
