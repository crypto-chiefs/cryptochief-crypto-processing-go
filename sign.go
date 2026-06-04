package cryptochief

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// canonicalJSON encodes v into the deterministic byte layout the API
// expects for signing:
//
//  1. json.Marshal(v) — Go's default encoder writes structs in field order
//     and maps with alphabetically sorted keys, HTML-escaping < > & and the
//     U+2028 / U+2029 separators.
//  2. json.Unmarshal into an untyped any drops struct field order and gives
//     a map[string]any whose keys json.Marshal will then sort.
//  3. json.Marshal again produces the canonical bytes.
//
// The two-pass round-trip guarantees that struct field order does not
// affect the signature.
//
// Caveat: integers serialised by step 1 are parsed back to float64 in step 2.
// Whole-number floats up to 2^53 round-trip cleanly; for arbitrary-precision
// values (token amounts, wei) always pass strings — which is also the API
// convention.
func canonicalJSON(v any) ([]byte, error) {
	if v == nil {
		return []byte(""), nil
	}
	first, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("cryptochief: marshal: %w", err)
	}
	var tmp any
	if err := json.Unmarshal(first, &tmp); err != nil {
		return nil, fmt.Errorf("cryptochief: unmarshal for canonicalisation: %w", err)
	}
	canonical, err := json.Marshal(tmp)
	if err != nil {
		return nil, fmt.Errorf("cryptochief: re-marshal: %w", err)
	}
	return canonical, nil
}

// signBody returns the hex MD5 signature for a canonical JSON body using
// the merchant API key as the secret. Empty body signs as md5(apiKey).
func signBody(canonicalBody []byte, apiKey string) string {
	b64 := base64.StdEncoding.EncodeToString(canonicalBody)
	sum := md5.Sum([]byte(b64 + apiKey))
	return hex.EncodeToString(sum[:])
}

// Sign computes the value of the Signature header for a body that has
// already been canonicalised. Useful only if you build raw bytes yourself —
// the [Client] does this transparently.
func Sign(canonicalBody []byte, apiKey string) string {
	return signBody(canonicalBody, apiKey)
}

// CanonicalJSON exposes the canonicalisation algorithm for callers who want
// to sign a pre-built request body (rare).
func CanonicalJSON(v any) ([]byte, error) {
	return canonicalJSON(v)
}
