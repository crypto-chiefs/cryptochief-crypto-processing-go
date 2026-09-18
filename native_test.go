package cryptochief

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const (
	nativeTestAddress = "0x000000000000000000000000000000000000dEaD"
	nativeTestKey     = "native-2026-09-18-0001"
)

// TestNative_QuoteWireShape asserts Native.Quote posts to /v1/native/quote
// and maps the full price breakdown.
func TestNative_QuoteWireShape(t *testing.T) {
	const apiKey = "k"
	var sent sentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = (&recorder{}).capture(r)
		_, _ = io.WriteString(w, `{"ref":"nq_01JZZ3","network":"ETH_MAINNET","receive_address":"`+nativeTestAddress+`","amount":"0.05","coin_price_usd":"225.50","transfer_fee":"0.000420","transfer_fee_usd":"1.89","subtotal_usd":"227.39","total_usd":"295.61","credits":2956100000,"coin_usd":"4510.00000000","expires_at":"2026-09-18T12:01:30Z","expires_in_sec":90}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", apiKey, WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Native.Quote(context.Background(), &NativeQuoteRequest{
		Network:        ChainEthMainnet,
		ReceiveAddress: nativeTestAddress,
		Amount:         "0.05",
	})
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}
	if sent.URLPath != "/v1/native/quote" {
		t.Errorf("path: %q", sent.URLPath)
	}
	want := `{"amount":"0.05","network":"ETH_MAINNET","receive_address":"` + nativeTestAddress + `"}`
	if !reflect.DeepEqual(jsonValue(t, sent.Body), jsonValue(t, []byte(want))) {
		t.Errorf("body = %s, want %s", sent.Body, want)
	}
	if err := verifyHMACv1(sent, apiKey, "/v1/native/quote"); err != nil {
		t.Error(err)
	}

	if out.Ref != "nq_01JZZ3" {
		t.Errorf("Ref: %q", out.Ref)
	}
	if out.Network != ChainEthMainnet || out.ReceiveAddress != nativeTestAddress || out.Amount != "0.05" {
		t.Errorf("identity: %q / %q / %q", out.Network, out.ReceiveAddress, out.Amount)
	}
	if out.CoinPriceUSD != "225.50" {
		t.Errorf("CoinPriceUSD: %q", out.CoinPriceUSD)
	}
	if out.TransferFee != "0.000420" || out.TransferFeeUSD != "1.89" {
		t.Errorf("transfer fee: %q / %q USD", out.TransferFee, out.TransferFeeUSD)
	}
	if out.SubtotalUSD != "227.39" || out.TotalUSD != "295.61" {
		t.Errorf("price: %q subtotal, %q total (coins + fee at the rate, then the sale price)", out.SubtotalUSD, out.TotalUSD)
	}
	if out.Credits != 2956100000 || out.CoinUSD != "4510.00000000" {
		t.Errorf("charge: %d credits @ %q", out.Credits, out.CoinUSD)
	}
	if out.ExpiresAt != "2026-09-18T12:01:30Z" || out.ExpiresInSec != 90 {
		t.Errorf("expiry: %q / %d", out.ExpiresAt, out.ExpiresInSec)
	}
}

const nativeDeliveredOrder = `{"id":90210,"idempotency_key":"` + nativeTestKey + `","status":"delivered","network":"ETH_MAINNET","receive_address":"` + nativeTestAddress + `","amount":"0.05","tx_hash":"0x9f8e7d6c5b4a39281706f5e4d3c2b1a09f8e7d6c5b4a39281706f5e4d3c2b1a0","transfer_fee":"0.000420","transfer_fee_usd":"1.89","coin_price_usd":"225.50","total_usd":"295.61","credits":2956100000,"coin_usd":"4510.00000000","settled":true,"needs_attention":false,"created_at":"2026-09-18T12:00:00Z","delivered_at":"2026-09-18T12:00:04Z"}`

// TestNative_BuyWireShape asserts Native.Buy posts to /v1/native/buy with the
// Idempotency-Key header from the context, signed, and maps a delivered
// order.
func TestNative_BuyWireShape(t *testing.T) {
	const apiKey = "k"
	var sent sentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = (&recorder{}).capture(r)
		_, _ = io.WriteString(w, nativeDeliveredOrder)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", apiKey, WithBaseURL(srv.URL), WithRetries(0))
	ctx := WithIdempotencyKey(context.Background(), nativeTestKey)
	out, err := c.Native.Buy(ctx, &NativeBuyRequest{QuoteRef: "nq_01JZZ3"})
	if err != nil {
		t.Fatalf("Buy: %v", err)
	}
	if sent.URLPath != "/v1/native/buy" {
		t.Errorf("path: %q", sent.URLPath)
	}
	want := `{"quote_ref":"nq_01JZZ3"}`
	if !reflect.DeepEqual(jsonValue(t, sent.Body), jsonValue(t, []byte(want))) {
		t.Errorf("body = %s, want %s", sent.Body, want)
	}
	if sent.Idempotency != nativeTestKey {
		t.Errorf("Idempotency-Key = %q, want %q", sent.Idempotency, nativeTestKey)
	}
	// The header is part of the string to sign: verifyHMACv1 recomputing the
	// signature with the key proves it.
	if err := verifyHMACv1(sent, apiKey, "/v1/native/buy"); err != nil {
		t.Error(err)
	}

	if out.ID != 90210 || out.IdempotencyKey != nativeTestKey {
		t.Errorf("order identity: %d / %q", out.ID, out.IdempotencyKey)
	}
	if out.Status != NativeOrderStatusDelivered {
		t.Errorf("Status: %q", out.Status)
	}
	if out.TxHash != "0x9f8e7d6c5b4a39281706f5e4d3c2b1a09f8e7d6c5b4a39281706f5e4d3c2b1a0" {
		t.Errorf("TxHash: %q", out.TxHash)
	}
	if out.TransferFee != "0.000420" || out.TransferFeeUSD != "1.89" {
		t.Errorf("transfer fee: %q / %q USD", out.TransferFee, out.TransferFeeUSD)
	}
	if out.CoinPriceUSD != "225.50" || out.TotalUSD != "295.61" {
		t.Errorf("price: %q / total %q", out.CoinPriceUSD, out.TotalUSD)
	}
	if out.Credits != 2956100000 || out.CoinUSD != "4510.00000000" {
		t.Errorf("charge: %d credits @ %q", out.Credits, out.CoinUSD)
	}
	if !out.Settled || out.NeedsAttention {
		t.Errorf("delivered order: settled=%v needs_attention=%v", out.Settled, out.NeedsAttention)
	}
	if out.Error != "" {
		t.Errorf("Error: %q", out.Error)
	}
	if out.CreatedAt != "2026-09-18T12:00:00Z" || out.DeliveredAt != "2026-09-18T12:00:04Z" {
		t.Errorf("timestamps: %q / %q", out.CreatedAt, out.DeliveredAt)
	}
}

// TestNative_BuyRefusedReturnsTheOrder: a refused order answers 502 with the
// order itself as the body — a business outcome, not a transport failure — so
// Buy returns it as a regular order. The fee and rate fields are always on
// the wire ("" / "0.00" when nothing was sent); what a refused order omits is
// the delivery and the charge: tx_hash, total_usd, credits, delivered_at.
func TestNative_BuyRefusedReturnsTheOrder(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body = `{"id":90211,"idempotency_key":"` + nativeTestKey + `","status":"refused","network":"ETH_MAINNET","receive_address":"` + nativeTestAddress + `","amount":"0.05","transfer_fee":"","transfer_fee_usd":"0.00","coin_price_usd":"0.00","coin_usd":"","settled":true,"needs_attention":false,"error_code":"INSUFFICIENT_LIQUIDITY","error":"we cannot fund that sale from our own wallet right now; nothing was bought and nothing was charged","created_at":"2026-09-18T12:00:00Z"}`
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	ctx := WithIdempotencyKey(context.Background(), nativeTestKey)
	order, err := c.Native.Buy(ctx, &NativeBuyRequest{
		Network:        ChainEthMainnet,
		ReceiveAddress: nativeTestAddress,
		Amount:         "0.05",
	})
	if err != nil {
		t.Fatalf("a refused order is returned, not thrown: %v", err)
	}
	if order == nil {
		t.Fatal("no order on a refused answer")
	}
	if order.Status != NativeOrderStatusRefused {
		t.Errorf("Status: %q", order.Status)
	}
	if !order.Settled || order.NeedsAttention {
		t.Errorf("refused order: settled=%v needs_attention=%v", order.Settled, order.NeedsAttention)
	}
	if order.ErrorCode != "INSUFFICIENT_LIQUIDITY" {
		t.Errorf("ErrorCode: %q", order.ErrorCode)
	}
	if order.Error != "we cannot fund that sale from our own wallet right now; nothing was bought and nothing was charged" {
		t.Errorf("Error: %q", order.Error)
	}
	// The fee/rate fields are always sent, zero-valued on a refusal.
	if order.TransferFee != "" || order.TransferFeeUSD != "0.00" ||
		order.CoinPriceUSD != "0.00" || order.CoinUSD != "" {
		t.Errorf("refused fee/rate fields: %+v", order)
	}
	// The delivery and the charge stay OFF the wire — they never happened.
	if order.TxHash != "" || order.TotalUSD != "" || order.Credits != 0 || order.DeliveredAt != "" {
		t.Errorf("a refused order reports no delivery and no charge: %+v", order)
	}
	for _, field := range []string{"tx_hash", "total_usd", "credits", "delivered_at"} {
		if strings.Contains(body, field) {
			t.Errorf("%q must stay off the wire when nothing was sent or charged: %s", field, body)
		}
	}
}

// TestNative_BuyRefusedForCreditsIs402: when the refusal reason is the credits
// balance, the same order view answers 402 instead of 502 — still an order,
// still returned.
func TestNative_BuyRefusedForCreditsIs402(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = io.WriteString(w, `{"id":90212,"idempotency_key":"`+nativeTestKey+`","status":"refused","network":"ETH_MAINNET","receive_address":"`+nativeTestAddress+`","amount":"0.05","transfer_fee":"","transfer_fee_usd":"0.00","coin_price_usd":"0.00","coin_usd":"","settled":true,"needs_attention":false,"error_code":"INSUFFICIENT_CREDITS","error":"your credit balance did not cover this order; nothing was bought and nothing was charged","created_at":"2026-09-18T12:00:00Z"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	ctx := WithIdempotencyKey(context.Background(), nativeTestKey)
	order, err := c.Native.Buy(ctx, &NativeBuyRequest{QuoteRef: "nq_01JZZ3"})
	if err != nil {
		t.Fatalf("a 402 refused order is returned, not thrown: %v", err)
	}
	if order.Status != NativeOrderStatusRefused || order.ErrorCode != CodeInsufficientCredits {
		t.Errorf("order: %q / %q", order.Status, order.ErrorCode)
	}
}

// TestNative_BuyErrorEnvelopeThrows: a non-2xx whose body is an error
// envelope ({"ok":false,...}) is not an order — it throws as usual, with the
// code from "error".
func TestNative_BuyErrorEnvelopeThrows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"ok":false,"error":"QUOTE_EXPIRED","msg":"the quote is no longer usable; re-quote"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	ctx := WithIdempotencyKey(context.Background(), nativeTestKey)
	out, err := c.Native.Buy(ctx, &NativeBuyRequest{QuoteRef: "nq_01JZZ3"})
	if out != nil {
		t.Errorf("order on an error envelope: %+v", out)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Code != "QUOTE_EXPIRED" {
		t.Errorf("Code = %q, want QUOTE_EXPIRED", apiErr.Code)
	}
}

// TestNative_BuyRequiresIdempotencyKey: without a key on the context Buy
// refuses locally — no request is sent, because a keyless retry after a
// timeout would buy the coins a second time.
func TestNative_BuyRequiresIdempotencyKey(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		_, _ = io.WriteString(w, nativeDeliveredOrder)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Native.Buy(context.Background(), &NativeBuyRequest{QuoteRef: "nq_01JZZ3"})
	if err == nil {
		t.Fatalf("Buy without an Idempotency-Key: order %+v, want an error", out)
	}
	if out != nil {
		t.Errorf("order on a local refusal: %+v", out)
	}
	if !strings.Contains(err.Error(), "Idempotency-Key") {
		t.Errorf("error does not name the missing key: %v", err)
	}
	if n := len(rec.all()); n != 0 {
		t.Errorf("requests: %d, want 0 — the refusal is local", n)
	}
}

// TestNative_OrderWireShape asserts Native.Order looks an order up by its
// idempotency key, POSTing {"key":...} to /v1/native/order.
func TestNative_OrderWireShape(t *testing.T) {
	const apiKey = "k"
	var sent sentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = (&recorder{}).capture(r)
		_, _ = io.WriteString(w, nativeDeliveredOrder)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", apiKey, WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Native.Order(context.Background(), nativeTestKey)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if sent.URLPath != "/v1/native/order" {
		t.Errorf("path: %q", sent.URLPath)
	}
	want := `{"key":"` + nativeTestKey + `"}`
	if !reflect.DeepEqual(jsonValue(t, sent.Body), jsonValue(t, []byte(want))) {
		t.Errorf("body = %s, want %s", sent.Body, want)
	}
	if err := verifyHMACv1(sent, apiKey, "/v1/native/order"); err != nil {
		t.Error(err)
	}
	if out.IdempotencyKey != nativeTestKey || out.Status != NativeOrderStatusDelivered {
		t.Errorf("order: %q / %q", out.IdempotencyKey, out.Status)
	}
}
