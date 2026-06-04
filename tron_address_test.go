package cryptochief

import (
	"strings"
	"testing"
)

// TestTronAddressVectors uses well-known TRC-20 contract addresses with
// publicly-known hex equivalents to verify both directions of conversion.
func TestTronAddressVectors(t *testing.T) {
	cases := []struct {
		base58 string
		hex    string // 0x41-prefixed
	}{
		// USDT TRC-20 contract on TRON mainnet
		{"TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", "0x41a614f803b6fd780986a42c78ec9c7f77e6ded13c"},
		// SUN token (a real, publicly known contract)
		{"TSSMHYeV2uE9qYH95DqyoCuNCzEL1NvU3S", "0x41b4a428ab7092c2f1395f376ce297033b3bb446c1"},
	}
	for _, tc := range cases {
		got, err := TronToHex(tc.base58)
		if err != nil {
			t.Errorf("TronToHex(%q): %v", tc.base58, err)
			continue
		}
		if !strings.EqualFold(got, tc.hex) {
			t.Errorf("TronToHex(%q): got %s want %s", tc.base58, got, tc.hex)
		}

		back, err := HexToTron(tc.hex)
		if err != nil {
			t.Errorf("HexToTron(%q): %v", tc.hex, err)
			continue
		}
		if back != tc.base58 {
			t.Errorf("HexToTron(%q): got %s want %s", tc.hex, back, tc.base58)
		}

		// 20-byte form should also round-trip via the explicit 0x41 prefix.
		short := "0x" + tc.hex[4:] // strip "0x41"
		back2, err := HexToTron(short)
		if err != nil {
			t.Errorf("HexToTron(20-byte %q): %v", short, err)
			continue
		}
		if back2 != tc.base58 {
			t.Errorf("HexToTron(20-byte %q): got %s want %s", short, back2, tc.base58)
		}
	}
}

func TestTronAddress_BadChecksum(t *testing.T) {
	// Last char tweaked → checksum no longer matches.
	bad := "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6T" // capital T instead of t
	_, err := TronToHex(bad)
	if err == nil {
		t.Fatal("expected checksum failure")
	}
}

func TestTronAddress_Errors(t *testing.T) {
	for _, in := range []string{
		"",
		"not-base58-0OIl",
		"TR7NHqjeKQxGTCi", // truncated
	} {
		if _, err := TronToHex(in); err == nil {
			t.Errorf("TronToHex(%q) should fail", in)
		}
	}
	for _, in := range []string{
		"",
		"0xzzz",
		"0xabcd",                          // too short
		"0x42" + strings.Repeat("ab", 20), // wrong leading byte for 21-byte form
	} {
		if _, err := HexToTron(in); err == nil {
			t.Errorf("HexToTron(%q) should fail", in)
		}
	}
}

// Base58 round-trip property check on random-ish inputs. Empty input is
// explicitly out of scope — TRON addresses are always 25 bytes.
func TestBase58RoundTrip(t *testing.T) {
	for _, in := range [][]byte{
		{0x00},
		{0x00, 0x00, 0xff},
		{0x41, 0xa6, 0x14, 0xf8, 0x03, 0xb6, 0xfd, 0x78, 0x09, 0x86, 0xa4, 0x2c, 0x78, 0xec, 0x9c, 0x7f, 0x77, 0xe6, 0xde, 0xd1, 0x3c, 0xb8, 0x3a, 0xfd, 0x16},
	} {
		enc := base58Encode(in)
		dec, err := base58Decode(enc)
		if err != nil {
			t.Fatalf("decode %q: %v", enc, err)
		}
		if len(dec) != len(in) {
			t.Errorf("length drift: %d → %d", len(in), len(dec))
			continue
		}
		for i := range in {
			if in[i] != dec[i] {
				t.Errorf("byte %d: in=%02x dec=%02x", i, in[i], dec[i])
			}
		}
	}
}
