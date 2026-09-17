package cryptochief

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Copied without changes from
// processing-api-gateway/internal/auth/testdata/hmac_v1_vectors.json.
const (
	requestVectorsFile   = "testdata/hmac_v1_vectors.json"
	requestVectorsSHA256 = "a87df4921399dc14c7ceaa7e4c0dfa02495ad0400a5e722adfc0d3e3c1e064fe"
)

// Outcomes a request vector expects of the verifying side.
const (
	requestExpectOK                  = "ok"
	requestExpectBadAuthHeaders      = "bad_auth_headers"
	requestExpectTimestampOutOfRange = "timestamp_out_of_range"
	requestExpectInvalidSignature    = "invalid_signature"
	requestVectorContentTypeJSON     = "application/json"
)

// hmacV1Vector is one entry of the request vectors. The receiver sees
// Merchant, X-CC-Timestamp, X-CC-Nonce, X-CC-Signature = "v1=" + Signature,
// Idempotency-Key when IdempotencyKey is set and Content-Type:
// application/json when the body is not empty, except for the headers listed
// in Headers: an empty list removes the header, two values repeat it.
type hmacV1Vector struct {
	Name           string              `json:"name"`
	APIKey         string              `json:"api_key"`
	Method         string              `json:"method"`
	Path           string              `json:"path"`
	Query          string              `json:"query"`
	Merchant       string              `json:"merchant"`
	IdempotencyKey string              `json:"idempotency_key"`
	Timestamp      string              `json:"timestamp"`
	Nonce          string              `json:"nonce"`
	Body           string              `json:"body"`
	BodySHA256     string              `json:"body_sha256"`
	StringToSign   string              `json:"string_to_sign"`
	Signature      string              `json:"signature"`
	Now            int64               `json:"now"`
	Expect         string              `json:"expect"`
	Headers        map[string][]string `json:"headers"`
}

func loadHMACv1Vectors(t *testing.T) []hmacV1Vector {
	t.Helper()
	raw, err := os.ReadFile(requestVectorsFile)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != requestVectorsSHA256 {
		t.Fatalf("%s sha256 = %s, want %s", requestVectorsFile, got, requestVectorsSHA256)
	}
	var vs []hmacV1Vector
	if err := json.Unmarshal(raw, &vs); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, v := range vs {
		counts[v.Expect]++
	}
	want := map[string]int{
		requestExpectOK:                  21,
		requestExpectBadAuthHeaders:      24,
		requestExpectInvalidSignature:    3,
		requestExpectTimestampOutOfRange: 2,
	}
	if len(vs) != 50 || len(counts) != len(want) {
		t.Fatalf("vectors: %d entries %v, want 50 %v", len(vs), counts, want)
	}
	for k, n := range want {
		if counts[k] != n {
			t.Fatalf("vectors: %v, want %v", counts, want)
		}
	}
	return vs
}

func (v hmacV1Vector) input() HMACv1Input {
	return HMACv1Input{
		Timestamp:      v.Timestamp,
		Nonce:          v.Nonce,
		Method:         v.Method,
		Path:           v.Path,
		Query:          v.Query,
		Merchant:       v.Merchant,
		IdempotencyKey: v.IdempotencyKey,
		Body:           []byte(v.Body),
	}
}

// header builds the headers the receiver sees, applying the overrides.
func (v hmacV1Vector) header(t *testing.T) http.Header {
	t.Helper()
	values := map[string][]string{
		headerMerchant:  {v.Merchant},
		HeaderTimestamp: {v.Timestamp},
		headerNonce:     {v.Nonce},
		HeaderSignature: {"v1=" + v.Signature},
	}
	if v.IdempotencyKey != "" {
		values[headerIdempotencyKey] = []string{v.IdempotencyKey}
	}
	if v.Body != "" {
		values["Content-Type"] = []string{requestVectorContentTypeJSON}
	}
	for name, vs := range v.Headers {
		if vs == nil {
			t.Fatalf("%s: header override %q is null", v.Name, name)
		}
		// Names are compared ignoring case; the override replaces whatever
		// spelling the defaults hold.
		for known := range values {
			if strings.EqualFold(known, name) {
				delete(values, known)
			}
		}
		values[name] = vs
	}
	h := http.Header{}
	for name, vs := range values {
		for _, val := range vs {
			h.Add(name, val)
		}
	}
	return h
}

// request synthesises the HTTP request the receiver sees. It is built rather
// than sent: a CR in a header value or a non-ASCII byte in one never survives
// net/http's own writer, and both are vectors.
func (v hmacV1Vector) request(t *testing.T) *http.Request {
	t.Helper()
	target := v.Path
	if v.Query != "" {
		target += "?" + v.Query
	}
	req, err := http.NewRequest(v.Method, "http://gateway.invalid"+target, bytes.NewReader([]byte(v.Body)))
	if err != nil {
		t.Fatalf("%s: %v", v.Name, err)
	}
	req.Header = v.header(t)
	return req
}

// requestOutcome maps the mock gateway's answer onto the vector's expect.
func requestOutcome(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code == http.StatusOK {
		return requestExpectOK
	}
	var env struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("status %d, body %q: %v", rec.Code, rec.Body.Bytes(), err)
	}
	switch {
	case rec.Code == http.StatusBadRequest && env.Error == CodeBadAuthHeaders:
		return requestExpectBadAuthHeaders
	case rec.Code == http.StatusUnauthorized && env.Error == CodeSignatureTimestampOutOfRange:
		return requestExpectTimestampOutOfRange
	case rec.Code == http.StatusUnauthorized && env.Error == CodeInvalidSignature:
		return requestExpectInvalidSignature
	}
	return fmt.Sprintf("%d %s", rec.Code, env.Error)
}

// TestHMACv1Vectors_Verify runs every vector — refusals included — through the
// gateway's own rules: the refusing ones get exactly the refusal they name,
// and no other.
func TestHMACv1Vectors_Verify(t *testing.T) {
	for _, v := range loadHMACv1Vectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			// A gateway per vector: several accepted vectors share a nonce,
			// which one store would count as a replay.
			g := newMockGatewayHandler(t, map[string]string{v.Merchant: v.APIKey})
			g.now = fixedClock(v.Now)
			rec := httptest.NewRecorder()
			g.ServeHTTP(rec, v.request(t))
			if got := requestOutcome(t, rec); got != v.Expect {
				t.Fatalf("verify = %s, want %s (body %s)", got, v.Expect, rec.Body.Bytes())
			}
		})
	}
}

// Each refused vector passes once its single defect — the header override or
// the clock — is removed.
func TestHMACv1Vectors_SingleDefect(t *testing.T) {
	for _, v := range loadHMACv1Vectors(t) {
		if v.Expect == requestExpectOK {
			continue
		}
		fixed := v
		fixed.Headers = nil
		fixed.Now = mustParseUnix(t, v.Timestamp)
		g := newMockGatewayHandler(t, map[string]string{fixed.Merchant: fixed.APIKey})
		g.now = fixedClock(fixed.Now)
		rec := httptest.NewRecorder()
		g.ServeHTTP(rec, fixed.request(t))
		if got := requestOutcome(t, rec); got != requestExpectOK {
			t.Errorf("%s: without the defect: %s (body %s)", v.Name, got, rec.Body.Bytes())
		}
	}
}

func mustParseUnix(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestHMACv1Vectors(t *testing.T) {
	for _, v := range loadHMACv1Vectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			sum := sha256.Sum256([]byte(v.Body))
			if got := hex.EncodeToString(sum[:]); got != v.BodySHA256 {
				t.Fatalf("body_sha256 = %s, want %s", got, v.BodySHA256)
			}

			sts, err := StringToSignHMACv1(v.input())
			if err != nil {
				t.Fatal(err)
			}
			if sts != v.StringToSign {
				t.Fatalf("string_to_sign\n got: %q\nwant: %q", sts, v.StringToSign)
			}

			sig, err := SignHMACv1(v.APIKey, v.input())
			if err != nil {
				t.Fatal(err)
			}
			if sig != v.Signature {
				t.Fatalf("signature = %s, want %s", sig, v.Signature)
			}
		})
	}
}

// A signed GET: the client sends POST, so a route on another Crypto Chief
// service — the energy API's GET /v1/orders/{key} and GET /v1/balance — is
// signed from the request itself, as README shows.
func TestSignHMACv1_GETFromRequest(t *testing.T) {
	var v hmacV1Vector
	for _, vec := range loadHMACv1Vectors(t) {
		if vec.Method == http.MethodGet {
			v = vec
			break
		}
	}
	if v.Name == "" {
		t.Fatal("no GET vector")
	}
	if v.Body != "" {
		t.Fatalf("%s: GET vector with a body", v.Name)
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.example"+v.Path+"?"+v.Query, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(headerMerchant, v.Merchant)
	sig, err := SignHMACv1(v.APIKey, HMACv1Input{
		Timestamp: v.Timestamp,
		Nonce:     v.Nonce,
		Method:    req.Method,
		Path:      req.URL.Path,
		Query:     req.URL.RawQuery,
		Merchant:  req.Header.Get(headerMerchant),
	})
	if err != nil {
		t.Fatal(err)
	}
	if sig != v.Signature {
		t.Fatalf("signature = %s, want %s", sig, v.Signature)
	}

	// The query is signed: drop it and the signature is another one.
	req.URL.RawQuery = ""
	noQuery, err := SignHMACv1(v.APIKey, HMACv1Input{
		Timestamp: v.Timestamp,
		Nonce:     v.Nonce,
		Method:    req.Method,
		Path:      req.URL.Path,
		Query:     req.URL.RawQuery,
		Merchant:  v.Merchant,
	})
	if err != nil {
		t.Fatal(err)
	}
	if noQuery == sig {
		t.Fatal("the query is not covered by the signature")
	}
}

// TestSignHMACv1_EnergyVectors pins the signatures the energy API publishes,
// the service the SDK reaches with [Client.Request]. Same key, merchant and
// timestamp as the shared vectors; one of the three is a GET.
func TestSignHMACv1_EnergyVectors(t *testing.T) {
	const body = `{"receive_address":"TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"}`
	for _, v := range []struct {
		method, path, nonce, idempotencyKey, body, want string
	}{
		{
			method: http.MethodPost, path: "/v1/quotes",
			nonce: "0123456789abcdef0123456789abcdef", body: body,
			want: "3d7e22707733341bd61ecfa0b8a04ad67c3e91145d9c5c89b09f1ae706310266",
		},
		{
			method: http.MethodGet, path: "/v1/balance",
			nonce: "3c4d5e6f708192a3b4c5d6e7f8091a2b",
			want:  "7fafffeb432c2a4f56504a972a020608d01774e616a9363e91e654905fb26164",
		},
		{
			method: http.MethodPost, path: "/v1/orders",
			nonce: "fedcba9876543210fedcba9876543210", idempotencyKey: "payout-8814", body: body,
			want: "b46e83809fd4bd95f386d9d8d789147363fd78265a677e7332e0dce409b50def",
		},
	} {
		var raw []byte
		if v.body != "" {
			raw = []byte(v.body)
		}
		sig, err := SignHMACv1(hmacTestAPIKey, HMACv1Input{
			Timestamp:      strconv.FormatInt(hmacTestUnix, 10),
			Nonce:          v.nonce,
			Method:         v.method,
			Path:           v.path,
			Merchant:       hmacTestMerchant,
			IdempotencyKey: v.idempotencyKey,
			Body:           raw,
		})
		if err != nil {
			t.Fatalf("%s %s: %v", v.method, v.path, err)
		}
		if sig != v.want {
			t.Errorf("%s %s: signature = %s, want %s", v.method, v.path, sig, v.want)
		}
	}
}

// TestHMACv1_MethodUpperASCIIOnly: a–z are upper-cased, every other byte goes
// into the string to sign as it is. Unicode case mapping would fold ſ to S and
// ı to I and sign a method the server never reads.
func TestHMACv1_MethodUpperASCIIOnly(t *testing.T) {
	v := loadHMACv1Vectors(t)[0]

	for _, method := range []string{"post", "PoSt", "POST"} {
		in := v.input()
		in.Method = method
		sig, err := SignHMACv1(v.APIKey, in)
		if err != nil {
			t.Fatal(err)
		}
		if sig != v.Signature {
			t.Errorf("%q: signature = %s, want %s", method, sig, v.Signature)
		}
	}

	for _, method := range []string{"poſt", "ıNFO", "MÉTHODE", "Ünlock", "PATCHµ"} {
		in := v.input()
		in.Method = method
		sts, err := StringToSignHMACv1(in)
		if err != nil {
			t.Fatal(err)
		}
		line := strings.Split(sts, "\n")[3]
		if want := upperASCIIWant(method); line != want {
			t.Errorf("%q: method line = %q, want %q", method, line, want)
		}
	}

	// Where Unicode case mapping disagrees with a–z, the string to sign
	// follows a–z.
	for _, method := range []string{"poſt", "ıNFO"} {
		in := v.input()
		in.Method = method
		sts, err := StringToSignHMACv1(in)
		if err != nil {
			t.Fatal(err)
		}
		line := strings.Split(sts, "\n")[3]
		if unicodeUpper := strings.ToUpper(method); line == unicodeUpper {
			t.Errorf("%q: signed the Unicode upper case %q", method, unicodeUpper)
		}
	}
}

// upperASCIIWant maps a–z and copies every other byte, computed apart from the
// implementation under test.
func upperASCIIWant(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - ('a' - 'A')
		}
	}
	return string(b)
}

// TestHMACv1_EmptyAPIKey: a key of nothing, or of spaces and tabs, signs
// nothing — requests and webhooks alike — and the client refuses to be built
// with one.
func TestHMACv1_EmptyAPIKey(t *testing.T) {
	v := loadHMACv1Vectors(t)[0]
	for _, key := range []string{"", " ", "\t", " \t ", "\t\t  \t"} {
		if _, err := SignHMACv1(key, v.input()); !errors.Is(err, ErrEmptyAPIKey) {
			t.Errorf("SignHMACv1(%q) = %v, want ErrEmptyAPIKey", key, err)
		}
		if _, err := SignWebhookV1(key, hmacTestUnix, "7c9e6679-7425-40de-944b-e07fc1f90ae7", nil); !errors.Is(err, ErrEmptyAPIKey) {
			t.Errorf("SignWebhookV1(%q) = %v, want ErrEmptyAPIKey", key, err)
		}
		if _, err := New(hmacTestMerchant, key); !errors.Is(err, ErrEmptyAPIKey) {
			t.Errorf("New(%q) = %v, want ErrEmptyAPIKey", key, err)
		}
	}

	// A key that is only spaces and tabs is not the key those spaces would
	// make: nothing is signed with it.
	if _, err := SignHMACv1(" \t", v.input()); err == nil {
		t.Fatal("a blank key signed a request")
	}
}

// TestHMACv1_EmptyAPIKeyRefused: the verifying side refuses a project whose
// key is empty or only spaces and tabs, whatever signature arrives.
func TestHMACv1_EmptyAPIKeyRefused(t *testing.T) {
	v := loadHMACv1Vectors(t)[0]
	for _, key := range []string{"", " ", "\t", " \t "} {
		g := newMockGatewayHandler(t, map[string]string{v.Merchant: key})
		g.now = fixedClock(v.Now)
		rec := httptest.NewRecorder()
		g.ServeHTTP(rec, v.request(t))
		if got := requestOutcome(t, rec); got != requestExpectInvalidSignature {
			t.Errorf("key %q: verify = %s, want %s", key, got, requestExpectInvalidSignature)
		}

		// The signature computed with that key does not help either.
		sig := hmacHexWithKey(key, v.StringToSign)
		w := v
		w.Headers = map[string][]string{HeaderSignature: {"v1=" + sig}}
		g2 := newMockGatewayHandler(t, map[string]string{v.Merchant: key})
		g2.now = fixedClock(v.Now)
		rec2 := httptest.NewRecorder()
		g2.ServeHTTP(rec2, w.request(t))
		if got := requestOutcome(t, rec2); got != requestExpectInvalidSignature {
			t.Errorf("key %q, signed with it: verify = %s, want %s", key, got, requestExpectInvalidSignature)
		}
	}
}

func hmacHexWithKey(key, stringToSign string) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(stringToSign))
	return hex.EncodeToString(m.Sum(nil))
}

// TestVerifyWebhook_EmptyAPIKey: the webhook receiver refuses the same way.
func TestVerifyWebhook_EmptyAPIKey(t *testing.T) {
	v := findWebhookV1Vector(t, "payout_paid")
	for _, key := range []string{"", " ", "\t", " \t "} {
		err := VerifyWebhook(key, v.body(t), v.header(t), v.clock())
		if !errors.Is(err, ErrEmptyAPIKey) {
			t.Errorf("key %q: %v, want ErrEmptyAPIKey", key, err)
		}
		for _, sentinel := range []error{ErrWebhookHeaders, ErrWebhookTimestamp, ErrWebhookSignature} {
			if errors.Is(err, sentinel) {
				t.Errorf("key %q: %v also matches %v", key, err, sentinel)
			}
		}
	}
}

func TestHMACv1_LineBreakRejected(t *testing.T) {
	base := loadHMACv1Vectors(t)[0].input()
	mutators := map[string]func(*HMACv1Input){
		"timestamp":       func(in *HMACv1Input) { in.Timestamp += "\n" },
		"nonce":           func(in *HMACv1Input) { in.Nonce += "\r" },
		"method":          func(in *HMACv1Input) { in.Method = "PO\nST" },
		"path":            func(in *HMACv1Input) { in.Path += "\r\n" },
		"query":           func(in *HMACv1Input) { in.Query = "a=1\nb=2" },
		"merchant":        func(in *HMACv1Input) { in.Merchant += "\n" },
		"idempotency_key": func(in *HMACv1Input) { in.IdempotencyKey = "k\r" },
	}
	for name, mutate := range mutators {
		t.Run(name, func(t *testing.T) {
			in := base
			mutate(&in)
			if _, err := StringToSignHMACv1(in); !errors.Is(err, ErrHMACv1LineBreak) {
				t.Fatalf("StringToSignHMACv1 err = %v, want ErrHMACv1LineBreak", err)
			}
			if _, err := SignHMACv1("k", in); !errors.Is(err, ErrHMACv1LineBreak) {
				t.Fatalf("SignHMACv1 err = %v, want ErrHMACv1LineBreak", err)
			}
		})
	}

	// Line breaks inside the body are signed through its hash.
	in := base
	in.Body = []byte("{\n}\r\n")
	if _, err := StringToSignHMACv1(in); err != nil {
		t.Fatalf("body with line breaks: %v", err)
	}
}
