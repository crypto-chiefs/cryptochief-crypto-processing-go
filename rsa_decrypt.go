package cryptochief

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

// ErrRSAKeyNotConfigured is returned by [WalletsService.DecryptPrivateKey]
// when the [Client] wasn't configured with [WithRSAPrivateKey] or
// [WithRSAPrivateKeyPEM] at construction time.
var ErrRSAKeyNotConfigured = errors.New("cryptochief: RSA private key not configured — pass WithRSAPrivateKey or WithRSAPrivateKeyPEM to New")

// LoadRSAPrivateKeyFile reads a PEM-encoded RSA private key from disk.
// Accepts both PKCS#1 (`BEGIN RSA PRIVATE KEY`) and PKCS#8
// (`BEGIN PRIVATE KEY`) formats — `openssl genrsa` emits PKCS#1 by
// default; PKCS#8 is the modern alternative.
func LoadRSAPrivateKeyFile(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cryptochief: read RSA key %q: %w", path, err)
	}
	return LoadRSAPrivateKeyPEM(data)
}

// LoadRSAPrivateKeyPEM parses a PEM-encoded RSA private key from memory.
// Accepts both PKCS#1 and PKCS#8.
func LoadRSAPrivateKeyPEM(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("cryptochief: RSA key: no PEM block found")
	}
	// PKCS#1: -----BEGIN RSA PRIVATE KEY-----
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	// PKCS#8: -----BEGIN PRIVATE KEY-----
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := k.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, fmt.Errorf("cryptochief: RSA key: PKCS#8 payload is %T, not *rsa.PrivateKey", k)
	}
	return nil, errors.New("cryptochief: RSA key: not a valid PKCS#1 or PKCS#8 RSA private key")
}

// DecryptRSAOAEP decrypts a single base64-encoded RSA-OAEP / SHA-256
// payload — the exact encoding the API uses for the
// `private_key_encrypted` field of a generated wallet. The returned
// string is the wallet's raw private key in the chain's hex format.
func DecryptRSAOAEP(priv *rsa.PrivateKey, base64Ciphertext string) (string, error) {
	if priv == nil {
		return "", ErrRSAKeyNotConfigured
	}
	ct, err := base64.StdEncoding.DecodeString(base64Ciphertext)
	if err != nil {
		return "", fmt.Errorf("cryptochief: RSA decrypt: bad base64: %w", err)
	}
	pt, err := rsa.DecryptOAEP(sha256.New(), nil, priv, ct, nil)
	if err != nil {
		return "", fmt.Errorf("cryptochief: RSA decrypt: %w", err)
	}
	return string(pt), nil
}
