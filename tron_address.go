package cryptochief

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// TronToHex converts a TRON base58 address ("T…") into the 0x41-prefixed
// 21-byte hex form. It validates the embedded SHA-256 double-hash
// checksum, so any typo or truncation is caught.
//
//	hexAddr, err := TronToHex("TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t")
//	// hexAddr == "0x41a614f803b6fd780986a42c78ec9c7f77e6ded13c"
func TronToHex(base58Addr string) (string, error) {
	decoded, err := base58Decode(strings.TrimSpace(base58Addr))
	if err != nil {
		return "", fmt.Errorf("cryptochief/tron: base58 decode: %w", err)
	}
	if len(decoded) != 25 {
		return "", fmt.Errorf("cryptochief/tron: decoded length %d, want 25", len(decoded))
	}
	payload, sum := decoded[:21], decoded[21:]
	if payload[0] != 0x41 {
		return "", fmt.Errorf("cryptochief/tron: leading byte 0x%02x, want 0x41", payload[0])
	}
	want := sha256d(payload)[:4]
	if !bytes.Equal(sum, want) {
		return "", errors.New("cryptochief/tron: checksum mismatch")
	}
	return "0x" + hex.EncodeToString(payload), nil
}

// HexToTron converts an EVM-style 20-byte hex address (or a TRON 0x41-prefixed
// 21-byte hex) into its base58 form. The 20-byte input is prefixed with 0x41
// automatically — that is how mainnet TRON addresses are encoded.
func HexToTron(hexAddr string) (string, error) {
	s := strings.TrimSpace(hexAddr)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	raw, err := hex.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("cryptochief/tron: bad hex %q: %w", hexAddr, err)
	}
	var payload []byte
	switch len(raw) {
	case 20:
		payload = make([]byte, 21)
		payload[0] = 0x41
		copy(payload[1:], raw)
	case 21:
		if raw[0] != 0x41 {
			return "", fmt.Errorf("cryptochief/tron: 21-byte input must start with 0x41, got 0x%02x", raw[0])
		}
		payload = raw
	default:
		return "", fmt.Errorf("cryptochief/tron: want 20- or 21-byte hex address, got %d bytes", len(raw))
	}
	sum := sha256d(payload)[:4]
	return base58Encode(append(payload, sum...)), nil
}

func sha256d(b []byte) []byte {
	h1 := sha256.Sum256(b)
	h2 := sha256.Sum256(h1[:])
	return h2[:]
}

// ─────────────────────────────────────────────────────────────────────────────
// Base58 (Bitcoin/Tron flavour — no separators, no version byte handling).
// ─────────────────────────────────────────────────────────────────────────────

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

var base58Map [128]int8

func init() {
	for i := range base58Map {
		base58Map[i] = -1
	}
	for i := 0; i < len(base58Alphabet); i++ {
		base58Map[base58Alphabet[i]] = int8(i)
	}
}

func base58Encode(b []byte) string {
	// Count leading zero bytes — they translate to leading '1' chars.
	zeros := 0
	for zeros < len(b) && b[zeros] == 0 {
		zeros++
	}
	num := new(big.Int).SetBytes(b)
	base := big.NewInt(58)
	mod := new(big.Int)

	var out []byte
	for num.Sign() > 0 {
		num.DivMod(num, base, mod)
		out = append(out, base58Alphabet[mod.Int64()])
	}
	// Append leading '1's.
	for i := 0; i < zeros; i++ {
		out = append(out, base58Alphabet[0])
	}
	// Reverse.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func base58Decode(s string) ([]byte, error) {
	if s == "" {
		return nil, errors.New("empty input")
	}
	// Count leading '1's → leading zero bytes.
	zeros := 0
	for zeros < len(s) && s[zeros] == base58Alphabet[0] {
		zeros++
	}
	num := new(big.Int)
	base := big.NewInt(58)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 128 || base58Map[c] < 0 {
			return nil, fmt.Errorf("invalid base58 char %q", c)
		}
		num.Mul(num, base)
		num.Add(num, big.NewInt(int64(base58Map[c])))
	}
	out := num.Bytes()
	// Re-add leading zeros.
	if zeros > 0 {
		padded := make([]byte, zeros+len(out))
		copy(padded[zeros:], out)
		out = padded
	}
	return out, nil
}
