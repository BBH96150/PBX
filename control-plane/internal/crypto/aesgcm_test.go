package crypto

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func newTestSealer(t *testing.T) *Sealer {
	t.Helper()
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	t.Setenv("TOTP_ENCRYPTION_KEY", key)
	s, err := NewSealerFromEnv()
	if err != nil {
		t.Fatalf("NewSealerFromEnv: %v", err)
	}
	if s == nil {
		t.Fatal("expected a sealer, got nil")
	}
	return s
}

func TestGenerateKeyIs32Bytes(t *testing.T) {
	k, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(k)
	if err != nil {
		t.Fatalf("not base64: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("want 32 bytes, got %d", len(raw))
	}
	// Two calls must differ (randomness).
	k2, _ := GenerateKey()
	if k == k2 {
		t.Fatal("GenerateKey returned identical keys")
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	s := newTestSealer(t)
	plain := []byte("JBSWY3DPEHPK3PXP") // a TOTP secret
	ct, nonce, err := s.Seal(plain)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Equal(ct, plain) {
		t.Fatal("ciphertext equals plaintext")
	}
	got, err := s.Open(ct, nonce)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("round-trip mismatch: %q != %q", got, plain)
	}
}

func TestSealUsesFreshNonce(t *testing.T) {
	s := newTestSealer(t)
	_, n1, _ := s.Seal([]byte("x"))
	_, n2, _ := s.Seal([]byte("x"))
	if bytes.Equal(n1, n2) {
		t.Fatal("nonce reused across Seal calls")
	}
}

func TestOpenTamperedFails(t *testing.T) {
	s := newTestSealer(t)
	ct, nonce, _ := s.Seal([]byte("secret"))
	ct[0] ^= 0xff // flip a bit
	if _, err := s.Open(ct, nonce); err == nil {
		t.Fatal("expected auth failure on tampered ciphertext")
	}
}

func TestNilSealerReturnsErrNoKey(t *testing.T) {
	var s *Sealer
	if _, _, err := s.Seal([]byte("x")); err != ErrNoKey {
		t.Fatalf("Seal on nil: want ErrNoKey, got %v", err)
	}
	if _, err := s.Open([]byte("x"), []byte("y")); err != ErrNoKey {
		t.Fatalf("Open on nil: want ErrNoKey, got %v", err)
	}
}

func TestNewSealerFromEnv(t *testing.T) {
	t.Run("missing key → nil,nil", func(t *testing.T) {
		t.Setenv("TOTP_ENCRYPTION_KEY", "")
		s, err := NewSealerFromEnv()
		if err != nil || s != nil {
			t.Fatalf("want nil,nil; got %v,%v", s, err)
		}
	})
	t.Run("invalid base64 → error", func(t *testing.T) {
		t.Setenv("TOTP_ENCRYPTION_KEY", "not!base64!")
		if _, err := NewSealerFromEnv(); err == nil {
			t.Fatal("expected error for invalid base64")
		}
	})
	t.Run("wrong length → error", func(t *testing.T) {
		t.Setenv("TOTP_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte("tooshort")))
		if _, err := NewSealerFromEnv(); err == nil {
			t.Fatal("expected error for non-32-byte key")
		}
	})
}
