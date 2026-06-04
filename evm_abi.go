package cryptochief

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"golang.org/x/crypto/sha3"
)

// EncodeEVMCall builds calldata for a Solidity-style function call. The
// signature is the canonical form Solidity itself hashes for the selector
// — "name(type1,type2,...)" with NO spaces, NO parameter names.
//
// Examples:
//
//	// ERC-20 transfer(address,uint256)
//	data, _ := EncodeEVMCall("transfer(address,uint256)",
//	    "0xRecipient...", big.NewInt(1000))
//
//	// Uniswap V2 swapExactTokensForTokens(uint256,uint256,address[],address,uint256)
//	data, _ := EncodeEVMCall(
//	    "swapExactTokensForTokens(uint256,uint256,address[],address,uint256)",
//	    amountIn, amountOutMin, path, to, deadline)
//
// Supported types:
//
//   - uintM, intM   (M ∈ {8,16,32,64,128,256}; bare "uint"/"int" alias to 256)
//   - address       (accepts 0x… hex, 0x41-prefixed TRON hex, or T… base58)
//   - bool
//   - bytes, bytesN (N ∈ 1..32)
//   - string
//   - T[]           (dynamic array of any supported T)
//   - T[N]          (fixed array of any supported T)
//
// Argument value forms accepted per type:
//
//   - integers: *big.Int, int / int8..int64, uint / uint8..uint64, string
//     (decimal, optional "0x" hex prefix)
//   - address:  string
//   - bool:     bool
//   - bytes:    []byte, string (raw or "0x"-prefixed hex)
//   - bytesN:   same as bytes; length must equal N
//   - string:   string
//   - arrays:   []any, []string, []*big.Int, []uint64, ...
//
// Returned bytes are exactly the calldata to put in the contract-call's
// data field (hex-encoded for the API).
func EncodeEVMCall(signature string, args ...any) ([]byte, error) {
	_, argTypes, err := parseSignature(signature)
	if err != nil {
		return nil, err
	}
	if len(argTypes) != len(args) {
		return nil, fmt.Errorf("cryptochief/evm: signature has %d args, got %d", len(argTypes), len(args))
	}
	selector := evmSelector(signature)
	body, err := encodeABITuple(argTypes, args)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 4+len(body))
	out = append(out, selector[:]...)
	out = append(out, body...)
	return out, nil
}

// EncodeEVMCallHex is the convenience hex string form ("0x…") that the
// Crypto Chief contract-call ContractCall.Data field expects.
func EncodeEVMCallHex(signature string, args ...any) (string, error) {
	b, err := EncodeEVMCall(signature, args...)
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(b), nil
}

// EVMSelector returns the 4-byte function selector for a Solidity signature.
// Useful for debugging and for hand-crafting calldata.
func EVMSelector(signature string) [4]byte {
	return evmSelector(signature)
}

func evmSelector(sig string) [4]byte {
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(canonicalSig(sig)))
	sum := h.Sum(nil)
	var out [4]byte
	copy(out[:], sum[:4])
	return out
}

// canonicalSig strips spaces and parameter names from a signature, keeping
// only the form keccak hashes against.
func canonicalSig(sig string) string {
	open := strings.IndexByte(sig, '(')
	close := strings.LastIndexByte(sig, ')')
	if open < 0 || close < 0 || close < open {
		return strings.ReplaceAll(sig, " ", "")
	}
	name := strings.TrimSpace(sig[:open])
	args := strings.TrimSpace(sig[open+1 : close])
	parts := strings.Split(args, ",")
	for i, p := range parts {
		// "uint256 amount" → "uint256"; "address[] memory path" → "address[]"
		p = strings.TrimSpace(p)
		if sp := strings.IndexByte(p, ' '); sp >= 0 {
			p = strings.TrimSpace(p[:sp])
		}
		parts[i] = expandAlias(p)
	}
	return name + "(" + strings.Join(parts, ",") + ")"
}

// expandAlias turns "uint" → "uint256" and similar shorthand into the
// canonical form Solidity uses for selector hashing.
func expandAlias(t string) string {
	// Arrays — recurse on element type.
	if i := strings.LastIndexByte(t, '['); i > 0 {
		return expandAlias(t[:i]) + t[i:]
	}
	switch t {
	case "uint":
		return "uint256"
	case "int":
		return "int256"
	case "byte":
		return "bytes1"
	}
	return t
}

func parseSignature(sig string) (name string, types []string, err error) {
	open := strings.IndexByte(sig, '(')
	close := strings.LastIndexByte(sig, ')')
	if open < 0 || close < 0 || close < open {
		return "", nil, fmt.Errorf("cryptochief/evm: bad signature %q", sig)
	}
	name = strings.TrimSpace(sig[:open])
	if name == "" {
		return "", nil, fmt.Errorf("cryptochief/evm: signature missing name")
	}
	body := strings.TrimSpace(sig[open+1 : close])
	if body == "" {
		return name, nil, nil
	}
	parts := strings.Split(body, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if sp := strings.IndexByte(p, ' '); sp >= 0 {
			p = strings.TrimSpace(p[:sp])
		}
		types = append(types, expandAlias(p))
	}
	return name, types, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Type system
// ─────────────────────────────────────────────────────────────────────────────

// abiType is the parsed form of one ABI type spec.
type abiType struct {
	kind    string // "uint", "int", "address", "bool", "bytes", "string", "bytesN", "array"
	size    int    // bits for uint/int; byte length for bytesN; element count for fixed arrays (-1 = dynamic)
	element *abiType
}

func parseType(t string) (*abiType, error) {
	t = strings.TrimSpace(t)
	if t == "" {
		return nil, errors.New("empty type")
	}
	// Array suffix? "[K]" or "[]"
	if t[len(t)-1] == ']' {
		open := strings.LastIndexByte(t, '[')
		if open < 0 {
			return nil, fmt.Errorf("malformed type %q", t)
		}
		inner, err := parseType(t[:open])
		if err != nil {
			return nil, err
		}
		size := -1 // dynamic
		if span := t[open+1 : len(t)-1]; span != "" {
			n, err := strconv.Atoi(span)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("bad array size %q in %q", span, t)
			}
			size = n
		}
		return &abiType{kind: "array", size: size, element: inner}, nil
	}
	switch {
	case strings.HasPrefix(t, "uint"):
		bits, err := parseIntBits(t[4:], "uint")
		if err != nil {
			return nil, err
		}
		return &abiType{kind: "uint", size: bits}, nil
	case strings.HasPrefix(t, "int"):
		bits, err := parseIntBits(t[3:], "int")
		if err != nil {
			return nil, err
		}
		return &abiType{kind: "int", size: bits}, nil
	case t == "address":
		return &abiType{kind: "address"}, nil
	case t == "bool":
		return &abiType{kind: "bool"}, nil
	case t == "string":
		return &abiType{kind: "string"}, nil
	case t == "bytes":
		return &abiType{kind: "bytes"}, nil
	case strings.HasPrefix(t, "bytes"):
		n, err := strconv.Atoi(t[5:])
		if err != nil || n < 1 || n > 32 {
			return nil, fmt.Errorf("invalid fixed bytes type %q", t)
		}
		return &abiType{kind: "bytesN", size: n}, nil
	}
	return nil, fmt.Errorf("unsupported type %q", t)
}

func parseIntBits(s, kind string) (int, error) {
	if s == "" {
		return 256, nil
	}
	bits, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid %s width %q", kind, s)
	}
	if bits <= 0 || bits > 256 || bits%8 != 0 {
		return 0, fmt.Errorf("invalid %s width %d", kind, bits)
	}
	return bits, nil
}

func (t *abiType) isDynamic() bool {
	switch t.kind {
	case "bytes", "string":
		return true
	case "array":
		if t.size < 0 {
			return true
		}
		return t.element.isDynamic()
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Encoding
// ─────────────────────────────────────────────────────────────────────────────

func encodeABITuple(typeSpecs []string, args []any) ([]byte, error) {
	types := make([]*abiType, len(typeSpecs))
	for i, s := range typeSpecs {
		t, err := parseType(s)
		if err != nil {
			return nil, fmt.Errorf("arg %d (%s): %w", i, s, err)
		}
		types[i] = t
	}
	return encodeABIComponents(types, args)
}

// encodeABIComponents is the head/tail packer used for both top-level tuples
// and dynamic arrays of dynamic types.
func encodeABIComponents(types []*abiType, args []any) ([]byte, error) {
	// First pass: encode each component to its tail bytes.
	tails := make([][]byte, len(types))
	for i, t := range types {
		b, err := encodeOne(t, args[i])
		if err != nil {
			return nil, fmt.Errorf("arg %d: %w", i, err)
		}
		tails[i] = b
	}

	// Head size = 32 bytes per component (offset or inline value).
	headSize := 32 * len(types)
	// Compute offsets for dynamic tails.
	offsets := make([]int, len(types))
	cursor := headSize
	for i, t := range types {
		if t.isDynamic() {
			offsets[i] = cursor
			cursor += len(tails[i])
		}
	}

	out := make([]byte, 0, cursor)
	// Heads.
	for i, t := range types {
		if t.isDynamic() {
			out = append(out, uint256Bytes(big.NewInt(int64(offsets[i])))...)
		} else {
			out = append(out, tails[i]...)
		}
	}
	// Tails.
	for i, t := range types {
		if t.isDynamic() {
			out = append(out, tails[i]...)
		}
	}
	return out, nil
}

func encodeOne(t *abiType, v any) ([]byte, error) {
	switch t.kind {
	case "uint":
		n, err := toBigUint(v, t.size)
		if err != nil {
			return nil, err
		}
		return uint256Bytes(n), nil
	case "int":
		n, err := toBigInt(v, t.size)
		if err != nil {
			return nil, err
		}
		return int256Bytes(n), nil
	case "address":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("address: want string, got %T", v)
		}
		addrBytes, err := normaliseEVMAddress(s)
		if err != nil {
			return nil, err
		}
		out := make([]byte, 32)
		copy(out[12:], addrBytes)
		return out, nil
	case "bool":
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("bool: want bool, got %T", v)
		}
		out := make([]byte, 32)
		if b {
			out[31] = 1
		}
		return out, nil
	case "bytesN":
		b, err := toBytes(v)
		if err != nil {
			return nil, err
		}
		if len(b) != t.size {
			return nil, fmt.Errorf("bytes%d: expected %d bytes, got %d", t.size, t.size, len(b))
		}
		out := make([]byte, 32)
		copy(out, b)
		return out, nil
	case "bytes":
		b, err := toBytes(v)
		if err != nil {
			return nil, err
		}
		return encodeDynBytes(b), nil
	case "string":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("string: want string, got %T", v)
		}
		return encodeDynBytes([]byte(s)), nil
	case "array":
		items, err := toAnySlice(v)
		if err != nil {
			return nil, err
		}
		if t.size >= 0 && len(items) != t.size {
			return nil, fmt.Errorf("fixed array T[%d]: expected %d items, got %d", t.size, t.size, len(items))
		}
		// Build element types for the inner packer.
		inner := make([]*abiType, len(items))
		for i := range items {
			inner[i] = t.element
		}
		body, err := encodeABIComponents(inner, items)
		if err != nil {
			return nil, err
		}
		if t.size < 0 {
			// Dynamic array: prepend length.
			out := make([]byte, 0, 32+len(body))
			out = append(out, uint256Bytes(big.NewInt(int64(len(items))))...)
			out = append(out, body...)
			return out, nil
		}
		return body, nil
	}
	return nil, fmt.Errorf("unsupported kind %q", t.kind)
}

func encodeDynBytes(b []byte) []byte {
	out := make([]byte, 0, 32+roundUp32(len(b)))
	out = append(out, uint256Bytes(big.NewInt(int64(len(b))))...)
	out = append(out, b...)
	if pad := roundUp32(len(b)) - len(b); pad > 0 {
		out = append(out, make([]byte, pad)...)
	}
	return out
}

func roundUp32(n int) int {
	r := n % 32
	if r == 0 {
		return n
	}
	return n + 32 - r
}

// ─────────────────────────────────────────────────────────────────────────────
// Value coercion
// ─────────────────────────────────────────────────────────────────────────────

func uint256Bytes(n *big.Int) []byte {
	out := make([]byte, 32)
	if n == nil || n.Sign() == 0 {
		return out
	}
	if n.Sign() < 0 {
		// Caller asked for an unsigned slot but supplied a negative — wrap
		// using two's-complement 256-bit semantics.
		two256 := new(big.Int).Lsh(big.NewInt(1), 256)
		n = new(big.Int).Add(n, two256)
	}
	b := n.Bytes()
	if len(b) > 32 {
		b = b[len(b)-32:]
	}
	copy(out[32-len(b):], b)
	return out
}

func int256Bytes(n *big.Int) []byte {
	if n == nil {
		return make([]byte, 32)
	}
	if n.Sign() >= 0 {
		return uint256Bytes(n)
	}
	// Two's complement to 256 bits.
	two256 := new(big.Int).Lsh(big.NewInt(1), 256)
	wrapped := new(big.Int).Add(n, two256)
	return uint256Bytes(wrapped)
}

func toBigUint(v any, bits int) (*big.Int, error) {
	n, err := toBigInt(v, bits)
	if err != nil {
		return nil, err
	}
	if n.Sign() < 0 {
		return nil, fmt.Errorf("uint%d: negative value %s", bits, n)
	}
	max := new(big.Int).Lsh(big.NewInt(1), uint(bits))
	if n.Cmp(max) >= 0 {
		return nil, fmt.Errorf("uint%d: value %s exceeds max", bits, n)
	}
	return n, nil
}

func toBigInt(v any, bits int) (*big.Int, error) {
	switch x := v.(type) {
	case *big.Int:
		return new(big.Int).Set(x), nil
	case big.Int:
		return new(big.Int).Set(&x), nil
	case int:
		return big.NewInt(int64(x)), nil
	case int8:
		return big.NewInt(int64(x)), nil
	case int16:
		return big.NewInt(int64(x)), nil
	case int32:
		return big.NewInt(int64(x)), nil
	case int64:
		return big.NewInt(x), nil
	case uint:
		return new(big.Int).SetUint64(uint64(x)), nil
	case uint8:
		return new(big.Int).SetUint64(uint64(x)), nil
	case uint16:
		return new(big.Int).SetUint64(uint64(x)), nil
	case uint32:
		return new(big.Int).SetUint64(uint64(x)), nil
	case uint64:
		return new(big.Int).SetUint64(x), nil
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return nil, errors.New("empty integer string")
		}
		base := 10
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			s = s[2:]
			base = 16
		}
		n, ok := new(big.Int).SetString(s, base)
		if !ok {
			return nil, fmt.Errorf("invalid integer string %q", x)
		}
		return n, nil
	}
	return nil, fmt.Errorf("integer: unsupported type %T", v)
}

func toBytes(v any) ([]byte, error) {
	switch x := v.(type) {
	case []byte:
		return x, nil
	case string:
		s := strings.TrimSpace(x)
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			b, err := hex.DecodeString(s[2:])
			if err != nil {
				return nil, fmt.Errorf("bytes: bad hex %q: %w", x, err)
			}
			return b, nil
		}
		return []byte(s), nil
	}
	return nil, fmt.Errorf("bytes: unsupported type %T", v)
}

func toAnySlice(v any) ([]any, error) {
	switch x := v.(type) {
	case []any:
		return x, nil
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out, nil
	case []*big.Int:
		out := make([]any, len(x))
		for i, n := range x {
			out[i] = n
		}
		return out, nil
	case []uint64:
		out := make([]any, len(x))
		for i, n := range x {
			out[i] = n
		}
		return out, nil
	case []int64:
		out := make([]any, len(x))
		for i, n := range x {
			out[i] = n
		}
		return out, nil
	case []bool:
		out := make([]any, len(x))
		for i, b := range x {
			out[i] = b
		}
		return out, nil
	case [][]byte:
		out := make([]any, len(x))
		for i, b := range x {
			out[i] = b
		}
		return out, nil
	}
	return nil, fmt.Errorf("array: unsupported slice type %T", v)
}

// normaliseEVMAddress accepts the three forms a merchant might paste in and
// returns the 20-byte address ABI encoding uses.
func normaliseEVMAddress(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("address: empty")
	}
	// TRON base58 — starts with 'T' on mainnet, 'T' on Nile testnet too.
	if len(s) >= 30 && (s[0] == 'T' || s[0] == 't') && !strings.HasPrefix(s, "0x") {
		hexAddr, err := TronToHex(s)
		if err != nil {
			return nil, fmt.Errorf("address (TRON base58): %w", err)
		}
		// hexAddr is 0x41 + 20 hex bytes; ABI uses the trailing 20.
		raw, err := hex.DecodeString(strings.TrimPrefix(hexAddr, "0x"))
		if err != nil {
			return nil, fmt.Errorf("address (TRON): %w", err)
		}
		if len(raw) == 21 && raw[0] == 0x41 {
			return raw[1:], nil
		}
		if len(raw) == 20 {
			return raw, nil
		}
		return nil, fmt.Errorf("address: unexpected TRON length %d", len(raw))
	}
	// Hex — accept "0x"-prefix or bare.
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if len(s) == 42 && (s[:2] == "41") {
		// 0x41-prefixed TRON hex.
		s = s[2:]
	}
	if len(s) != 40 {
		return nil, fmt.Errorf("address: want 20 hex bytes, got %d chars", len(s))
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("address: bad hex: %w", err)
	}
	return raw, nil
}
