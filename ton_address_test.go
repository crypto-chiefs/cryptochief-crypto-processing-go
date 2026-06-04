package cryptochief

import (
	"encoding/hex"
	"strings"
	"testing"
)

// TestParseTONAddress_RoundTrip — parse user-friendly → String() should
// regenerate the SAME user-friendly text.
func TestParseTONAddress_RoundTrip(t *testing.T) {
	const usdtMaster = "EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs"
	a, err := ParseTONAddress(usdtMaster)
	if err != nil {
		t.Fatal(err)
	}
	if a.Workchain != 0 {
		t.Errorf("workchain: %d, want 0", a.Workchain)
	}
	if !a.Bounceable {
		t.Error("EQ-prefixed should be bounceable")
	}
	if a.Testnet {
		t.Error("EQ-prefixed should NOT be testnet")
	}
	if out := a.String(); out != usdtMaster {
		t.Errorf("round-trip drift:\n in: %s\nout: %s", usdtMaster, out)
	}
}

// TestParseTONAddress_RawForm exercises the "workchain:hex" syntax.
func TestParseTONAddress_RawForm(t *testing.T) {
	const hashHex = "b113a994b5024a16719f69139328eb759596c38a25f59028b146fecdc3621dfe"
	a, err := ParseTONAddress("0:" + hashHex)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString(hashHex)
	for i := range a.Hash {
		if a.Hash[i] != want[i] {
			t.Errorf("hash byte %d: got %02x want %02x", i, a.Hash[i], want[i])
		}
	}
	if got := a.Raw(); got != "0:"+hashHex {
		t.Errorf("Raw(): got %s want 0:%s", got, hashHex)
	}
}

// TestParseTONAddress_UserFriendlyMatchesRaw — a user-friendly address
// and its raw form must produce the same Hash + Workchain.
func TestParseTONAddress_UserFriendlyMatchesRaw(t *testing.T) {
	const friendly = "EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs"
	a1, err := ParseTONAddress(friendly)
	if err != nil {
		t.Fatalf("user-friendly: %v", err)
	}
	raw := a1.Raw()
	a2, err := ParseTONAddress(raw)
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	if a1.Workchain != a2.Workchain || a1.Hash != a2.Hash {
		t.Errorf("user-friendly vs raw mismatch:\n friendly: %+v\n      raw: %+v", a1, a2)
	}
}

// TestParseTONAddress_UQNonBounceable — a UQ-prefixed address must come
// back with Bounceable=false and round-trip to itself.
func TestParseTONAddress_UQNonBounceable(t *testing.T) {
	// Same hash as the USDT master, just re-encoded with tag 0x51 (non-bounceable).
	a := TONAddress{Workchain: 0, Bounceable: false}
	copy(a.Hash[:], mustHex(t, "b113a994b5024a16719f69139328eb759596c38a25f59028b146fecdc3621dfe"))
	uq := a.String()
	if !strings.HasPrefix(uq, "UQ") {
		t.Fatalf("non-bounceable mainnet should start with UQ, got %s", uq)
	}
	back, err := ParseTONAddress(uq)
	if err != nil {
		t.Fatal(err)
	}
	if back.Bounceable {
		t.Error("UQ should decode as non-bounceable")
	}
	if back.Hash != a.Hash {
		t.Error("UQ hash drift")
	}
}

func TestParseTONAddress_Errors(t *testing.T) {
	for _, in := range []string{
		"",
		"not-an-address",
		"EQ_too_short",
		// 48 chars but corrupted CRC (last bytes intentionally wrong)
		"EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_AAA",
		// raw form with bad workchain
		"foo:b113a994b5024a16719f69139328eb759596c38a25f59028b146fecdc3621dfe",
		// raw form with bad hash length
		"0:abcd",
	} {
		if _, err := ParseTONAddress(in); err == nil {
			t.Errorf("ParseTONAddress(%q) should fail", in)
		}
	}
}

// TestCRC16XMODEM — sanity check the algorithm against the canonical test
// vector. "123456789" gives 0x31C3 under CRC-16/XMODEM.
func TestCRC16XMODEM(t *testing.T) {
	if got := crc16XMODEM([]byte("123456789")); got != 0x31C3 {
		t.Fatalf("CRC-16/XMODEM(\"123456789\") = %#04x, want 0x31C3", got)
	}
	if got := crc16XMODEM(nil); got != 0 {
		t.Errorf("CRC of empty input should be 0, got %#04x", got)
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("mustHex(%q): %v", s, err)
	}
	return b
}
