package cryptochief

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const hmacV1Scope = "CC-HMAC-SHA256-REQ-V1"

// ErrHMACv1LineBreak is returned when a field of the string to sign contains
// CR or LF.
var ErrHMACv1LineBreak = errors.New("cryptochief: hmac v1 field contains CR or LF")

// ErrEmptyAPIKey is returned when the signing key is empty or made only of
// spaces and tabs. Such a key signs nothing the server would accept — a
// project configured with one is refused there.
var ErrEmptyAPIKey = errors.New("cryptochief: API key is empty")

// blankAPIKey reports whether apiKey counts as empty: nothing, or only spaces
// and tabs.
func blankAPIKey(apiKey string) bool {
	return strings.Trim(apiKey, " \t") == ""
}

// upperASCII upper-cases a–z and leaves every other byte alone. An HTTP method
// is an RFC 9110 token; Unicode case mapping would rewrite bytes outside that
// range (ſ → S, ı → I) and sign a method the server never sees.
func upperASCII(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 'a' || c > 'z' {
			continue
		}
		if b == nil {
			b = []byte(s)
		}
		b[i] = c - ('a' - 'A')
	}
	if b == nil {
		return s
	}
	return string(b)
}

// HMACv1Input holds the fields of the HMAC-SHA256 v1 string to sign.
type HMACv1Input struct {
	Timestamp      string // X-CC-Timestamp, Unix seconds
	Nonce          string // X-CC-Nonce
	Method         string // signed with a–z upper-cased, other bytes as they are
	Path           string // API route from /v1/, percent-decoded, without query and base URL prefix
	Query          string // query without "?" as the URL carries it, or empty; signed even on GET
	Merchant       string // Merchant header
	IdempotencyKey string // Idempotency-Key header, or empty
	Body           []byte // body bytes as sent
}

// StringToSignHMACv1 builds the HMAC-SHA256 v1 string to sign. Lines are
// joined by "\n", with no trailing newline:
//
//	CC-HMAC-SHA256-REQ-V1
//	<Timestamp>
//	<Nonce>
//	<METHOD>
//	<Path>
//	<Query>
//	<Merchant>
//	<IdempotencyKey>
//	<lowercase hex SHA-256 of Body>
//
// Path is the percent-decoded route: a request for
// /v1/orders/payout%2F8814 signs /v1/orders/payout/8814, the form the server
// reads. Query keeps the spelling the URL carries. Method is upper-cased over
// a–z only.
func StringToSignHMACv1(in HMACv1Input) (string, error) {
	fields := [...]string{
		in.Timestamp,
		in.Nonce,
		upperASCII(in.Method),
		in.Path,
		in.Query,
		in.Merchant,
		in.IdempotencyKey,
	}
	for _, f := range fields {
		if strings.ContainsAny(f, "\r\n") {
			return "", ErrHMACv1LineBreak
		}
	}
	bodySum := sha256.Sum256(in.Body)

	var b strings.Builder
	b.WriteString(hmacV1Scope)
	for _, f := range fields {
		b.WriteByte('\n')
		b.WriteString(f)
	}
	b.WriteByte('\n')
	b.WriteString(hex.EncodeToString(bodySum[:]))
	return b.String(), nil
}

// SignHMACv1 returns the X-CC-Signature header value of a request:
// [SignatureV1Prefix] and lowercase hex HMAC-SHA256(key = apiKey, message =
// [StringToSignHMACv1](in)) — the same shape [SignWebhookV1] returns for a
// webhook. The [Client] does this on every request.
//
// Use it to sign a request the SDK does not send itself. To send one through
// the client instead — signed, retried and with the error envelope parsed —
// use [Client.Request], which takes the method.
//
// An apiKey that is empty or only spaces and tabs returns [ErrEmptyAPIKey].
func SignHMACv1(apiKey string, in HMACv1Input) (string, error) {
	if blankAPIKey(apiKey) {
		return "", ErrEmptyAPIKey
	}
	sts, err := StringToSignHMACv1(in)
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, []byte(apiKey))
	m.Write([]byte(sts))
	return SignatureV1Prefix + hex.EncodeToString(m.Sum(nil)), nil
}

// newHMACv1Nonce returns 32 lowercase hex characters from 16 random bytes.
func newHMACv1Nonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("cryptochief: generate nonce: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
