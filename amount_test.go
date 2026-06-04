package cryptochief

import (
	"math/big"
	"testing"
)

func TestHumanToBase(t *testing.T) {
	cases := []struct {
		human    string
		decimals int
		want     string
	}{
		{"0", 18, "0"},
		{"1", 18, "1000000000000000000"},
		{"1.5", 18, "1500000000000000000"},
		{"0.0001", 18, "100000000000000"},
		{"0.0001", 8, "10000"},
		// Sub-base-unit precision is silently truncated, matching how every
		// other web3 client behaves.
		{"0.000000000000000001", 18, "1"},
		{"0.0000000000000000019", 18, "1"},
		// Decimals 0 — base-unit-only, no fractional digits permitted by the
		// math but ".0" is a no-op.
		{"42.0", 0, "42"},
		{"42", 0, "42"},
		// Lots of zeros up front shouldn't trip up the integer parser.
		{"00000.5", 18, "500000000000000000"},
		{".5", 18, "500000000000000000"},
	}
	for _, tc := range cases {
		got, err := HumanToBase(tc.human, tc.decimals)
		if err != nil {
			t.Errorf("HumanToBase(%q, %d): unexpected err: %v", tc.human, tc.decimals, err)
			continue
		}
		want, _ := new(big.Int).SetString(tc.want, 10)
		if got.Cmp(want) != 0 {
			t.Errorf("HumanToBase(%q, %d): got %s want %s", tc.human, tc.decimals, got, want)
		}
	}
}

func TestHumanToBase_Errors(t *testing.T) {
	bad := []struct {
		human    string
		decimals int
	}{
		{"", 18},
		{"abc", 18},
		{"1.2.3", 18},
		{"-1", 18},
		{"1e10", 18},
		{"1E10", 18},
		{"1.", 18},
		{"1.5", -1},
	}
	for _, tc := range bad {
		_, err := HumanToBase(tc.human, tc.decimals)
		if err == nil {
			t.Errorf("HumanToBase(%q, %d): expected error, got nil", tc.human, tc.decimals)
		}
	}
}

func TestBaseToHuman(t *testing.T) {
	cases := []struct {
		base     string
		decimals int
		want     string
	}{
		{"0", 18, "0"},
		{"1000000000000000000", 18, "1"},
		{"1500000000000000000", 18, "1.5"},
		{"1", 18, "0.000000000000000001"},
		{"100000000000000", 18, "0.0001"},
		{"10000", 8, "0.0001"},
		{"42", 0, "42"},
	}
	for _, tc := range cases {
		b, _ := new(big.Int).SetString(tc.base, 10)
		got := BaseToHuman(b, tc.decimals)
		if got != tc.want {
			t.Errorf("BaseToHuman(%s, %d): got %q want %q", tc.base, tc.decimals, got, tc.want)
		}
	}
}

// Round-trip property: HumanToBase ∘ BaseToHuman is the identity on valid
// human strings.
func TestHumanBase_RoundTrip(t *testing.T) {
	inputs := []struct {
		human    string
		decimals int
	}{
		{"0", 18},
		{"1.234", 18},
		{"0.0001", 8},
		{"1234567.89", 6},
	}
	for _, tc := range inputs {
		base, err := HumanToBase(tc.human, tc.decimals)
		if err != nil {
			t.Fatalf("HumanToBase: %v", err)
		}
		round := BaseToHuman(base, tc.decimals)
		// Compare via re-parse so "1" vs "1.0" don't trip us.
		want, _ := new(big.Int).SetString(base.String(), 10)
		got, err := HumanToBase(round, tc.decimals)
		if err != nil {
			t.Fatalf("HumanToBase(roundtrip): %v", err)
		}
		if got.Cmp(want) != 0 {
			t.Errorf("round-trip drift: human=%q decimals=%d → base=%s → human=%q → base=%s",
				tc.human, tc.decimals, base, round, got)
		}
	}
}
