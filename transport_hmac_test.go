package cryptochief

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	hmacTestMerchant = "3f2a1b4c-5d6e-7f80-9a1b-2c3d4e5f6071"
	hmacTestAPIKey   = "test_api_key_123"
	hmacTestUnix     = int64(1789430400)
)

var hexNonce32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

// sentRequest is what the test server saw.
type sentRequest struct {
	Method      string
	URLPath     string
	RawQuery    string
	RequestURI  string // path and query as they went on the wire
	ContentType string
	Merchant    string
	Signature   []string // values of a "Signature" header; the client sends none
	Timestamp   string
	Nonce       string
	HMAC        string // X-CC-Signature
	Idempotency string
	Body        []byte
}

type recorder struct {
	mu   sync.Mutex
	reqs []sentRequest
}

func (rec *recorder) capture(r *http.Request) sentRequest {
	body, _ := io.ReadAll(r.Body)
	s := sentRequest{
		Method:      r.Method,
		URLPath:     r.URL.Path,
		RawQuery:    r.URL.RawQuery,
		RequestURI:  r.RequestURI,
		ContentType: r.Header.Get("Content-Type"),
		Merchant:    r.Header.Get(headerMerchant),
		Signature:   r.Header.Values("Signature"),
		Timestamp:   r.Header.Get(HeaderTimestamp),
		Nonce:       r.Header.Get(headerNonce),
		HMAC:        r.Header.Get(HeaderSignature),
		Idempotency: r.Header.Get(headerIdempotencyKey),
		Body:        body,
	}
	rec.mu.Lock()
	rec.reqs = append(rec.reqs, s)
	rec.mu.Unlock()
	return s
}

func (rec *recorder) all() []sentRequest {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]sentRequest(nil), rec.reqs...)
}

// verifyHMACv1 recomputes X-CC-Signature from what was received, signing
// routePath instead of the transport path, and checks that no Signature
// header was sent.
func verifyHMACv1(s sentRequest, apiKey, routePath string) error {
	if len(s.Signature) != 0 {
		return fmt.Errorf("Signature header sent: %q", s.Signature)
	}
	if !hexNonce32.MatchString(s.Nonce) {
		return fmt.Errorf("nonce %q is not 32 lowercase hex", s.Nonce)
	}
	if _, err := strconv.ParseInt(s.Timestamp, 10, 64); err != nil {
		return fmt.Errorf("timestamp %q: %v", s.Timestamp, err)
	}
	want, err := SignHMACv1(apiKey, HMACv1Input{
		Timestamp:      s.Timestamp,
		Nonce:          s.Nonce,
		Method:         s.Method,
		Path:           routePath,
		Query:          s.RawQuery,
		Merchant:       s.Merchant,
		IdempotencyKey: s.Idempotency,
		Body:           s.Body,
	})
	if err != nil {
		return err
	}
	if s.HMAC != "v1="+want {
		return fmt.Errorf("X-CC-Signature = %q, want %q", s.HMAC, "v1="+want)
	}
	return nil
}

func fixedClock(unix int64) func() time.Time {
	return func() time.Time { return time.Unix(unix, 0) }
}

func TestTransport_HMACv1Headers(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		_, _ = io.WriteString(w, `{"uuid":"u1","status":"queue"}`)
	}))
	defer srv.Close()

	// A base URL with a path prefix: the signed path is still the API route.
	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL+"/prefix"), WithRetries(0))
	c.now = fixedClock(hmacTestUnix)

	_, err := c.Payouts.Execute(context.Background(), &ExecutePayoutRequest{
		OrderID: "o1", UserID: "u", Network: ChainEthSepolia, Coin: "ETH",
		Amount: "0.0001", ToAddress: "0xAbC", URLCallback: "https://x/cb?a=1&b=2",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	reqs := rec.all()
	if len(reqs) != 1 {
		t.Fatalf("requests: %d", len(reqs))
	}
	s := reqs[0]
	if s.URLPath != "/prefix/v1/payout/execute" {
		t.Errorf("URL path: %q", s.URLPath)
	}
	if s.Timestamp != strconv.FormatInt(hmacTestUnix, 10) {
		t.Errorf("X-CC-Timestamp: %q", s.Timestamp)
	}
	if s.Merchant != hmacTestMerchant {
		t.Errorf("Merchant: %q", s.Merchant)
	}
	if s.ContentType != "application/json" {
		t.Errorf("Content-Type: %q", s.ContentType)
	}
	if err := verifyHMACv1(s, hmacTestAPIKey, "/v1/payout/execute"); err != nil {
		t.Error(err)
	}
}

func TestTransport_HMACv1QueryFromURL(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))
	if err := c.do(context.Background(), "/v1/payments/history?a=1&b=2", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	s := rec.all()[0]
	if s.RawQuery != "a=1&b=2" {
		t.Fatalf("query: %q", s.RawQuery)
	}
	if err := verifyHMACv1(s, hmacTestAPIKey, "/v1/payments/history"); err != nil {
		t.Error(err)
	}
}

func TestTransport_HMACv1IdempotencyKeySigned(t *testing.T) {
	c, _ := New(hmacTestMerchant, hmacTestAPIKey)
	c.now = fixedClock(hmacTestUnix)
	body := []byte(`{"order_id":"po-1"}`)

	req, _ := http.NewRequest(http.MethodPost, "https://api.example/v1/payout/execute", nil)
	req.Header.Set(headerMerchant, hmacTestMerchant)
	req.Header.Set(headerIdempotencyKey, "payout-2026-09-15-0001")
	if err := c.signHMACv1(req, "/v1/payout/execute", body); err != nil {
		t.Fatal(err)
	}

	in := HMACv1Input{
		Timestamp:      req.Header.Get(HeaderTimestamp),
		Nonce:          req.Header.Get(headerNonce),
		Method:         http.MethodPost,
		Path:           "/v1/payout/execute",
		Merchant:       hmacTestMerchant,
		IdempotencyKey: "payout-2026-09-15-0001",
		Body:           body,
	}
	want, _ := SignHMACv1(hmacTestAPIKey, in)
	if got := req.Header.Get(HeaderSignature); got != "v1="+want {
		t.Fatalf("X-CC-Signature = %q, want %q", got, "v1="+want)
	}
	in.IdempotencyKey = ""
	without, _ := SignHMACv1(hmacTestAPIKey, in)
	if without == want {
		t.Fatal("Idempotency-Key does not change the signature")
	}
}

func TestTransport_HMACv1RecomputedOnRetry(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		if n := len(rec.all()); n < 3 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"ok":false,"error":"SERVICE_ERROR"}`)
			return
		}
		_, _ = io.WriteString(w, `{"uuid":"u1","status":"paid"}`)
	}))
	defer srv.Close()

	c, _ := New(hmacTestMerchant, hmacTestAPIKey,
		WithBaseURL(srv.URL),
		WithRetries(3),
		WithRetryBackoff(time.Millisecond, 2*time.Millisecond),
	)
	tick := hmacTestUnix
	c.now = func() time.Time {
		tick += 7
		return time.Unix(tick, 0)
	}

	if _, err := c.Payouts.Info(context.Background(), "u1"); err != nil {
		t.Fatalf("Info: %v", err)
	}

	reqs := rec.all()
	if len(reqs) != 3 {
		t.Fatalf("requests: %d, want 3", len(reqs))
	}
	seenTS := map[string]bool{}
	seenNonce := map[string]bool{}
	seenSig := map[string]bool{}
	for i, s := range reqs {
		if err := verifyHMACv1(s, hmacTestAPIKey, "/v1/payout/info"); err != nil {
			t.Errorf("attempt %d: %v", i, err)
		}
		seenTS[s.Timestamp] = true
		seenNonce[s.Nonce] = true
		seenSig[s.HMAC] = true
	}
	if len(seenTS) != 3 || len(seenNonce) != 3 || len(seenSig) != 3 {
		t.Errorf("not recomputed per attempt: %d timestamps, %d nonces, %d signatures",
			len(seenTS), len(seenNonce), len(seenSig))
	}
}

// Bodies of 401 SIGNATURE_TIMESTAMP_OUT_OF_RANGE for a server_time.
func gatewayTimestampRefusal(serverUnix int64) string {
	return fmt.Sprintf(`{"ok":false,"error":"SIGNATURE_TIMESTAMP_OUT_OF_RANGE","msg":"X-CC-Timestamp differs from server time by more than 300 seconds","server_time":%d}`, serverUnix)
}

func installationTimestampRefusal(serverUnix int64) string {
	return fmt.Sprintf(`{"data":null,"error":{"status":401,"name":"UnauthorizedError","message":"X-CC-Timestamp differs from server time by more than 300 seconds","details":{"code":"SIGNATURE_TIMESTAMP_OUT_OF_RANGE","server_time":%d}},"server_time":%d}`, serverUnix, serverUnix)
}

// skewServer answers SIGNATURE_TIMESTAMP_OUT_OF_RANGE with refusal(serverUnix)
// while X-CC-Timestamp is more than 300 seconds from serverUnix.
func skewServer(rec *recorder, serverUnix int64, refusal func(int64) string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := rec.capture(r)
		ts, _ := strconv.ParseInt(s.Timestamp, 10, 64)
		if d := serverUnix - ts; d > 300 || d < -300 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, refusal(serverUnix))
			return
		}
		_, _ = io.WriteString(w, `{"uuid":"u1","status":"paid"}`)
	}))
}

var timestampRefusals = []struct {
	name string
	body func(int64) string
}{
	{"gateway", gatewayTimestampRefusal},
	{"installation", installationTimestampRefusal},
	{"installation details only", func(serverUnix int64) string {
		return fmt.Sprintf(`{"data":null,"error":{"status":401,"name":"UnauthorizedError","message":"out of range","details":{"code":"SIGNATURE_TIMESTAMP_OUT_OF_RANGE","server_time":%d}}}`, serverUnix)
	}},
}

func TestTransport_ClockSkewCorrected(t *testing.T) {
	for _, format := range timestampRefusals {
		for _, skew := range []int64{1000, -1000} {
			t.Run(format.name+"/"+strconv.FormatInt(skew, 10), func(t *testing.T) {
				testClockSkewCorrected(t, skew, format.body)
			})
		}
	}
}

func testClockSkewCorrected(t *testing.T, skew int64, refusal func(int64) string) {
	serverUnix := hmacTestUnix + skew
	rec := &recorder{}
	srv := skewServer(rec, serverUnix, refusal)
	defer srv.Close()

	// No retry budget: the correction is a separate repeat.
	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))
	c.now = fixedClock(hmacTestUnix)

	if _, err := c.Payouts.Info(context.Background(), "u1"); err != nil {
		t.Fatalf("Info: %v", err)
	}
	reqs := rec.all()
	if len(reqs) != 2 {
		t.Fatalf("requests: %d, want 2", len(reqs))
	}
	if reqs[0].Timestamp != strconv.FormatInt(hmacTestUnix, 10) {
		t.Errorf("first X-CC-Timestamp: %s", reqs[0].Timestamp)
	}
	if reqs[1].Timestamp != strconv.FormatInt(serverUnix, 10) {
		t.Errorf("repeated X-CC-Timestamp: %s, want %d", reqs[1].Timestamp, serverUnix)
	}
	if reqs[0].Nonce == reqs[1].Nonce {
		t.Error("nonce reused on repeat")
	}
	for i, s := range reqs {
		if err := verifyHMACv1(s, hmacTestAPIKey, "/v1/payout/info"); err != nil {
			t.Errorf("request %d: %v", i, err)
		}
	}

	// The offset stays for later requests.
	if _, err := c.Payouts.Info(context.Background(), "u1"); err != nil {
		t.Fatalf("second Info: %v", err)
	}
	reqs = rec.all()
	if len(reqs) != 3 || reqs[2].Timestamp != strconv.FormatInt(serverUnix, 10) {
		t.Errorf("later request: %d requests, X-CC-Timestamp %s", len(reqs), reqs[len(reqs)-1].Timestamp)
	}
}

func TestTransport_ClockSkewCorrectedOnce(t *testing.T) {
	for _, format := range timestampRefusals {
		t.Run(format.name, func(t *testing.T) {
			testClockSkewCorrectedOnce(t, format.body)
		})
	}
}

func testClockSkewCorrectedOnce(t *testing.T, refusal func(int64) string) {
	rec := &recorder{}
	var mu sync.Mutex
	serverUnix := hmacTestUnix
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		// Server time keeps running away from the corrected client.
		mu.Lock()
		serverUnix += 1000
		st := serverUnix
		mu.Unlock()
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, refusal(st))
	}))
	defer srv.Close()

	c, _ := New(hmacTestMerchant, hmacTestAPIKey,
		WithBaseURL(srv.URL),
		WithRetries(3),
		WithRetryBackoff(time.Millisecond, 2*time.Millisecond),
	)
	c.now = fixedClock(hmacTestUnix)

	_, err := c.Payouts.Info(context.Background(), "u1")
	if !errors.Is(err, ErrSignatureTimestampOutOfRange) {
		t.Fatalf("err = %v, want SIGNATURE_TIMESTAMP_OUT_OF_RANGE", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus != http.StatusUnauthorized ||
		apiErr.Code != CodeSignatureTimestampOutOfRange {
		t.Errorf("err = %#v", err)
	}
	if n := len(rec.all()); n != 2 {
		t.Errorf("requests: %d, want 2", n)
	}
}

func TestTransport_ClockSkewWithoutServerTime(t *testing.T) {
	bodies := map[string]string{
		"gateway":      `{"ok":false,"error":"SIGNATURE_TIMESTAMP_OUT_OF_RANGE","msg":"out of range"}`,
		"installation": `{"data":null,"error":{"status":401,"name":"UnauthorizedError","message":"out of range","details":{"code":"SIGNATURE_TIMESTAMP_OUT_OF_RANGE"}}}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			rec := &recorder{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec.capture(r)
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, body)
			}))
			defer srv.Close()

			c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(3))
			_, err := c.Payouts.Info(context.Background(), "u1")
			if !errors.Is(err, ErrSignatureTimestampOutOfRange) {
				t.Fatalf("err = %v", err)
			}
			if n := len(rec.all()); n != 1 {
				t.Errorf("requests: %d, want 1", n)
			}
			if off := c.clockOffset.Load(); off != 0 {
				t.Errorf("clock offset changed: %d", off)
			}
		})
	}
}

func TestTransport_SignatureAndBodyLimitCodes(t *testing.T) {
	cases := []struct {
		status   int
		body     string
		sentinel error
		code     string
	}{
		{http.StatusBadRequest, `{"ok":false,"error":"BAD_AUTH_HEADERS","msg":"Merchant, X-CC-Timestamp, X-CC-Nonce and X-CC-Signature headers are required in the documented format"}`, ErrBadAuthHeaders, CodeBadAuthHeaders},
		{http.StatusUnauthorized, `{"ok":false,"error":"INVALID_SIGNATURE","msg":"signature mismatch"}`, &APIError{Code: CodeInvalidSignature}, CodeInvalidSignature},
		{http.StatusUnauthorized, `{"ok":false,"error":"SIGNATURE_REPLAYED","msg":"X-CC-Nonce has already been used"}`, ErrSignatureReplayed, CodeSignatureReplayed},
		{http.StatusRequestEntityTooLarge, `{"ok":false,"error":"PAYLOAD_TOO_LARGE","msg":"request body exceeds 8388608 bytes"}`, ErrPayloadTooLarge, CodePayloadTooLarge},
		{http.StatusBadRequest, `{"data":null,"error":{"status":400,"name":"BadRequestError","message":"X-CC-Nonce is malformed","details":{"code":"BAD_AUTH_HEADERS"}}}`, ErrBadAuthHeaders, CodeBadAuthHeaders},
		{http.StatusUnauthorized, `{"data":null,"error":{"status":401,"name":"UnauthorizedError","message":"X-CC-Nonce has already been used","details":{"code":"SIGNATURE_REPLAYED"}}}`, ErrSignatureReplayed, CodeSignatureReplayed},
		{http.StatusRequestEntityTooLarge, `{"data":null,"error":{"status":413,"name":"PayloadTooLargeError","message":"request body exceeds 8388608 bytes","details":{"code":"PAYLOAD_TOO_LARGE"}}}`, ErrPayloadTooLarge, CodePayloadTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			rec := &recorder{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec.capture(r)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(3))
			_, err := c.Payouts.Info(context.Background(), "u1")
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Code != tc.code || apiErr.HTTPStatus != tc.status {
				t.Fatalf("err = %v", err)
			}
			if !errors.Is(err, tc.sentinel) {
				t.Errorf("errors.Is(%s) should match", tc.code)
			}
			if !strings.Contains(err.Error(), apiErr.Message) {
				t.Errorf("Error() lost the message: %s", err)
			}
			if n := len(rec.all()); n != 1 {
				t.Errorf("requests: %d, want 1", n)
			}
		})
	}
}

func TestParseAPIError_InstallationFormat(t *testing.T) {
	const outOfRange = "X-CC-Timestamp differs from server time by more than 300 seconds"
	cases := []struct {
		name           string
		status         int
		body           string
		wantCode       string
		wantMessage    string
		wantServerTime int64
	}{
		{
			name:           "timestamp refusal",
			status:         http.StatusUnauthorized,
			body:           installationTimestampRefusal(hmacTestUnix),
			wantCode:       CodeSignatureTimestampOutOfRange,
			wantMessage:    outOfRange,
			wantServerTime: hmacTestUnix,
		},
		{
			name:           "server_time in details only",
			status:         http.StatusUnauthorized,
			body:           `{"data":null,"error":{"status":401,"name":"UnauthorizedError","message":"m","details":{"code":"SIGNATURE_TIMESTAMP_OUT_OF_RANGE","server_time":1789430400}}}`,
			wantCode:       CodeSignatureTimestampOutOfRange,
			wantMessage:    "m",
			wantServerTime: 1789430400,
		},
		{
			name:        "invalid signature",
			status:      http.StatusUnauthorized,
			body:        `{"data":null,"error":{"status":401,"name":"UnauthorizedError","message":"signature mismatch","details":{"code":"INVALID_SIGNATURE"}}}`,
			wantCode:    CodeInvalidSignature,
			wantMessage: "signature mismatch",
		},
		{
			name:        "no code in details falls back to name",
			status:      http.StatusBadRequest,
			body:        `{"data":null,"error":{"status":400,"name":"ValidationError","message":"amount is required","details":{}}}`,
			wantCode:    "ValidationError",
			wantMessage: "amount is required",
		},
		{
			name:        "details not an object",
			status:      http.StatusBadRequest,
			body:        `{"data":null,"error":{"status":400,"name":"ValidationError","message":"bad","details":["x"]}}`,
			wantCode:    "ValidationError",
			wantMessage: "bad",
		},
		{
			name:        "empty error object falls back to the status",
			status:      http.StatusForbidden,
			body:        `{"data":null,"error":{}}`,
			wantCode:    "HTTP_403",
			wantMessage: "",
		},
		{
			name:           "gateway server_time",
			status:         http.StatusUnauthorized,
			body:           gatewayTimestampRefusal(hmacTestUnix),
			wantCode:       CodeSignatureTimestampOutOfRange,
			wantMessage:    outOfRange,
			wantServerTime: hmacTestUnix,
		},
		{
			name:        "non-numeric server_time is ignored",
			status:      http.StatusUnauthorized,
			body:        `{"ok":false,"error":"SIGNATURE_TIMESTAMP_OUT_OF_RANGE","msg":"m","server_time":"1789430400"}`,
			wantCode:    CodeSignatureTimestampOutOfRange,
			wantMessage: "m",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			apiErr := parseAPIError(tc.status, []byte(tc.body))
			if apiErr.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", apiErr.Code, tc.wantCode)
			}
			if apiErr.Message != tc.wantMessage {
				t.Errorf("Message = %q, want %q", apiErr.Message, tc.wantMessage)
			}
			if apiErr.serverTime != tc.wantServerTime {
				t.Errorf("serverTime = %d, want %d", apiErr.serverTime, tc.wantServerTime)
			}
			if apiErr.HTTPStatus != tc.status || string(apiErr.Raw) != tc.body {
				t.Errorf("HTTPStatus = %d, Raw = %q", apiErr.HTTPStatus, apiErr.Raw)
			}
		})
	}
}

// TestTransport_IdempotencyKeyFromContext: a key on the context is sent and
// covered by the signature; without one no header goes out.
func TestTransport_IdempotencyKeyFromContext(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	const key = "payout-2026-09-16-0001"
	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))
	ctx := WithIdempotencyKey(context.Background(), key)
	if got := IdempotencyKeyFromContext(ctx); got != key {
		t.Fatalf("IdempotencyKeyFromContext = %q", got)
	}
	if err := c.do(ctx, "/v1/payout/execute", map[string]string{"order_id": "o1"}, nil); err != nil {
		t.Fatal(err)
	}
	s := rec.all()[0]
	if s.Idempotency != key {
		t.Fatalf("Idempotency-Key = %q, want %q", s.Idempotency, key)
	}
	if err := verifyHMACv1(s, hmacTestAPIKey, "/v1/payout/execute"); err != nil {
		t.Error(err)
	}
	// The same request signed without the key gives a different signature, so
	// the check above is not vacuous.
	without, err := SignHMACv1(hmacTestAPIKey, HMACv1Input{
		Timestamp: s.Timestamp, Nonce: s.Nonce, Method: s.Method, Path: "/v1/payout/execute",
		Query: s.RawQuery, Merchant: s.Merchant, Body: s.Body,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.HMAC == "v1="+without {
		t.Error("Idempotency-Key is not covered by the signature")
	}

	if err := c.do(context.Background(), "/v1/payout/execute", map[string]string{"order_id": "o1"}, nil); err != nil {
		t.Fatal(err)
	}
	if plain := rec.all()[1]; plain.Idempotency != "" {
		t.Errorf("Idempotency-Key without a key on the context: %q", plain.Idempotency)
	}
}

// TestTransport_IdempotencyKeyRefused: a key the server would read differently
// from the signed value fails the call before anything is sent.
func TestTransport_IdempotencyKeyRefused(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))
	for _, key := range []string{" po-1", "po-1 ", "po\t1", "\tpo-1", "po-1\r", "po-1\n", "заказ-1", "po 1"} {
		err := c.do(WithIdempotencyKey(context.Background(), key), "/v1/payout/info", nil, nil)
		if !errors.Is(err, ErrIdempotencyKey) {
			t.Errorf("key %q: err = %v, want ErrIdempotencyKey", key, err)
		}
	}
	if n := len(rec.all()); n != 0 {
		t.Errorf("%d requests sent", n)
	}
	// An empty key is not an error; it sends no header.
	if err := c.do(WithIdempotencyKey(context.Background(), ""), "/v1/payout/info", nil, nil); err != nil {
		t.Fatal(err)
	}
	if s := rec.all()[0]; s.Idempotency != "" {
		t.Errorf("Idempotency-Key for an empty key: %q", s.Idempotency)
	}
}

// TestTransport_IdempotencyKeyOnRetry: every attempt carries the key and is
// signed with it.
func TestTransport_IdempotencyKeyOnRetry(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		if len(rec.all()) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"ok":false,"error":"SERVICE_ERROR"}`)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	const key = "batch-7"
	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL),
		WithRetryBackoff(time.Millisecond, time.Millisecond))
	if err := c.do(WithIdempotencyKey(context.Background(), key), "/v1/payout/info", nil, nil); err != nil {
		t.Fatal(err)
	}
	sent := rec.all()
	if len(sent) != 3 {
		t.Fatalf("attempts: %d", len(sent))
	}
	for i, s := range sent {
		if s.Idempotency != key {
			t.Errorf("attempt %d: Idempotency-Key = %q", i, s.Idempotency)
		}
		if err := verifyHMACv1(s, hmacTestAPIKey, "/v1/payout/info"); err != nil {
			t.Errorf("attempt %d: %v", i, err)
		}
	}
}
