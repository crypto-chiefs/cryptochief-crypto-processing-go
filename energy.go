package cryptochief

import (
	"context"
	"encoding/json"
	"errors"
)

// EnergyService groups the TRON energy-rental endpoints: renting energy for a
// transfer is cheaper than burning TRX for it. Charges go to the same credits
// balance as every other paid call (see Client.Credits). Access via
// Client.Energy.
type EnergyService struct{ c *Client }

// Energy order status values surface in EnergyOrder.Status. An order is
// final from the moment Rent answers: delivered (the energy is delegated),
// refused (nothing was bought or charged) or unresolved (the supplier's
// answer never arrived — see EnergyOrder.NeedsAttention).
const (
	// EnergyOrderStatusDelivered: the energy is delegated to the receiver.
	// Final.
	EnergyOrderStatusDelivered = "delivered"
	// EnergyOrderStatusRefused: the purchase did not happen and nothing was
	// charged; EnergyOrder.Error says why. Final.
	EnergyOrderStatusRefused = "refused"
	// EnergyOrderStatusUnresolved: the order may or may not have been bought
	// supplier-side. Final, and MUST NOT be retried — resolve it with Order
	// or support first.
	EnergyOrderStatusUnresolved = "unresolved"
)

// EnergyQuoteRequest is the body of POST /v1/energy/quote.
type EnergyQuoteRequest struct {
	// ReceiveAddress is the address the energy would be delegated to — the
	// sender of the transfer you are pricing. Required.
	ReceiveAddress string `json:"receive_address"`
	// Energy is the amount of energy to price. Optional: omitted, the service
	// reads the receiver and sizes the order for a USDT transfer to it (an
	// address already holding the token needs about half the energy of one
	// that does not — see EnergyQuote.RecipientState).
	Energy int64 `json:"energy,omitempty"`
	// DurationSec is the rental duration in seconds. Optional; the service
	// default is one hour.
	DurationSec int64 `json:"duration_sec,omitempty"`
}

// EnergyQuote is what /v1/energy/quote returns: a price the service stands
// behind until ExpiresAt, plus what burning TRX directly would cost for
// comparison. The endpoint is free of charge.
//
// TRX amounts come twice: an integer in SUN (PriceSUN — authoritative, safe
// to compare) and a decimal string in TRX (PriceTRX — for humans). Credits is
// what the order would be charged, in the same unit as
// CreditsBalance.CreditsBalance, so it can be compared against the balance
// directly.
type EnergyQuote struct {
	// Ref is the quote handle to pass as EnergyRentRequest.QuoteRef.
	Ref            string `json:"ref"`
	ReceiveAddress string `json:"receive_address"`
	Energy         int64  `json:"energy"`
	DurationSec    int64  `json:"duration_sec"`

	PriceSUN int64  `json:"price_sun"`
	PriceTRX string `json:"price_trx"`
	// PriceUSD is PriceTRX converted at TRXUSD; "" when no rate is available.
	PriceUSD string `json:"price_usd,omitempty"`
	// Credits is the charge in billing credits at TRXUSD. 0 when no rate is
	// available.
	Credits int64 `json:"credits,omitempty"`
	// TRXUSD is the rate the USD and credits figures are computed at; "" when
	// unavailable.
	TRXUSD string `json:"trx_usd,omitempty"`

	// RecipientState is why the energy figure is what it is: "warm" when the
	// receiver already holds the token (the transfer updates a storage slot),
	// "cold" when it does not (a slot is created — about twice the energy),
	// "unknown" when the service could not look.
	RecipientState string `json:"recipient_state"`

	// Burn* is what the same transfer would cost paying the chain directly;
	// Saving* is the difference. Published so the saving is checkable rather
	// than claimed. USD and credits figures are "" / 0 when no rate is
	// available.
	BurnPriceSUN     int64  `json:"burn_price_sun"`
	BurnPriceTRX     string `json:"burn_price_trx"`
	BurnPriceUSD     string `json:"burn_price_usd,omitempty"`
	BurnPriceCredits int64  `json:"burn_price_credits,omitempty"`
	SavingTRX        string `json:"saving_trx"`
	SavingUSD        string `json:"saving_usd,omitempty"`
	SavingCredits    int64  `json:"saving_credits,omitempty"`

	// ExpiresAt is when the quote stops being usable, RFC 3339;
	// ExpiresInSec is the same as a countdown.
	ExpiresAt    string `json:"expires_at"`
	ExpiresInSec int64  `json:"expires_in_sec"`
}

// EnergyRentRequest is the body of POST /v1/energy/rent.
//
// The call REQUIRES an idempotency key, sent as the Idempotency-Key header —
// set it on the context with [WithIdempotencyKey]:
//
//	ctx := cryptochief.WithIdempotencyKey(ctx, "energy-2026-09-18-0001")
//	order, err := c.Energy.Rent(ctx, &cryptochief.EnergyRentRequest{...})
//
// Without one Rent refuses locally: the key is what makes a retry after a
// timeout return the same order instead of buying the energy a second time.
type EnergyRentRequest struct {
	// ReceiveAddress is the address the energy is delegated to — the sender
	// of the transfer you are fueling. Required unless QuoteRef is given, in
	// which case it must match the quote's.
	ReceiveAddress string `json:"receive_address,omitempty"`
	// Energy is the amount of energy to rent. Optional: omitted, the service
	// reads the receiver and sizes the order for a USDT transfer to it.
	Energy int64 `json:"energy,omitempty"`
	// DurationSec is the rental duration in seconds. Optional; the service
	// default is one hour.
	DurationSec int64 `json:"duration_sec,omitempty"`
	// QuoteRef buys at a price already quoted (EnergyQuote.Ref). Omitted,
	// the order is priced and bought in the same call.
	QuoteRef string `json:"quote_ref,omitempty"`
}

// EnergyOrder is one energy purchase, returned by Rent and Order.
//
// PriceUSD, Credits and TRXUSD record what the caller was ACTUALLY charged,
// so they are absent ("" / 0) on an order nobody was charged for — a refused
// one — rather than zero, which would read as "this was free".
type EnergyOrder struct {
	ID             int64  `json:"id"`
	IdempotencyKey string `json:"idempotency_key"`
	// Status is one of the EnergyOrderStatus* constants.
	Status string `json:"status"`

	ReceiveAddress string `json:"receive_address"`
	Energy         int64  `json:"energy"`
	DurationSec    int64  `json:"duration_sec"`

	PriceSUN int64  `json:"price_sun"`
	PriceTRX string `json:"price_trx"`
	// PriceUSD is what was charged, in USD. "" when nothing was charged.
	PriceUSD string `json:"price_usd,omitempty"`
	// Credits is what was charged, in billing credits. 0 when nothing was
	// charged.
	Credits int64 `json:"credits,omitempty"`
	// TRXUSD is the rate the charge was computed at. "" when nothing was
	// charged.
	TRXUSD string `json:"trx_usd,omitempty"`

	// DeliveredEnergy is the energy actually delegated. 0 until delivered.
	DeliveredEnergy int64 `json:"delivered_energy,omitempty"`

	// Settled says the outcome is final either way; NeedsAttention says it is
	// not and that no retry will help — the order may already have been
	// bought supplier-side. Both are given as plain booleans rather than left
	// to infer from Status, because getting that inference wrong is exactly
	// how an order gets placed twice.
	Settled        bool `json:"settled"`
	NeedsAttention bool `json:"needs_attention"`

	// Error is why a refused order was refused — a sanitised human sentence.
	// ErrorCode is the machine form of it, for integrations that branch on a
	// code rather than a sentence; "" when the order is not a refusal.
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`

	// CreatedAt is when the order was placed, RFC 3339. DeliveredAt is when
	// the energy was delegated; "" until delivered.
	CreatedAt   string `json:"created_at"`
	DeliveredAt string `json:"delivered_at,omitempty"`
}

// Quote prices an energy rental without buying anything and holds the price
// until the quote expires. Free of charge; rate-limited per project. Pass the
// returned Ref as EnergyRentRequest.QuoteRef to buy at exactly this price —
// an expired or already-used quote is refused with QUOTE_EXPIRED /
// QUOTE_ALREADY_USED.
func (s *EnergyService) Quote(ctx context.Context, in *EnergyQuoteRequest) (*EnergyQuote, error) {
	var out EnergyQuote
	if err := s.c.do(ctx, "/v1/energy/quote", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Rent buys energy and delegates it to the receiver, charging the project's
// credits balance. The call is SYNCHRONOUS: by the time it answers, the
// energy is delegated or the refusal reason is known — there is nothing to
// poll.
//
// An Idempotency-Key is REQUIRED — set it with [WithIdempotencyKey] on the
// context; without one the method refuses locally. The key deduplicates
// retries: re-sending with the same key returns the same order, so the
// client's automatic retry of a 502 (a supplier failure where nothing was
// bought) is safe. A 409 NEEDS_ATTENTION, by contrast, must NOT be retried:
// the order is unresolved and may already have been bought — resolve it with
// [EnergyService.Order] or support.
//
// A refused or unresolved order is a business outcome, not a transport
// failure: the API answers 502 (or 402, when the credits balance did not
// cover the order) / 409 with the order itself as the body, and Rent returns
// it as a regular (*EnergyOrder, nil) — branch on Status, Error and
// ErrorCode:
//
//	order, err := c.Energy.Rent(ctx, req)
//	if err != nil {
//	    return err // a real failure — no order exists
//	}
//	switch order.Status {
//	case cryptochief.EnergyOrderStatusRefused:
//	    // nothing was bought or charged; order.Error / order.ErrorCode say why
//	case cryptochief.EnergyOrderStatusUnresolved:
//	    // order.NeedsAttention — may already be bought; DO NOT retry,
//	    // resolve with c.Energy.Order(ctx, key) or support
//	}
//
// Errors with no order to report (a keyless call, a gateway refusal, ...)
// surface as a regular *APIError.
func (s *EnergyService) Rent(ctx context.Context, in *EnergyRentRequest) (*EnergyOrder, error) {
	if IdempotencyKeyFromContext(ctx) == "" {
		return nil, errors.New("cryptochief: energy rent: an Idempotency-Key is required — set it with WithIdempotencyKey; without it a retry after a timeout would buy the energy twice")
	}
	var out EnergyOrder
	if err := s.c.do(ctx, "/v1/energy/rent", in, &out); err != nil {
		if raw, ok := orderViewFromError(err); ok && json.Unmarshal(raw, &out) == nil {
			return &out, nil
		}
		return nil, err
	}
	return &out, nil
}

// Order fetches the current state of one energy order by the idempotency key
// it was placed with. Another project's key answers 404 NOT_FOUND, like a key
// that never existed.
func (s *EnergyService) Order(ctx context.Context, key string) (*EnergyOrder, error) {
	var out EnergyOrder
	if err := s.c.do(ctx, "/v1/energy/order", map[string]string{"key": key}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
