package cryptochief

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	mockNonceFormat     = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)
	mockSignatureFormat = regexp.MustCompile(`^v1=[0-9A-Fa-f]{64}$`)
)

// mockGateway checks requests the way the gateway does: the four HMAC v1
// headers, the timestamp window, the signature over the received body bytes,
// nonce replay. The Signature header is not read.
type mockGateway struct {
	t      *testing.T
	keys   map[string]string // merchant → api key
	now    func() time.Time
	prefix string // base URL path before the API route

	// body returns the response body for a verified request; nil answers
	// "null".
	body func(*http.Request) string

	mu       sync.Mutex
	nonces   map[string]bool
	accepted []sentRequest
}

// sent returns the requests the gateway accepted.
func (g *mockGateway) sent() []sentRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]sentRequest(nil), g.accepted...)
}

func (g *mockGateway) refuse(w http.ResponseWriter, status int, code string, serverTime int64) {
	body := map[string]any{"ok": false, "error": code, "msg": code}
	if serverTime > 0 {
		body["server_time"] = serverTime
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// mockIsDecimal is the gateway's rule for X-CC-Timestamp on a request: up to
// 18 digits, a leading zero allowed — the header value goes into the string to
// sign as it is.
func mockIsDecimal(s string) bool {
	if s == "" || len(s) > 18 {
		return false
	}
	if len(s) > 1 && s[0] == '0' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func mockSingleHeader(h http.Header, name string) (string, bool) {
	vs := h.Values(name)
	if len(vs) != 1 {
		return "", false
	}
	v := strings.Trim(vs[0], " \t")
	return v, v != "" && !strings.ContainsAny(v, "\r\n")
}

// mockOptionalHeader is the gateway's rule for a header that may be absent —
// Idempotency-Key. Absent is an empty value; repeated is a refusal.
func mockOptionalHeader(h http.Header, name string) (string, bool) {
	vs := h.Values(name)
	if len(vs) > 1 {
		return "", false
	}
	if len(vs) == 0 {
		return "", true
	}
	v := strings.Trim(vs[0], " \t")
	return v, !strings.ContainsAny(v, "\r\n")
}

// mockIsJSONContentType is the gateway's rule for Content-Type on a request
// with a body: media type application/json ignoring ASCII case, parameters
// allowed, the value parsable as a media type.
func mockIsJSONContentType(v string) bool {
	base, _, _ := strings.Cut(v, ";")
	if !strings.EqualFold(strings.Trim(base, " \t"), "application/json") {
		return false
	}
	_, _, err := mime.ParseMediaType(v)
	return err == nil
}

func (g *mockGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		g.refuse(w, http.StatusBadRequest, "READ_BODY", 0)
		return
	}
	merchant, okM := mockSingleHeader(r.Header, headerMerchant)
	tsValue, okT := mockSingleHeader(r.Header, HeaderTimestamp)
	nonce, okN := mockSingleHeader(r.Header, headerNonce)
	sig, okS := mockSingleHeader(r.Header, HeaderSignature)
	idem, okI := mockOptionalHeader(r.Header, headerIdempotencyKey)
	ts, tsErr := strconv.ParseInt(tsValue, 10, 64)
	if !okM || !okT || !okN || !okS || !okI || tsErr != nil || !mockIsDecimal(tsValue) ||
		!mockNonceFormat.MatchString(nonce) || !mockSignatureFormat.MatchString(sig) {
		g.refuse(w, http.StatusBadRequest, CodeBadAuthHeaders, 0)
		return
	}
	// A body requires Content-Type: application/json, and that is a header
	// refusal too — it is checked before the timestamp window.
	if len(body) > 0 && !mockIsJSONContentType(r.Header.Get("Content-Type")) {
		g.refuse(w, http.StatusBadRequest, CodeBadAuthHeaders, 0)
		return
	}
	now := g.now().Unix()
	if ts < now-300 || ts > now+300 {
		g.refuse(w, http.StatusUnauthorized, CodeSignatureTimestampOutOfRange, now)
		return
	}
	key := g.keys[merchant]
	route := strings.TrimPrefix(r.URL.Path, g.prefix)
	sum := sha256.Sum256(body)
	sts := strings.Join([]string{
		"CC-HMAC-SHA256-REQ-V1", tsValue, nonce, upperASCII(r.Method), route, r.URL.RawQuery, merchant, idem,
		hex.EncodeToString(sum[:]),
	}, "\n")
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(sts))
	got, _ := hex.DecodeString(sig[len("v1="):])
	// A project whose key is empty or only spaces and tabs signs nothing the
	// gateway accepts.
	if blankAPIKey(key) || !hmac.Equal(m.Sum(nil), got) {
		g.refuse(w, http.StatusUnauthorized, CodeInvalidSignature, 0)
		return
	}

	g.mu.Lock()
	replayed := g.nonces[merchant+"\x00"+nonce]
	g.nonces[merchant+"\x00"+nonce] = true
	if !replayed {
		g.accepted = append(g.accepted, sentRequest{
			Method: r.Method, URLPath: r.URL.Path, RawQuery: r.URL.RawQuery,
			RequestURI: r.RequestURI, ContentType: r.Header.Get("Content-Type"),
			Merchant: merchant, Signature: r.Header.Values("Signature"),
			Timestamp: tsValue, Nonce: nonce, HMAC: sig, Idempotency: idem,
			Body: body,
		})
	}
	g.mu.Unlock()
	if replayed {
		g.refuse(w, http.StatusUnauthorized, CodeSignatureReplayed, 0)
		return
	}
	out := `null`
	if g.body != nil {
		out = g.body(r)
	}
	_, _ = io.WriteString(w, out)
}

// newMockGatewayHandler builds the verifier without a server, for requests
// synthesised in a test rather than sent over the wire.
func newMockGatewayHandler(t *testing.T, keys map[string]string) *mockGateway {
	return &mockGateway{t: t, keys: keys, now: time.Now, nonces: map[string]bool{}}
}

func newMockGateway(t *testing.T, prefix string) (*mockGateway, *httptest.Server) {
	g := newMockGatewayHandler(t, map[string]string{hmacTestMerchant: hmacTestAPIKey})
	g.prefix = prefix
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	return g, srv
}

// TestClient_HMACv1Gateway runs client methods over HTTP against a server
// that accepts HMAC v1 only.
func TestClient_HMACv1Gateway(t *testing.T) {
	g, srv := newMockGateway(t, "/api")
	c, err := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL+"/api"), WithRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const addr = "0x000000000000000000000000000000000000dEaD"

	calls := map[string]func() error{
		"Credits.Balance": func() error { return discard(c.Credits.Balance(ctx)) },
		"Wallets.List":    func() error { return discard(c.Wallets.List(ctx)) },
		"Payouts.Execute": func() error {
			return discard(c.Payouts.Execute(ctx, &ExecutePayoutRequest{
				OrderID: "o<1>&", UserID: "u", Network: ChainEthMainnet, Coin: "ETH", Amount: "0.5",
				ToAddress: addr, URLCallback: "https://m.example/cb?a=1&b=2",
			}))
		},
		"PayIns.Create": func() error {
			return discard(c.PayIns.Create(ctx, &CreatePayInRequest{
				OrderID: "ё-заказ 😀", UserID: "u", Mode: PayInModeCrypto, AmountCrypto: "1",
				Asset: &Asset{Network: ChainTronMainnet, Coin: "USDT"}, AdditionalData: "{\"k\":\"\u2028\"}",
			}))
		},
		"Webhooks.Resend": func() error { return discard(c.Webhooks.Resend(ctx, "7c9e6679-7425-40de-944b-e07fc1f90ae7")) },
	}
	for name, call := range calls {
		if err := call(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	g.mu.Lock()
	accepted := append([]sentRequest(nil), g.accepted...)
	g.mu.Unlock()
	if len(accepted) != len(calls) {
		t.Fatalf("accepted %d requests, want %d", len(accepted), len(calls))
	}
	for _, s := range accepted {
		if len(s.Signature) != 0 {
			t.Errorf("%s: Signature header sent: %q", s.URLPath, s.Signature)
		}
	}
}

func TestClient_HMACv1GatewayRefusals(t *testing.T) {
	g, srv := newMockGateway(t, "")

	// Capture one signed request as the client sends it.
	var captured *http.Request
	var capturedBody []byte
	capture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		captured = r.Clone(context.Background())
		_, _ = io.WriteString(w, `null`)
	}))
	defer capture.Close()
	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(capture.URL), WithRetries(0))
	if _, err := c.Payouts.Info(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}

	send := func(body []byte, header http.Header) (int, string) {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/payout/info", bytes.NewReader(body))
		req.Header = header.Clone()
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var env struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&env)
		return resp.StatusCode, env.Error
	}

	cases := []struct {
		name   string
		body   []byte
		header http.Header
		status int
		code   string
	}{
		{
			name: "Signature without X-CC-*",
			body: capturedBody,
			header: http.Header{
				"Content-Type": {"application/json"},
				"Merchant":     {hmacTestMerchant},
				"Signature":    {strings.Repeat("0a", 16)},
			},
			status: http.StatusBadRequest, code: CodeBadAuthHeaders,
		},
		{"as sent", capturedBody, captured.Header, http.StatusOK, ""},
		{"replayed nonce", capturedBody, captured.Header, http.StatusUnauthorized, CodeSignatureReplayed},
		{"other body bytes", append(append([]byte{}, capturedBody...), ' '), captured.Header, http.StatusUnauthorized, CodeInvalidSignature},
	}
	for _, tc := range cases {
		status, code := send(tc.body, tc.header)
		if status != tc.status || code != tc.code {
			t.Errorf("%s: %d %q, want %d %q", tc.name, status, code, tc.status, tc.code)
		}
	}

	// The client corrects its clock against the same server.
	g.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	c2, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))
	if _, err := c2.Payouts.Info(context.Background(), "u1"); err != nil {
		t.Fatalf("after clock correction: %v", err)
	}
	c3, _ := New(hmacTestMerchant, "other_key", WithBaseURL(srv.URL), WithRetries(0))
	_, err := c3.Payouts.Info(context.Background(), "u1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != CodeInvalidSignature || apiErr.HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("wrong key: %v", err)
	}
}

// TestTransport_BodyIsEncodingJSON checks the body is the encoding/json output
// of the request value, integers are sent exactly, and the signature covers
// the bytes sent.
func TestTransport_BodyIsEncodingJSON(t *testing.T) {
	type nested struct {
		Zeta  string  `json:"zeta"`
		Alpha float64 `json:"alpha"`
	}
	type request struct {
		Text    string            `json:"text"`
		Big     int64             `json:"big"`
		Max     uint64            `json:"max"`
		Small   float64           `json:"small"`
		Missing *string           `json:"missing"`
		Omitted *string           `json:"omitted,omitempty"`
		Nested  []nested          `json:"nested"`
		Extra   map[string]string `json:"extra"`
	}
	in := request{
		Text:   "<b>&amp;</b> \u2028 é 😀",
		Big:    9007199254740993,
		Max:    18446744073709551615,
		Small:  0.0000001,
		Nested: []nested{{Zeta: "z", Alpha: 1.5}},
		Extra:  map[string]string{"я": "1", "Z": "2"},
	}
	want := `{"text":"\u003cb\u003e\u0026amp;\u003c/b\u003e \u2028 é 😀","big":9007199254740993,"max":18446744073709551615,` +
		`"small":1e-7,"missing":null,"nested":[{"zeta":"z","alpha":1.5}],"extra":{"Z":"2","я":"1"}}`

	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))
	for name, tc := range map[string]struct {
		in   any
		want string
	}{
		"struct":     {in, want},
		"nil":        {nil, ""},
		"empty":      {struct{}{}, "{}"},
		"raw bytes":  {json.RawMessage(`{"b":1,"a":2}`), `{"b":1,"a":2}`},
		"typed nil":  {(*request)(nil), "null"},
		"big number": {map[string]json.Number{"n": "123456789012345678901234567890"}, `{"n":123456789012345678901234567890}`},
	} {
		before := len(rec.all())
		if err := c.do(context.Background(), "/v1/test", tc.in, nil); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		s := rec.all()[before]
		if string(s.Body) != tc.want {
			t.Errorf("%s: body\n got: %s\nwant: %s", name, s.Body, tc.want)
		}
		if err := verifyHMACv1(s, hmacTestAPIKey, "/v1/test"); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// unsignedHeader adds a header after the client has signed the request.
type unsignedHeader struct {
	rt   http.RoundTripper
	name string
	val  string
}

func (h unsignedHeader) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set(h.name, h.val)
	return h.rt.RoundTrip(r)
}

// TestClient_HMACv1GatewayIdempotencyKey: the key from the context reaches the
// gateway inside the signature; the same header set outside the client does
// not, and the gateway refuses the request.
func TestClient_HMACv1GatewayIdempotencyKey(t *testing.T) {
	_, srv := newMockGateway(t, "")

	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))
	ctx := WithIdempotencyKey(context.Background(), "payout-2026-09-16-0001")
	if _, err := c.Payouts.Info(ctx, "u1"); err != nil {
		t.Fatalf("Idempotency-Key from the context: %v", err)
	}

	hc := &http.Client{Transport: unsignedHeader{http.DefaultTransport, headerIdempotencyKey, "payout-0001"}}
	c2, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0), WithHTTPClient(hc))
	_, err := c2.Payouts.Info(context.Background(), "u1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != CodeInvalidSignature {
		t.Fatalf("Idempotency-Key added after signing: err = %v", err)
	}
}
