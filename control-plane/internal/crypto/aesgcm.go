// Package crypto wraps AES-GCM with a process-wide key loaded from env.
//
// Used by 2FA enrollment to seal TOTP secrets before they hit the DB. The
// design choice: a leaked DB dump alone shouldn't yield working codes — an
// attacker also needs TOTP_ENCRYPTION_KEY (typically held in secrets manager
// and only ever materialized in the control-plane process env).
//
// Key rotation: not implemented here. Rotating the key requires an offline
// re-encrypt pass over user_2fa_methods. Until that script exists, treat the
// key as permanent.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
)

// Sealer is constructed once from the env key and reused for every
// seal/open call.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealerFromEnv loads TOTP_ENCRYPTION_KEY (base64-encoded 32 bytes).
// Returns nil + nil error if the var isn't set — callers decide whether to
// continue. The 2FA wiring in main.go gates startup if there are existing
// enrolled methods and no key is set.
func NewSealerFromEnv() (*Sealer, error) {
	raw := os.Getenv("TOTP_ENCRYPTION_KEY")
	if raw == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("TOTP_ENCRYPTION_KEY: not valid base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("TOTP_ENCRYPTION_KEY: expected 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal returns (ciphertext, nonce). Each call uses a fresh random nonce.
func (s *Sealer) Seal(plaintext []byte) (ct, nonce []byte, err error) {
	if s == nil {
		return nil, nil, ErrNoKey
	}
	nonce = make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	ct = s.aead.Seal(nil, nonce, plaintext, nil)
	return ct, nonce, nil
}

// Open reverses Seal.
func (s *Sealer) Open(ct, nonce []byte) ([]byte, error) {
	if s == nil {
		return nil, ErrNoKey
	}
	return s.aead.Open(nil, nonce, ct, nil)
}

// GenerateKey emits a freshly-generated 32-byte base64 string. Useful for
// a one-time `go run ./cmd/genkey` or similar bootstrap command. Not called
// by the server itself.
func GenerateKey() (string, error) {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(k), nil
}

// ErrNoKey is returned when seal/open is invoked on a nil sealer (i.e. the
// env key was never set). Callers should surface this as a user-facing
// "2FA is disabled in this deployment" message.
var ErrNoKey = errors.New("TOTP_ENCRYPTION_KEY not set; 2FA disabled")
