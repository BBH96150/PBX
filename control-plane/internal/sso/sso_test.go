package sso

import "testing"

// RFC 7636 Appendix B test vector for the S256 code-challenge method.
func TestPKCES256RFCVector(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := pkceS256(verifier); got != want {
		t.Fatalf("pkceS256 = %q, want %q (RFC 7636)", got, want)
	}
}

func TestRandomLengthAndUniqueness(t *testing.T) {
	// base64url(no pad) of n bytes = ceil(n*4/3) chars.
	if got := len(random(32)); got != 43 {
		t.Errorf("random(32) len = %d, want 43", got)
	}
	if got := len(random(64)); got != 86 {
		t.Errorf("random(64) len = %d, want 86", got)
	}
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		s := NewState()
		if seen[s] {
			t.Fatal("NewState collision within 100 draws")
		}
		seen[s] = true
	}
}

func TestNewPKCEVerifierMeetsMinLength(t *testing.T) {
	// RFC 7636 requires 43..128 chars.
	if got := len(NewPKCEVerifier()); got < 43 || got > 128 {
		t.Errorf("verifier length %d out of RFC range 43..128", got)
	}
}

func TestSelfSignedSAMLKeypairIsValidPEM(t *testing.T) {
	cert, key, err := GenerateSelfSignedSAMLKeypair("sp.example.com", 0)
	if err != nil {
		t.Fatalf("GenerateSelfSignedSAMLKeypair: %v", err)
	}
	if len(cert) == 0 || len(key) == 0 {
		t.Fatal("empty cert or key")
	}
	if want := "BEGIN CERTIFICATE"; !contains(cert, want) {
		t.Errorf("cert PEM missing %q", want)
	}
	if !contains(key, "PRIVATE KEY") {
		t.Errorf("key PEM missing PRIVATE KEY block")
	}
}

func contains(b []byte, sub string) bool {
	return len(b) >= len(sub) && indexOf(string(b), sub) >= 0
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
