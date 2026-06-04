package cryptochief

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"testing"
)

// TestAnchorDiscriminator verifies the SHA-256("global:"+method)[:8] formula
// the Anchor framework uses. We compute the expected value from primitives
// to avoid hard-coding a magic constant.
func TestAnchorDiscriminator(t *testing.T) {
	for _, m := range []string{"initialize", "transfer", "swap", "set_authority"} {
		want := sha256.Sum256([]byte("global:" + m))
		got := AnchorDiscriminator(m)
		for i := 0; i < 8; i++ {
			if got[i] != want[i] {
				t.Errorf("%q discriminator byte %d: got %02x want %02x", m, i, got[i], want[i])
			}
		}
	}
}

func TestBorshU64(t *testing.T) {
	val := uint64(1_234_567)
	b, err := BorshU64(val).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 8 {
		t.Fatalf("u64 length: %d", len(b))
	}
	if got := binary.LittleEndian.Uint64(b); got != val {
		t.Errorf("u64: got %d want %d", got, val)
	}
}

func TestBorshU128(t *testing.T) {
	// 1 << 64 — fits in u128 but not in u64.
	n := new(big.Int).Lsh(big.NewInt(1), 64)
	b, err := BorshU128(n).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 16 {
		t.Fatalf("u128 length: %d", len(b))
	}
	// Low 8 bytes zero, byte 8 = 0x01.
	for i := 0; i < 8; i++ {
		if b[i] != 0 {
			t.Errorf("u128 LE low %d: got %02x", i, b[i])
		}
	}
	if b[8] != 0x01 {
		t.Errorf("u128 LE byte 8: got %02x want 01", b[8])
	}
}

func TestBorshString(t *testing.T) {
	b, err := BorshString("hello").Encode()
	if err != nil {
		t.Fatal(err)
	}
	// 4-byte LE length + "hello"
	if hex.EncodeToString(b) != "0500000068656c6c6f" {
		t.Fatalf("got %s", hex.EncodeToString(b))
	}
}

func TestBorshBool(t *testing.T) {
	tr, _ := BorshBool(true).Encode()
	fl, _ := BorshBool(false).Encode()
	if hex.EncodeToString(tr) != "01" {
		t.Errorf("true: %s", hex.EncodeToString(tr))
	}
	if hex.EncodeToString(fl) != "00" {
		t.Errorf("false: %s", hex.EncodeToString(fl))
	}
}

func TestBorshVec(t *testing.T) {
	// Vec<u32> = [1, 2, 3]: 4-byte LE len + 3 × 4-byte LE u32
	b, err := BorshVec([]BorshValue{BorshU32(1), BorshU32(2), BorshU32(3)}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	want := "03000000" + "01000000" + "02000000" + "03000000"
	if hex.EncodeToString(b) != want {
		t.Fatalf("got %s want %s", hex.EncodeToString(b), want)
	}
}

func TestBorshPubkey(t *testing.T) {
	// System program pubkey — well-known all-zero 32 bytes encoded base58 = "11111111111111111111111111111111".
	const systemProgram = "11111111111111111111111111111111"
	b, err := BorshPubkey(systemProgram).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 32 {
		t.Fatalf("len %d", len(b))
	}
	for i, by := range b {
		if by != 0 {
			t.Errorf("byte %d: got %02x", i, by)
		}
	}
}

func TestEncodeAnchorInstruction(t *testing.T) {
	data, err := EncodeAnchorInstruction("transfer",
		BorshU64(1_000),
		BorshBool(true),
	)
	if err != nil {
		t.Fatal(err)
	}
	// First 8 bytes — discriminator.
	disc := AnchorDiscriminator("transfer")
	for i := 0; i < 8; i++ {
		if data[i] != disc[i] {
			t.Errorf("disc byte %d", i)
		}
	}
	// u64 1000 in LE = e8 03 00 00 00 00 00 00
	if hex.EncodeToString(data[8:16]) != "e803000000000000" {
		t.Errorf("u64: %s", hex.EncodeToString(data[8:16]))
	}
	// bool true
	if data[16] != 0x01 {
		t.Errorf("bool byte: %02x", data[16])
	}
	if len(data) != 17 {
		t.Errorf("total len: %d", len(data))
	}
}

func TestBorshFixedBytes(t *testing.T) {
	b, err := BorshFixedBytes([]byte{1, 2, 3, 4}, 4).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(b) != "01020304" {
		t.Errorf("got %s", hex.EncodeToString(b))
	}
	if _, err := BorshFixedBytes([]byte{1, 2, 3}, 4).Encode(); err == nil {
		t.Error("expected length error")
	}
}

func TestBorshOption(t *testing.T) {
	none, _ := BorshOption(nil).Encode()
	if hex.EncodeToString(none) != "00" {
		t.Errorf("none: %s", hex.EncodeToString(none))
	}
	inner := BorshU32(42)
	some, _ := BorshOption(&inner).Encode()
	if hex.EncodeToString(some) != "012a000000" {
		t.Errorf("some: %s", hex.EncodeToString(some))
	}
}
