package cryptochief

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestTransport_RequestShape verifies the wire form the transport sends:
// POST with canonical JSON body, both auth headers populated, signature
// matches independent recomputation.
func TestTransport_RequestShape(t *testing.T) {
	const merchant = "merchant-xyz"
	const apiKey = "test_api_key_123"
	var gotPath, gotMerchant, gotSig string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMerchant = r.Header.Get(headerMerchant)
		gotSig = r.Header.Get(headerSignature)
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"uuid":"u1","status":"queue"}`)
	}))
	defer srv.Close()

	c, _ := New(merchant, apiKey, WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Payouts.Execute(context.Background(), &ExecutePayoutRequest{
		OrderID:     "o1",
		UserID:      "u",
		Network:     ChainEthSepolia,
		Coin:        "ETH",
		Amount:      "0.0001",
		ToAddress:   "0xAbC",
		URLCallback: "https://x/cb",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.UUID != "u1" || out.Status != "queue" {
		t.Errorf("unexpected response: %+v", out)
	}

	if gotPath != "/v1/payout/execute" {
		t.Errorf("path: %q", gotPath)
	}
	if gotMerchant != merchant {
		t.Errorf("merchant header: %q", gotMerchant)
	}
	wantSig := signBody(gotBody, apiKey)
	if gotSig != wantSig {
		t.Errorf("signature mismatch: got %s want %s", gotSig, wantSig)
	}
	// The transport should have sent the canonical (sorted) JSON form.
	if !strings.Contains(string(gotBody), `"amount":"0.0001"`) {
		t.Errorf("body missing fields: %s", gotBody)
	}
	if !strings.HasPrefix(string(gotBody), `{"amount":`) {
		t.Errorf("body not canonical (sorted) — starts with %s", gotBody[:20])
	}
}

// TestTransport_ParsesErrorEnvelope confirms the API's error response
// shape is decoded into a typed *APIError with the stable Code.
func TestTransport_ParsesErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"SERVICE_ERROR","msg":"INSUFFICIENT_FUNDS","ok":false}`)
	}))
	defer srv.Close()

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	_, err := c.Payouts.Info(context.Background(), "u")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Code != CodeInsufficientFunds {
		t.Errorf("code: %s", apiErr.Code)
	}
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Errorf("errors.Is(ErrInsufficientFunds) should match")
	}
}

// TestParseAPIError_BothEnvelopeShapes pins the resolution rule across the
// two shapes the API uses: the gateway's own refusals carry the machine code
// in "error" and an English sentence in "msg", while refusals relayed from
// upstream mark "error" as SERVICE_ERROR and carry the code in "msg". Both
// must land in Code, and Message must keep the human text.
func TestParseAPIError_BothEnvelopeShapes(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		body        string
		wantCode    string
		wantMessage string
	}{
		{
			name:        "gateway refusal keeps its code in error",
			status:      http.StatusBadRequest,
			body:        `{"ok":false,"error":"LABEL_TOO_LONG","msg":"label is longer than 255 characters"}`,
			wantCode:    CodeLabelTooLong,
			wantMessage: "label is longer than 255 characters",
		},
		{
			name:        "upstream refusal keeps its code in msg",
			status:      http.StatusBadRequest,
			body:        `{"ok":false,"error":"SERVICE_ERROR","msg":"wallet_not_found"}`,
			wantCode:    "wallet_not_found",
			wantMessage: "wallet_not_found",
		},
		{
			name:        "gateway billing refusal",
			status:      http.StatusPaymentRequired,
			body:        `{"ok":false,"error":"INSUFFICIENT_CREDITS","msg":"not enough credits to cover gas"}`,
			wantCode:    CodeInsufficientCredits,
			wantMessage: "not enough credits to cover gas",
		},
		{
			name:        "SERVICE_ERROR with no detail stays SERVICE_ERROR",
			status:      http.StatusBadGateway,
			body:        `{"ok":false,"error":"SERVICE_ERROR"}`,
			wantCode:    CodeServiceError,
			wantMessage: CodeServiceError,
		},
		{
			name:        "error only",
			status:      http.StatusBadRequest,
			body:        `{"ok":false,"error":"UNKNOWN_FIELD"}`,
			wantCode:    "UNKNOWN_FIELD",
			wantMessage: "UNKNOWN_FIELD",
		},
		{
			name:        "unparseable body falls back to the status",
			status:      http.StatusInternalServerError,
			body:        `<html>502 Bad Gateway</html>`,
			wantCode:    "HTTP_500",
			wantMessage: "",
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
			if apiErr.HTTPStatus != tc.status {
				t.Errorf("HTTPStatus = %d, want %d", apiErr.HTTPStatus, tc.status)
			}
			// Raw must keep the whole body — nothing is dropped on the way.
			if string(apiErr.Raw) != tc.body {
				t.Errorf("Raw = %q, want %q", apiErr.Raw, tc.body)
			}
		})
	}
}

// TestTransport_GatewayCodeReachesCaller is the end-to-end form: a refusal
// the gateway decided itself must arrive with the constant in Code, must
// match the sentinel through errors.Is, and must still carry the English
// sentence for a human to read.
func TestTransport_GatewayCodeReachesCaller(t *testing.T) {
	const sentence = "label is longer than 255 characters"
	body := `{"ok":false,"error":"LABEL_TOO_LONG","msg":"` + sentence + `"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	_, err := c.Wallets.SetLabel(context.Background(), "0xabc", strings.Repeat("x", 256))
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	// The constant the changelog advertises must actually match.
	if apiErr.Code != CodeLabelTooLong {
		t.Errorf("Code = %q, want %q", apiErr.Code, CodeLabelTooLong)
	}
	// The human sentence is still there, not overwritten by the code.
	if apiErr.Message != sentence {
		t.Errorf("Message = %q, want %q", apiErr.Message, sentence)
	}
	// ...and it is in the Error() string, so a bare %v still reads well.
	if !strings.Contains(err.Error(), sentence) {
		t.Errorf("Error() lost the message: %s", err)
	}
	if string(apiErr.Raw) != body {
		t.Errorf("Raw = %q, want %q", apiErr.Raw, body)
	}
}

// TestTransport_SentinelMatchesGatewayCode confirms errors.Is now fires for
// a gateway-decided code, not only for the upstream-relayed ones.
func TestTransport_SentinelMatchesGatewayCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = io.WriteString(w,
			`{"ok":false,"error":"DEBT_LIMIT_EXCEEDED","msg":"outstanding debt exceeds the 50 USD limit"}`)
	}))
	defer srv.Close()

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	_, err := c.Payouts.Info(context.Background(), "u")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrDebtLimitExceeded) {
		t.Errorf("errors.Is(ErrDebtLimitExceeded) should match, got %v", err)
	}
	if errors.Is(err, ErrInsufficientFunds) {
		t.Error("errors.Is must not match an unrelated sentinel")
	}
}

func TestTransport_RetriesOn5xx(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n < 3 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"error":"upstream timeout","ok":false}`)
			return
		}
		_, _ = io.WriteString(w, `{"uuid":"u1","status":"paid"}`)
	}))
	defer srv.Close()

	c, _ := New("m", "k",
		WithBaseURL(srv.URL),
		WithRetries(3),
		// Tighten backoff so the test stays fast.
		WithRetryBackoff(1*time.Millisecond, 5*time.Millisecond),
	)
	out, err := c.Payouts.Info(context.Background(), "u1")
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if out.UUID != "u1" {
		t.Errorf("unexpected response: %+v", out)
	}
	if atomic.LoadInt32(&hits) != 3 {
		t.Errorf("expected 3 hits, got %d", hits)
	}
}

func TestTransport_DoesNotRetry4xx(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"SERVICE_ERROR","msg":"ASSET_NOT_ENABLED","ok":false}`)
	}))
	defer srv.Close()

	c, _ := New("m", "k",
		WithBaseURL(srv.URL),
		WithRetries(3),
		WithRetryBackoff(1*time.Millisecond, 5*time.Millisecond),
	)
	_, err := c.Payouts.Info(context.Background(), "u1")
	if err == nil {
		t.Fatal("expected error")
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Errorf("4xx should not retry: got %d hits", hits)
	}
}

func TestTransport_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := c.Payouts.Info(ctx, "u")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestNew_ValidatesCredentials(t *testing.T) {
	if _, err := New("", "k"); err == nil {
		t.Error("empty merchant should fail")
	}
	if _, err := New("m", ""); err == nil {
		t.Error("empty api key should fail")
	}
	c, err := New("m", "k")
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL() != DefaultBaseURL {
		t.Errorf("default base URL: %s", c.BaseURL())
	}
	if c.MerchantID() != "m" {
		t.Errorf("MerchantID: %s", c.MerchantID())
	}
	if c.Payouts == nil || c.Transactions == nil || c.PayIns == nil ||
		c.Wallets == nil || c.Sweeps == nil || c.Withdrawals == nil ||
		c.StaticDeposits == nil || c.Blockchain == nil || c.Currencies == nil ||
		c.Credits == nil {
		t.Error("not all sub-clients wired")
	}
}
