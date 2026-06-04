package cryptochief

import (
	"testing"
)

// Signature regression suite — fixed payloads with known correct hashes.
// A drift in canonical JSON or MD5 wiring fails here before it can fail
// against the live API. Secret: "test_api_key_123".
func TestSignatureVectors(t *testing.T) {
	const secret = "test_api_key_123"
	cases := []struct {
		name    string
		body    any
		want    string
		wantCan string // canonical bytes (sanity-check the first hop too)
	}{
		{
			name: "payout estimate body",
			body: map[string]any{
				"network":        "ETH_SEPOLIA",
				"coin":           "ETH",
				"amount":         "0.0001",
				"to_address":     "0xAbC",
				"from_addresses": []any{"0x111", "0x222"},
			},
			want:    "97bd68e4e4dc86b6dad8aa06e1f7b63d",
			wantCan: `{"amount":"0.0001","coin":"ETH","from_addresses":["0x111","0x222"],"network":"ETH_SEPOLIA","to_address":"0xAbC"}`,
		},
		{
			name: "batch payout body, url with HTML-escaped chars",
			body: map[string]any{
				"items": []any{
					map[string]any{"order_id": "b", "user_id": "u", "amount": "1"},
					map[string]any{"order_id": "a", "user_id": "u2", "amount": "2"},
				},
				"url_callback": "https://x.io/cb?a=1&b=2",
			},
			want: "8b85b5464c9a92059a74039d7a008618",
		},
		{
			// HTML-escaping of < > & is applied to string values, so the
			// canonical bytes contain <, >, & instead. The
			// hash below is computed against those escaped bytes — matching
			// it is sufficient proof that the encoder emits them correctly.
			name: "nested map + array, HTML chars in values",
			body: map[string]any{
				"z":   true,
				"a":   1,
				"m":   map[string]any{"y": "<tag>", "x": "a&b"},
				"arr": []any{3, 2, 1},
			},
			want: "5fcfb2c41ee9d91073b9adcf22fe8a79",
		},
		{
			name:    "empty body",
			body:    map[string]any{},
			want:    "33d8723e69fba9d68b8991ad200be4b3",
			wantCan: `{}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			canon, err := canonicalJSON(tc.body)
			if err != nil {
				t.Fatalf("canonicalJSON: %v", err)
			}
			if tc.wantCan != "" && string(canon) != tc.wantCan {
				t.Fatalf("canonical mismatch\n got: %s\nwant: %s", canon, tc.wantCan)
			}
			got := signBody(canon, secret)
			if got != tc.want {
				t.Fatalf("signature mismatch: got %s want %s\n canonical: %s", got, tc.want, canon)
			}
		})
	}
}

// Round-tripping a Go struct through canonicalJSON must produce the same
// output as the equivalent map literal — that's the guarantee callers rely on
// when they pass typed request structs to the client.
func TestCanonicalJSON_StructMatchesMap(t *testing.T) {
	type body struct {
		Network       string   `json:"network"`
		Coin          string   `json:"coin"`
		Amount        string   `json:"amount"`
		ToAddress     string   `json:"to_address"`
		FromAddresses []string `json:"from_addresses"`
	}
	asStruct := body{
		Network:       "ETH_SEPOLIA",
		Coin:          "ETH",
		Amount:        "0.0001",
		ToAddress:     "0xAbC",
		FromAddresses: []string{"0x111", "0x222"},
	}
	asMap := map[string]any{
		"network":        "ETH_SEPOLIA",
		"coin":           "ETH",
		"amount":         "0.0001",
		"to_address":     "0xAbC",
		"from_addresses": []any{"0x111", "0x222"},
	}
	a, err := canonicalJSON(asStruct)
	if err != nil {
		t.Fatal(err)
	}
	b, err := canonicalJSON(asMap)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("struct vs map canonical drift\n struct: %s\n    map: %s", a, b)
	}
}
