package cryptochief

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// TONAddress is a parsed TON address. TON addresses come in three skins:
//
//   - User-friendly bounceable      "EQ…" (mainnet) / "kQ…" (testnet)
//   - User-friendly non-bounceable  "UQ…" (mainnet) / "0Q…" (testnet)
//   - Raw                            "<workchain>:<32-byte-hex-hash>"
//
// The user-friendly forms wrap the same 33 bytes (1 tag + 1 workchain +
// 32 hash) with a 2-byte CRC16-XMODEM checksum.
type TONAddress struct {
	Workchain  int8
	Hash       [32]byte
	Bounceable bool
	Testnet    bool
}

// ParseTONAddress accepts any of the three forms and returns a normalised
// TONAddress. CRC mismatches and length errors are caught.
func ParseTONAddress(s string) (TONAddress, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return TONAddress{}, errors.New("cryptochief/ton: empty address")
	}
	if i := strings.IndexByte(s, ':'); i > 0 {
		return parseRawTONAddress(s, i)
	}
	return parseUserFriendlyTONAddress(s)
}

func parseRawTONAddress(s string, colon int) (TONAddress, error) {
	wc, err := strconv.Atoi(s[:colon])
	if err != nil {
		return TONAddress{}, fmt.Errorf("cryptochief/ton: bad raw workchain %q", s[:colon])
	}
	if wc < -128 || wc > 127 {
		return TONAddress{}, fmt.Errorf("cryptochief/ton: workchain %d out of int8 range", wc)
	}
	hashHex := s[colon+1:]
	if len(hashHex) != 64 {
		return TONAddress{}, fmt.Errorf("cryptochief/ton: hash hex length %d, want 64", len(hashHex))
	}
	hashBytes, err := hex.DecodeString(hashHex)
	if err != nil {
		return TONAddress{}, fmt.Errorf("cryptochief/ton: bad hash hex: %w", err)
	}
	out := TONAddress{Workchain: int8(wc), Bounceable: true}
	copy(out.Hash[:], hashBytes)
	return out, nil
}

func parseUserFriendlyTONAddress(s string) (TONAddress, error) {
	if len(s) != 48 {
		return TONAddress{}, fmt.Errorf("cryptochief/ton: user-friendly address length %d, want 48", len(s))
	}
	// TON uses URL-safe base64 with no padding.
	raw, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(s)
	if err != nil {
		// Some wallets emit standard base64. Fall back.
		raw, err = base64.StdEncoding.WithPadding(base64.NoPadding).DecodeString(s)
		if err != nil {
			return TONAddress{}, fmt.Errorf("cryptochief/ton: base64 decode: %w", err)
		}
	}
	if len(raw) != 36 {
		return TONAddress{}, fmt.Errorf("cryptochief/ton: decoded length %d, want 36", len(raw))
	}
	want := crc16XMODEM(raw[:34])
	got := uint16(raw[34])<<8 | uint16(raw[35])
	if want != got {
		return TONAddress{}, errors.New("cryptochief/ton: CRC mismatch")
	}
	tag := raw[0]
	out := TONAddress{
		Workchain:  int8(raw[1]),
		Bounceable: tag&0x40 == 0,
		Testnet:    tag&0x80 != 0,
	}
	copy(out.Hash[:], raw[2:34])
	return out, nil
}

// String returns the user-friendly form (default: bounceable, mainnet —
// unless the address was constructed with those flags flipped).
func (a TONAddress) String() string {
	var tag byte = 0x11 // bounceable, mainnet
	if !a.Bounceable {
		tag = 0x51
	}
	if a.Testnet {
		tag |= 0x80
	}
	buf := make([]byte, 36)
	buf[0] = tag
	buf[1] = byte(a.Workchain)
	copy(buf[2:34], a.Hash[:])
	crc := crc16XMODEM(buf[:34])
	buf[34] = byte(crc >> 8)
	buf[35] = byte(crc)
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(buf)
}

// Raw returns the "workchain:hex" form.
func (a TONAddress) Raw() string {
	return fmt.Sprintf("%d:%s", a.Workchain, hex.EncodeToString(a.Hash[:]))
}

// IsZero reports whether the address is the unset zero value (used as a
// stand-in for "no address" in some message schemas).
func (a TONAddress) IsZero() bool {
	return a == TONAddress{}
}

// crc16XMODEM computes the CRC-16/XMODEM checksum (poly 0x1021, init 0x0000,
// non-reflected) used by TON to validate user-friendly addresses.
func crc16XMODEM(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
