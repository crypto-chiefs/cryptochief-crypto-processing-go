package cryptochief

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	energyTestAddress = "TNPee8f4rZQ7eHWvRzYFqhJmZ8zK5cEkLm"
	energyTestKey     = "energy-2026-09-18-0001"
)

// TestEnergy_QuoteWireShape asserts Energy.Quote posts to /v1/energy/quote
// and maps the full quote, burn and saving figures.
func TestEnergy_QuoteWireShape(t *testing.T) {
	const apiKey = "k"
	var sent sentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = (&recorder{}).capture(r)
		_, _ = io.WriteString(w, `{"ref":"q_01JZZ3","receive_address":"`+energyTestAddress+`","energy":64285,"duration_sec":3600,"price_sun":1542840,"price_trx":"1.542840","price_usd":"0.46","credits":4600000,"trx_usd":"0.29750000","recipient_state":"warm","burn_price_sun":26993700,"burn_price_trx":"26.993700","burn_price_usd":"8.03","burn_price_credits":80300000,"saving_trx":"25.450860","saving_usd":"7.57","saving_credits":75700000,"expires_at":"2026-09-18T12:05:00Z","expires_in_sec":300}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", apiKey, WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Energy.Quote(context.Background(), &EnergyQuoteRequest{
		ReceiveAddress: energyTestAddress,
		Energy:         64285,
		DurationSec:    3600,
	})
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}
	if sent.URLPath != "/v1/energy/quote" {
		t.Errorf("path: %q", sent.URLPath)
	}
	want := `{"duration_sec":3600,"energy":64285,"receive_address":"` + energyTestAddress + `"}`
	if !reflect.DeepEqual(jsonValue(t, sent.Body), jsonValue(t, []byte(want))) {
		t.Errorf("body = %s, want %s", sent.Body, want)
	}
	if err := verifyHMACv1(sent, apiKey, "/v1/energy/quote"); err != nil {
		t.Error(err)
	}

	if out.Ref != "q_01JZZ3" {
		t.Errorf("Ref: %q", out.Ref)
	}
	if out.ReceiveAddress != energyTestAddress {
		t.Errorf("ReceiveAddress: %q", out.ReceiveAddress)
	}
	if out.Energy != 64285 || out.DurationSec != 3600 {
		t.Errorf("Energy/DurationSec: %d/%d", out.Energy, out.DurationSec)
	}
	if out.PriceSUN != 1542840 || out.PriceTRX != "1.542840" {
		t.Errorf("price: %d SUN / %q TRX", out.PriceSUN, out.PriceTRX)
	}
	if out.PriceUSD != "0.46" || out.Credits != 4600000 || out.TRXUSD != "0.29750000" {
		t.Errorf("charge: %q USD / %d credits @ %q", out.PriceUSD, out.Credits, out.TRXUSD)
	}
	if out.RecipientState != "warm" {
		t.Errorf("RecipientState: %q", out.RecipientState)
	}
	if out.BurnPriceSUN != 26993700 || out.BurnPriceTRX != "26.993700" ||
		out.BurnPriceUSD != "8.03" || out.BurnPriceCredits != 80300000 {
		t.Errorf("burn price: %+v", out)
	}
	if out.SavingTRX != "25.450860" || out.SavingUSD != "7.57" || out.SavingCredits != 75700000 {
		t.Errorf("saving: %+v", out)
	}
	if out.ExpiresAt != "2026-09-18T12:05:00Z" || out.ExpiresInSec != 300 {
		t.Errorf("expiry: %q / %d", out.ExpiresAt, out.ExpiresInSec)
	}
}

// TestEnergy_QuoteNoRate: when no TRX/USD rate is available the quote carries
// no dollar or credits figures — "" / 0, not a guessed rate.
func TestEnergy_QuoteNoRate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ref":"q_01JZZ4","receive_address":"`+energyTestAddress+`","energy":130285,"duration_sec":3600,"price_sun":3126840,"price_trx":"3.126840","recipient_state":"cold","burn_price_sun":54719700,"burn_price_trx":"54.719700","saving_trx":"51.592860","expires_at":"2026-09-18T12:05:00Z","expires_in_sec":299}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Energy.Quote(context.Background(), &EnergyQuoteRequest{ReceiveAddress: energyTestAddress})
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}
	if out.PriceUSD != "" || out.Credits != 0 || out.TRXUSD != "" ||
		out.BurnPriceUSD != "" || out.BurnPriceCredits != 0 ||
		out.SavingUSD != "" || out.SavingCredits != 0 {
		t.Errorf("rate-derived fields must stay empty without a rate: %+v", out)
	}
	if out.RecipientState != "cold" {
		t.Errorf("RecipientState: %q", out.RecipientState)
	}
}

const energyDeliveredOrder = `{"id":481516,"idempotency_key":"` + energyTestKey + `","status":"delivered","receive_address":"` + energyTestAddress + `","energy":64285,"duration_sec":3600,"price_sun":1542840,"price_trx":"1.542840","price_usd":"0.46","credits":4600000,"trx_usd":"0.29750000","delivered_energy":64285,"settled":true,"needs_attention":false,"created_at":"2026-09-18T12:00:00Z","delivered_at":"2026-09-18T12:00:03Z"}`

// TestEnergy_RentWireShape asserts Energy.Rent posts to /v1/energy/rent with
// the Idempotency-Key header from the context, signed, and maps a delivered
// order.
func TestEnergy_RentWireShape(t *testing.T) {
	const apiKey = "k"
	var sent sentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = (&recorder{}).capture(r)
		_, _ = io.WriteString(w, energyDeliveredOrder)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", apiKey, WithBaseURL(srv.URL), WithRetries(0))
	ctx := WithIdempotencyKey(context.Background(), energyTestKey)
	out, err := c.Energy.Rent(ctx, &EnergyRentRequest{
		ReceiveAddress: energyTestAddress,
		QuoteRef:       "q_01JZZ3",
	})
	if err != nil {
		t.Fatalf("Rent: %v", err)
	}
	if sent.URLPath != "/v1/energy/rent" {
		t.Errorf("path: %q", sent.URLPath)
	}
	want := `{"quote_ref":"q_01JZZ3","receive_address":"` + energyTestAddress + `"}`
	if !reflect.DeepEqual(jsonValue(t, sent.Body), jsonValue(t, []byte(want))) {
		t.Errorf("body = %s, want %s", sent.Body, want)
	}
	if sent.Idempotency != energyTestKey {
		t.Errorf("Idempotency-Key = %q, want %q", sent.Idempotency, energyTestKey)
	}
	// The header is part of the string to sign: verifyHMACv1 recomputing the
	// signature with the key proves it.
	if err := verifyHMACv1(sent, apiKey, "/v1/energy/rent"); err != nil {
		t.Error(err)
	}

	if out.ID != 481516 || out.IdempotencyKey != energyTestKey {
		t.Errorf("order identity: %d / %q", out.ID, out.IdempotencyKey)
	}
	if out.Status != EnergyOrderStatusDelivered {
		t.Errorf("Status: %q", out.Status)
	}
	if out.PriceSUN != 1542840 || out.PriceTRX != "1.542840" {
		t.Errorf("price: %d SUN / %q TRX", out.PriceSUN, out.PriceTRX)
	}
	if out.PriceUSD != "0.46" || out.Credits != 4600000 || out.TRXUSD != "0.29750000" {
		t.Errorf("charge: %q USD / %d credits @ %q", out.PriceUSD, out.Credits, out.TRXUSD)
	}
	if out.DeliveredEnergy != 64285 {
		t.Errorf("DeliveredEnergy: %d", out.DeliveredEnergy)
	}
	if !out.Settled || out.NeedsAttention {
		t.Errorf("delivered order: settled=%v needs_attention=%v", out.Settled, out.NeedsAttention)
	}
	if out.Error != "" {
		t.Errorf("Error: %q", out.Error)
	}
	if out.CreatedAt != "2026-09-18T12:00:00Z" || out.DeliveredAt != "2026-09-18T12:00:03Z" {
		t.Errorf("timestamps: %q / %q", out.CreatedAt, out.DeliveredAt)
	}
}

// TestEnergy_RentRefusedReturnsTheOrder: a refused order answers 502 with the
// order itself as the body — a business outcome, not a transport failure — so
// Rent returns it as a regular order. What was never charged stays OFF the
// wire — no price_usd, no credits, no trx_usd — rather than reading as "this
// was free".
func TestEnergy_RentRefusedReturnsTheOrder(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body = `{"id":481517,"idempotency_key":"` + energyTestKey + `","status":"refused","receive_address":"` + energyTestAddress + `","energy":64285,"duration_sec":3600,"price_sun":1542840,"price_trx":"1.542840","settled":true,"needs_attention":false,"error_code":"SUPPLIER_REFUSED","error":"no supplier could fill this order; nothing was bought and nothing was charged","created_at":"2026-09-18T12:00:00Z"}`
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	ctx := WithIdempotencyKey(context.Background(), energyTestKey)
	order, err := c.Energy.Rent(ctx, &EnergyRentRequest{ReceiveAddress: energyTestAddress})
	if err != nil {
		t.Fatalf("a refused order is returned, not thrown: %v", err)
	}
	if order == nil {
		t.Fatal("no order on a refused answer")
	}
	if order.Status != EnergyOrderStatusRefused {
		t.Errorf("Status: %q", order.Status)
	}
	if !order.Settled || order.NeedsAttention {
		t.Errorf("refused order: settled=%v needs_attention=%v", order.Settled, order.NeedsAttention)
	}
	if order.ErrorCode != "SUPPLIER_REFUSED" {
		t.Errorf("ErrorCode: %q", order.ErrorCode)
	}
	if order.Error != "no supplier could fill this order; nothing was bought and nothing was charged" {
		t.Errorf("Error: %q", order.Error)
	}
	if order.Credits != 0 || order.PriceUSD != "" || order.TRXUSD != "" {
		t.Errorf("a refused order reports no charge: credits=%d price_usd=%q trx_usd=%q",
			order.Credits, order.PriceUSD, order.TRXUSD)
	}
	if order.DeliveredEnergy != 0 || order.DeliveredAt != "" {
		t.Errorf("a refused order reports no delivery: %+v", order)
	}
	for _, field := range []string{"credits", "price_usd", "trx_usd", "delivered_at"} {
		if strings.Contains(body, field) {
			t.Errorf("%q must stay off the wire when nothing was charged: %s", field, body)
		}
	}
}

// TestEnergy_RentUnresolvedIsNotRetried: a 409 with the unresolved order as
// the body is a terminal state — the client must not burn its retry budget on
// it, and Rent returns the order with NeedsAttention set.
func TestEnergy_RentUnresolvedIsNotRetried(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"id":481518,"idempotency_key":"`+energyTestKey+`","status":"unresolved","receive_address":"`+energyTestAddress+`","energy":64285,"duration_sec":3600,"price_sun":1542840,"price_trx":"1.542840","price_usd":"0.46","credits":4600000,"trx_usd":"0.29750000","settled":false,"needs_attention":true,"error_code":"SUPPLIER_UNKNOWN","error":"the supplier's answer never arrived","created_at":"2026-09-18T12:00:00Z"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(3))
	ctx := WithIdempotencyKey(context.Background(), energyTestKey)
	order, err := c.Energy.Rent(ctx, &EnergyRentRequest{ReceiveAddress: energyTestAddress})
	if err != nil {
		t.Fatalf("an unresolved order is returned, not thrown: %v", err)
	}
	if n := len(rec.all()); n != 1 {
		t.Errorf("requests: %d, want 1 — a 409 must not be retried", n)
	}
	if order.Status != EnergyOrderStatusUnresolved || !order.NeedsAttention || order.Settled {
		t.Errorf("unresolved order: %+v", order)
	}
	if order.ErrorCode != "SUPPLIER_UNKNOWN" {
		t.Errorf("ErrorCode: %q", order.ErrorCode)
	}
}

// TestEnergy_RentRequiresIdempotencyKey: without a key on the context Rent
// refuses locally — no request is sent, because a keyless retry after a
// timeout would buy the energy a second time.
func TestEnergy_RentRequiresIdempotencyKey(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		_, _ = io.WriteString(w, energyDeliveredOrder)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Energy.Rent(context.Background(), &EnergyRentRequest{ReceiveAddress: energyTestAddress})
	if err == nil {
		t.Fatalf("Rent without an Idempotency-Key: order %+v, want an error", out)
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

// TestEnergy_Rent502Retried: a 502 means nothing was bought, so the idempotent
// retry — same key — is exactly what the client's default retry does.
func TestEnergy_Rent502Retried(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		if len(rec.all()) < 2 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"ok":false,"error":"SERVICE_ERROR","msg":"supplier timeout"}`)
			return
		}
		_, _ = io.WriteString(w, energyDeliveredOrder)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", "k", WithBaseURL(srv.URL),
		WithRetries(3), WithRetryBackoff(time.Millisecond, time.Millisecond))
	ctx := WithIdempotencyKey(context.Background(), energyTestKey)
	out, err := c.Energy.Rent(ctx, &EnergyRentRequest{ReceiveAddress: energyTestAddress})
	if err != nil {
		t.Fatalf("Rent: %v", err)
	}
	if out.Status != EnergyOrderStatusDelivered {
		t.Errorf("Status: %q", out.Status)
	}
	reqs := rec.all()
	if len(reqs) != 2 {
		t.Fatalf("requests: %d, want 2", len(reqs))
	}
	for i, s := range reqs {
		if s.Idempotency != energyTestKey {
			t.Errorf("attempt %d: Idempotency-Key = %q", i, s.Idempotency)
		}
		if err := verifyHMACv1(s, "k", "/v1/energy/rent"); err != nil {
			t.Errorf("attempt %d: %v", i, err)
		}
	}
}

// TestEnergy_OrderWireShape asserts Energy.Order looks an order up by its
// idempotency key, POSTing {"key":...} to /v1/energy/order.
func TestEnergy_OrderWireShape(t *testing.T) {
	const apiKey = "k"
	var sent sentRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = (&recorder{}).capture(r)
		_, _ = io.WriteString(w, energyDeliveredOrder)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", apiKey, WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Energy.Order(context.Background(), energyTestKey)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if sent.URLPath != "/v1/energy/order" {
		t.Errorf("path: %q", sent.URLPath)
	}
	want := `{"key":"` + energyTestKey + `"}`
	if !reflect.DeepEqual(jsonValue(t, sent.Body), jsonValue(t, []byte(want))) {
		t.Errorf("body = %s, want %s", sent.Body, want)
	}
	if err := verifyHMACv1(sent, apiKey, "/v1/energy/order"); err != nil {
		t.Error(err)
	}
	if out.IdempotencyKey != energyTestKey || out.Status != EnergyOrderStatusDelivered {
		t.Errorf("order: %q / %q", out.IdempotencyKey, out.Status)
	}
}
