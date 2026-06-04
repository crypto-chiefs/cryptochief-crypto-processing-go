package cryptochief

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// ErrBadAmount is returned by HumanToBase when its input doesn't look like a
// decimal number.
var ErrBadAmount = errors.New("cryptochief: invalid amount")

// HumanToBase converts a decimal human-readable amount string ("0.0001") to
// its base-unit integer representation (wei / satoshi / lamports / ...) for
// the given number of decimals.
//
// Uses [math/big] internally — never lossy, regardless of how many decimals
// you have. Negative amounts and exponential notation are rejected.
//
//	wei, _ := HumanToBase("1.5", 18)   // 1500000000000000000
//	sat, _ := HumanToBase("0.0001", 8) // 10000
func HumanToBase(human string, decimals int) (*big.Int, error) {
	s := strings.TrimSpace(human)
	if s == "" {
		return nil, fmt.Errorf("%w: empty", ErrBadAmount)
	}
	if decimals < 0 {
		return nil, fmt.Errorf("%w: negative decimals %d", ErrBadAmount, decimals)
	}
	if strings.ContainsAny(s, "eE") {
		return nil, fmt.Errorf("%w: scientific notation not allowed: %q", ErrBadAmount, human)
	}
	if strings.HasPrefix(s, "-") {
		return nil, fmt.Errorf("%w: negative not allowed: %q", ErrBadAmount, human)
	}
	intPart, fracPart, ok := splitDecimal(s)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrBadAmount, human)
	}

	// Pad or truncate fractional part to exactly `decimals` digits. Truncate
	// is what every blockchain client library does — sub-base-unit precision
	// is meaningless on-chain.
	if len(fracPart) > decimals {
		fracPart = fracPart[:decimals]
	} else if len(fracPart) < decimals {
		fracPart += strings.Repeat("0", decimals-len(fracPart))
	}

	combined := strings.TrimLeft(intPart+fracPart, "0")
	if combined == "" {
		combined = "0"
	}
	out, ok := new(big.Int).SetString(combined, 10)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrBadAmount, human)
	}
	return out, nil
}

// BaseToHuman is the inverse of HumanToBase: turns a base-unit integer into a
// decimal string with the given decimals, trimming trailing zeroes.
//
//	BaseToHuman(big.NewInt(1500000000000000000), 18) // "1.5"
//	BaseToHuman(big.NewInt(0), 18)                   // "0"
func BaseToHuman(base *big.Int, decimals int) string {
	if base == nil {
		return "0"
	}
	if decimals < 0 {
		decimals = 0
	}
	// Always work with the absolute value; we don't accept negative input.
	abs := new(big.Int).Abs(base).String()
	if decimals == 0 {
		return abs
	}
	if len(abs) <= decimals {
		abs = strings.Repeat("0", decimals-len(abs)+1) + abs
	}
	cut := len(abs) - decimals
	intPart := abs[:cut]
	fracPart := strings.TrimRight(abs[cut:], "0")
	if fracPart == "" {
		return intPart
	}
	return intPart + "." + fracPart
}

func splitDecimal(s string) (intPart, fracPart string, ok bool) {
	if s == "" {
		return "", "", false
	}
	dot := strings.IndexByte(s, '.')
	if dot < 0 {
		if !allDigits(s) {
			return "", "", false
		}
		return s, "", true
	}
	intPart = s[:dot]
	fracPart = s[dot+1:]
	if intPart == "" {
		intPart = "0"
	}
	if fracPart == "" {
		return "", "", false
	}
	if !allDigits(intPart) || !allDigits(fracPart) {
		return "", "", false
	}
	return intPart, fracPart, true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
