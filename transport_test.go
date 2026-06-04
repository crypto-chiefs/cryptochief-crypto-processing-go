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
		c.StaticDeposits == nil || c.Blockchain == nil || c.Currencies == nil {
		t.Error("not all sub-clients wired")
	}
}
