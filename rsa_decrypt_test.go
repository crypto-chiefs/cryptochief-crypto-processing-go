package cryptochief

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// encryptOAEP is a tiny mirror of what the API does on the server side:
// take a hex-string plaintext, RSA-OAEP/SHA-256 encrypt it with the
// public key, base64-encode the result. Used to set up round-trip tests
// without needing any network access.
func encryptOAEP(t *testing.T, pub *rsa.PublicKey, plaintext string) string {
	t.Helper()
	ct, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pub, []byte(plaintext), nil)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(ct)
}

func generateRSAKeyForTest(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func writePEM(t *testing.T, k *rsa.PrivateKey, pkcs8 bool) (path string, raw []byte) {
	t.Helper()
	var block *pem.Block
	if pkcs8 {
		der, err := x509.MarshalPKCS8PrivateKey(k)
		if err != nil {
			t.Fatal(err)
		}
		block = &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	} else {
		block = &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)}
	}
	raw = pem.EncodeToMemory(block)
	path = filepath.Join(t.TempDir(), "rsa.pem")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path, raw
}

// TestDecryptRSAOAEP_RoundTrip — encrypt a known plaintext with the test
// public key, decrypt with the SDK helper, expect the original bytes back.
func TestDecryptRSAOAEP_RoundTrip(t *testing.T) {
	priv := generateRSAKeyForTest(t)
	const plaintext = "0123456789abcdef" // pretend wallet private key (hex)
	ct := encryptOAEP(t, &priv.PublicKey, plaintext)
	got, err := DecryptRSAOAEP(priv, ct)
	if err != nil {
		t.Fatal(err)
	}
	if got != plaintext {
		t.Errorf("round-trip: got %q want %q", got, plaintext)
	}
}

func TestLoadRSAPrivateKeyFile_PKCS1(t *testing.T) {
	priv := generateRSAKeyForTest(t)
	path, _ := writePEM(t, priv, false)
	loaded, err := LoadRSAPrivateKeyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.N.Cmp(priv.N) != 0 {
		t.Error("modulus mismatch")
	}
}

func TestLoadRSAPrivateKeyFile_PKCS8(t *testing.T) {
	priv := generateRSAKeyForTest(t)
	path, _ := writePEM(t, priv, true)
	loaded, err := LoadRSAPrivateKeyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.N.Cmp(priv.N) != 0 {
		t.Error("modulus mismatch")
	}
}

func TestLoadRSAPrivateKeyPEM_BadInput(t *testing.T) {
	for _, in := range [][]byte{
		nil,
		[]byte(""),
		[]byte("not a PEM"),
		// Valid PEM but wrong type
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("garbage")}),
	} {
		if _, err := LoadRSAPrivateKeyPEM(in); err == nil {
			t.Errorf("expected error for input %q", in)
		}
	}
}

// TestWalletsService_DecryptPrivateKey_NoKey — calling without an RSA
// key configured must return the sentinel error, not a panic or empty
// string.
func TestWalletsService_DecryptPrivateKey_NoKey(t *testing.T) {
	c, _ := New("m", "k")
	_, err := c.Wallets.DecryptPrivateKey("anything")
	if !errors.Is(err, ErrRSAKeyNotConfigured) {
		t.Fatalf("want ErrRSAKeyNotConfigured, got %v", err)
	}
}

// TestWalletsService_DecryptPrivateKey_FromFile — load the RSA key
// from a temp PEM, then decrypt a payload through the public client
// method.
func TestWalletsService_DecryptPrivateKey_FromFile(t *testing.T) {
	priv := generateRSAKeyForTest(t)
	path, _ := writePEM(t, priv, false)
	c, err := New("m", "k", WithRSAPrivateKey(path))
	if err != nil {
		t.Fatal(err)
	}
	const plaintext = "deadbeef"
	got, err := c.Wallets.DecryptPrivateKey(encryptOAEP(t, &priv.PublicKey, plaintext))
	if err != nil {
		t.Fatal(err)
	}
	if got != plaintext {
		t.Errorf("got %q want %q", got, plaintext)
	}
}

// TestWalletsService_DecryptPrivateKey_FromPEMBytes — same but with the
// PEM bytes already in memory (vault / env / secret manager path).
func TestWalletsService_DecryptPrivateKey_FromPEMBytes(t *testing.T) {
	priv := generateRSAKeyForTest(t)
	_, raw := writePEM(t, priv, false)
	c, err := New("m", "k", WithRSAPrivateKeyPEM(raw))
	if err != nil {
		t.Fatal(err)
	}
	const plaintext = "feedfacecafebeef"
	got, err := c.Wallets.DecryptPrivateKey(encryptOAEP(t, &priv.PublicKey, plaintext))
	if err != nil {
		t.Fatal(err)
	}
	if got != plaintext {
		t.Errorf("got %q want %q", got, plaintext)
	}
}

// TestWalletsService_DecryptPrivateKey_BadKey — a malformed PEM should
// surface on the first decrypt attempt with a clear error message, not
// at New time (which would surprise callers who load credentials lazily).
func TestWalletsService_DecryptPrivateKey_BadKey(t *testing.T) {
	c, err := New("m", "k", WithRSAPrivateKeyPEM([]byte("not a PEM")))
	if err != nil {
		t.Fatalf("New should not fail on bad PEM (deferred), got %v", err)
	}
	_, err = c.Wallets.DecryptPrivateKey("anything")
	if err == nil {
		t.Fatal("expected error from deferred init")
	}
	if !strings.Contains(err.Error(), "PEM") {
		t.Errorf("expected PEM-related error, got %v", err)
	}
}
