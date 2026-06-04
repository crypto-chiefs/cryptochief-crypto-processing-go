package cryptochief

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
)

// BorshValue is a value with explicit Borsh type information. Anchor program
// instructions are Borsh-encoded, but Borsh has no on-wire type tags — the
// caller and the program must agree on the layout. These constructors are
// the safe way to express that.
//
// Anchor data layout:  [discriminator: 8 bytes][args: Borsh-encoded]
type BorshValue struct {
	encoder func() ([]byte, error)
}

// Encode produces the on-wire bytes for v.
func (v BorshValue) Encode() ([]byte, error) {
	if v.encoder == nil {
		return nil, errors.New("cryptochief/anchor: nil BorshValue")
	}
	return v.encoder()
}

// BorshU8 wraps a single byte value.
func BorshU8(n uint8) BorshValue {
	return BorshValue{encoder: func() ([]byte, error) { return []byte{n}, nil }}
}

// BorshU16 wraps a little-endian u16.
func BorshU16(n uint16) BorshValue {
	return BorshValue{encoder: func() ([]byte, error) {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, n)
		return b, nil
	}}
}

// BorshU32 wraps a little-endian u32.
func BorshU32(n uint32) BorshValue {
	return BorshValue{encoder: func() ([]byte, error) {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, n)
		return b, nil
	}}
}

// BorshU64 wraps a little-endian u64.
func BorshU64(n uint64) BorshValue {
	return BorshValue{encoder: func() ([]byte, error) {
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, n)
		return b, nil
	}}
}

// BorshI8/16/32/64 wrap signed integers in two's-complement little-endian form.
func BorshI8(n int8) BorshValue   { return BorshU8(uint8(n)) }
func BorshI16(n int16) BorshValue { return BorshU16(uint16(n)) }
func BorshI32(n int32) BorshValue { return BorshU32(uint32(n)) }
func BorshI64(n int64) BorshValue { return BorshU64(uint64(n)) }

// BorshU128 wraps a 128-bit unsigned value (little-endian). n must be non-negative
// and < 2^128.
func BorshU128(n *big.Int) BorshValue {
	return BorshValue{encoder: func() ([]byte, error) {
		if n == nil {
			return make([]byte, 16), nil
		}
		if n.Sign() < 0 {
			return nil, fmt.Errorf("cryptochief/anchor: u128 negative")
		}
		max := new(big.Int).Lsh(big.NewInt(1), 128)
		if n.Cmp(max) >= 0 {
			return nil, fmt.Errorf("cryptochief/anchor: u128 overflow")
		}
		b := make([]byte, 16)
		bigBytes := n.Bytes()
		// Borsh is little-endian; big.Int.Bytes returns big-endian.
		for i, by := range bigBytes {
			b[len(bigBytes)-1-i] = by
		}
		return b, nil
	}}
}

// BorshBool wraps a 1-byte boolean (0x00 / 0x01).
func BorshBool(b bool) BorshValue {
	return BorshValue{encoder: func() ([]byte, error) {
		if b {
			return []byte{1}, nil
		}
		return []byte{0}, nil
	}}
}

// BorshString wraps a UTF-8 string as 4-byte little-endian length + bytes.
func BorshString(s string) BorshValue {
	return BorshValue{encoder: func() ([]byte, error) {
		buf := make([]byte, 4+len(s))
		binary.LittleEndian.PutUint32(buf[:4], uint32(len(s)))
		copy(buf[4:], s)
		return buf, nil
	}}
}

// BorshBytes wraps a raw byte slice — same wire form as BorshString.
func BorshBytes(b []byte) BorshValue {
	return BorshValue{encoder: func() ([]byte, error) {
		buf := make([]byte, 4+len(b))
		binary.LittleEndian.PutUint32(buf[:4], uint32(len(b)))
		copy(buf[4:], b)
		return buf, nil
	}}
}

// BorshFixedBytes wraps a fixed-length byte slice with NO length prefix —
// matches Anchor's `[u8; N]` layout. Length must equal n exactly.
func BorshFixedBytes(b []byte, n int) BorshValue {
	return BorshValue{encoder: func() ([]byte, error) {
		if len(b) != n {
			return nil, fmt.Errorf("cryptochief/anchor: BorshFixedBytes: expected %d bytes, got %d", n, len(b))
		}
		out := make([]byte, n)
		copy(out, b)
		return out, nil
	}}
}

// BorshPubkey wraps a Solana 32-byte address (base58 string or raw 32 bytes).
func BorshPubkey(pk any) BorshValue {
	return BorshValue{encoder: func() ([]byte, error) {
		raw, err := decodeSolanaPubkey(pk)
		if err != nil {
			return nil, err
		}
		return raw, nil
	}}
}

// BorshOption wraps a nullable value. nil → 0x00; non-nil → 0x01 + inner encoding.
func BorshOption(inner *BorshValue) BorshValue {
	return BorshValue{encoder: func() ([]byte, error) {
		if inner == nil {
			return []byte{0}, nil
		}
		body, err := inner.Encode()
		if err != nil {
			return nil, err
		}
		return append([]byte{1}, body...), nil
	}}
}

// BorshVec wraps a homogeneous slice (Borsh `Vec<T>` layout: 4-byte length +
// elements).
func BorshVec(items []BorshValue) BorshValue {
	return BorshValue{encoder: func() ([]byte, error) {
		out := make([]byte, 4)
		binary.LittleEndian.PutUint32(out, uint32(len(items)))
		for i, it := range items {
			b, err := it.Encode()
			if err != nil {
				return nil, fmt.Errorf("vec[%d]: %w", i, err)
			}
			out = append(out, b...)
		}
		return out, nil
	}}
}

// BorshStruct wraps a heterogeneous tuple — fields encoded in order with NO
// length prefix.
func BorshStruct(fields ...BorshValue) BorshValue {
	return BorshValue{encoder: func() ([]byte, error) {
		var out []byte
		for i, f := range fields {
			b, err := f.Encode()
			if err != nil {
				return nil, fmt.Errorf("field[%d]: %w", i, err)
			}
			out = append(out, b...)
		}
		return out, nil
	}}
}

// ─────────────────────────────────────────────────────────────────────────────
// Anchor instruction
// ─────────────────────────────────────────────────────────────────────────────

// AnchorDiscriminator returns the 8-byte instruction discriminator Anchor
// programs prepend to every instruction payload. The discriminator is the
// SHA-256 of "global:<method>"; only the first 8 bytes are kept.
//
//	disc := AnchorDiscriminator("initialize")
func AnchorDiscriminator(method string) [8]byte {
	sum := sha256.Sum256([]byte("global:" + method))
	var out [8]byte
	copy(out[:], sum[:8])
	return out
}

// EncodeAnchorInstruction builds the raw instruction data for an Anchor
// program method call: 8-byte discriminator followed by Borsh-encoded args.
//
//	data, _ := EncodeAnchorInstruction("transfer",
//	    cryptochief.BorshU64(1_000_000),
//	    cryptochief.BorshPubkey("Recipient1111…"),
//	)
func EncodeAnchorInstruction(method string, args ...BorshValue) ([]byte, error) {
	disc := AnchorDiscriminator(method)
	out := make([]byte, 8, 32)
	copy(out, disc[:])
	for i, a := range args {
		b, err := a.Encode()
		if err != nil {
			return nil, fmt.Errorf("anchor arg %d: %w", i, err)
		}
		out = append(out, b...)
	}
	return out, nil
}

// decodeSolanaPubkey accepts either a base58 string (the usual wire form) or
// a raw 32-byte slice and returns the canonical 32-byte representation.
func decodeSolanaPubkey(v any) ([]byte, error) {
	switch x := v.(type) {
	case []byte:
		if len(x) != 32 {
			return nil, fmt.Errorf("solana pubkey: want 32 bytes, got %d", len(x))
		}
		return x, nil
	case string:
		raw, err := base58Decode(x)
		if err != nil {
			return nil, fmt.Errorf("solana pubkey: bad base58: %w", err)
		}
		if len(raw) != 32 {
			return nil, fmt.Errorf("solana pubkey: decoded length %d, want 32", len(raw))
		}
		return raw, nil
	}
	return nil, fmt.Errorf("solana pubkey: unsupported type %T", v)
}
