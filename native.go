package cryptochief

import (
	"context"
	"encoding/json"
	"errors"
)

// NativeService groups the native-coin purchase endpoints: the platform sells
// the native coin of a network (TRX, ETH, BNB, SOL, TON, ...) out of its own
// liquidity, delivered to any address, and charges the same credits balance as
// every other paid call (see Client.Credits). The price is the coins at the
// coin's rate plus the platform's transfer fee at the same rate — the
// transfer the platform sends is part of what you pay for, so the receiver
// gets exactly Amount.
// Access via Client.Native.
type NativeService struct{ c *Client }

// Native order status values surface in NativeOrder.Status. An order is final
// from the moment Buy answers: delivered (the coins are sent), refused
// (nothing was bought or charged) or unresolved (the outcome never arrived —
// see NativeOrder.NeedsAttention).
const (
	// NativeOrderStatusDelivered: the coins are sent to the receiver; TxHash
	// is the transfer. Final.
	NativeOrderStatusDelivered = "delivered"
	// NativeOrderStatusRefused: the purchase did not happen and nothing was
	// charged; NativeOrder.Error says why. Final.
	NativeOrderStatusRefused = "refused"
	// NativeOrderStatusUnresolved: the order may or may not have been
	// delivered. Final, and MUST NOT be retried — resolve it with Order or
	// support first.
	NativeOrderStatusUnresolved = "unresolved"
)

// NativeQuoteRequest is the body of POST /v1/native/quote.
type NativeQuoteRequest struct {
	// Network is the network whose native coin is priced (e.g.
	// ChainTronMainnet). Required.
	Network Chain `json:"network"`
	// ReceiveAddress is the address the coins would be delivered to. Any
	// address qualifies — the platform pays for the transfer. Required.
	ReceiveAddress string `json:"receive_address"`
	// Amount is how much native coin to price, in human units as a decimal
	// string ("0.05" TRX, ETH, ...). Required.
	Amount string `json:"amount"`
}

// NativeQuote is what /v1/native/quote returns: a price the service stands
// behind until ExpiresAt. The endpoint is free of charge.
//
// Credits is what the order would be charged, in the same unit as
// CreditsBalance.CreditsBalance, so it can be compared against the balance
// directly.
type NativeQuote struct {
	// Ref is the quote handle to pass as NativeBuyRequest.QuoteRef.
	Ref            string `json:"ref"`
	Network        Chain  `json:"network"`
	ReceiveAddress string `json:"receive_address"`
	Amount         string `json:"amount"`

	// CoinPriceUSD is Amount converted at CoinUSD — the cost of the coins
	// themselves.
	CoinPriceUSD string `json:"coin_price_usd"`
	// TransferFee is the fee of the transfer the platform sends, in the
	// native coin; TransferFeeUSD is the same at CoinUSD.
	TransferFee    string `json:"transfer_fee"`
	TransferFeeUSD string `json:"transfer_fee_usd"`
	// SubtotalUSD is CoinPriceUSD + TransferFeeUSD; TotalUSD is the full
	// price of the purchase, in USD.
	SubtotalUSD string `json:"subtotal_usd"`
	TotalUSD    string `json:"total_usd"`
	// Credits is the charge in billing credits at TotalUSD.
	Credits int64 `json:"credits"`
	// CoinUSD is the rate the USD figures are computed at.
	CoinUSD string `json:"coin_usd"`

	// ExpiresAt is when the quote stops being usable, RFC 3339;
	// ExpiresInSec is the same as a countdown (about 90 seconds — a quote is
	// single-use).
	ExpiresAt    string `json:"expires_at"`
	ExpiresInSec int64  `json:"expires_in_sec"`
}

// NativeBuyRequest is the body of POST /v1/native/buy.
//
// The call REQUIRES an idempotency key, sent as the Idempotency-Key header —
// set it on the context with [WithIdempotencyKey]:
//
//	ctx := cryptochief.WithIdempotencyKey(ctx, "native-2026-09-18-0001")
//	order, err := c.Native.Buy(ctx, &cryptochief.NativeBuyRequest{...})
//
// Without one the method refuses locally: the key is what makes a retry after
// a timeout return the same order instead of buying the coins a second time.
type NativeBuyRequest struct {
	// Network is the network whose native coin to buy. Required unless
	// QuoteRef is given, in which case it must match the quote's.
	Network Chain `json:"network,omitempty"`
	// ReceiveAddress is the address the coins are delivered to — any address
	// qualifies, the platform pays for the transfer. Required unless QuoteRef
	// is given, in which case it must match the quote's.
	ReceiveAddress string `json:"receive_address,omitempty"`
	// Amount is how much native coin to buy, in human units as a decimal
	// string. Required unless QuoteRef is given.
	Amount string `json:"amount,omitempty"`
	// QuoteRef buys at a price already quoted (NativeQuote.Ref). Omitted, the
	// order is priced and bought in the same call.
	QuoteRef string `json:"quote_ref,omitempty"`
}

// NativeOrder is one native-coin purchase, returned by Buy and Order.
//
// TransferFee, TransferFeeUSD, CoinPriceUSD and CoinUSD are always on the
// wire — on a refused order they arrive as "" / "0.00" rather than absent.
// What a refused order OMITS is the delivery and the charge: TxHash,
// TotalUSD, Credits and DeliveredAt stay off the wire, because a zero there
// would read as "this was free".
type NativeOrder struct {
	ID             int64  `json:"id"`
	IdempotencyKey string `json:"idempotency_key"`
	// Status is one of the NativeOrderStatus* constants.
	Status string `json:"status"`

	Network        Chain  `json:"network"`
	ReceiveAddress string `json:"receive_address"`
	Amount         string `json:"amount"`

	// TxHash is the hash of the transfer that delivered the coins. Absent
	// until delivered.
	TxHash string `json:"tx_hash,omitempty"`
	// TransferFee is the fee of the platform's transfer, in the native coin;
	// TransferFeeUSD is the same at CoinUSD. Always on the wire; "" / "0.00"
	// on a refused order.
	TransferFee    string `json:"transfer_fee"`
	TransferFeeUSD string `json:"transfer_fee_usd"`
	// CoinPriceUSD is what the coins cost at CoinUSD. Always on the wire;
	// "0.00" on a refused order.
	CoinPriceUSD string `json:"coin_price_usd"`
	// TotalUSD is what was charged, in USD (the coins plus the platform's
	// transfer fee, at CoinUSD). Absent when nothing was charged.
	TotalUSD string `json:"total_usd,omitempty"`
	// Credits is what was charged, in billing credits. Absent when nothing
	// was charged.
	Credits int64 `json:"credits,omitempty"`
	// CoinUSD is the rate the charge was computed at. Always on the wire;
	// "" on a refused order.
	CoinUSD string `json:"coin_usd"`

	// Settled says the outcome is final either way; NeedsAttention says it is
	// not and that no retry will help — the order may already have been
	// delivered. Both are given as plain booleans rather than left to infer
	// from Status, because getting that inference wrong is exactly how an
	// order gets placed twice.
	Settled        bool `json:"settled"`
	NeedsAttention bool `json:"needs_attention"`

	// Error is why a refused order was refused — a sanitised human sentence.
	// ErrorCode is the machine form of it, for integrations that branch on a
	// code rather than a sentence; "" when the order is not a refusal.
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`

	// CreatedAt is when the order was placed, RFC 3339. DeliveredAt is when
	// the coins were sent; "" until delivered.
	CreatedAt   string `json:"created_at"`
	DeliveredAt string `json:"delivered_at,omitempty"`
}

// Quote prices a native-coin purchase without buying anything and holds the
// price until the quote expires (about 90 seconds; a quote is single-use).
// Free of charge; rate-limited per project. Pass the returned Ref as
// NativeBuyRequest.QuoteRef to buy at exactly this price — an expired or
// already-used quote is refused with QUOTE_EXPIRED / QUOTE_ALREADY_USED (409)
// and must be re-quoted.
func (s *NativeService) Quote(ctx context.Context, in *NativeQuoteRequest) (*NativeQuote, error) {
	var out NativeQuote
	if err := s.c.do(ctx, "/v1/native/quote", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Buy purchases native coins and delivers them to the receiver, charging the
// project's credits balance. The call is SYNCHRONOUS: by the time it answers,
// the coins are sent or the refusal reason is known — there is nothing to
// poll.
//
// An Idempotency-Key is REQUIRED — set it with [WithIdempotencyKey] on the
// context; without one the method refuses locally. The key deduplicates
// retries: re-sending with the same key returns the same order, so the
// client's automatic retry of a 502 (a delivery failure where nothing was
// charged) is safe. A 409 NEEDS_ATTENTION, by contrast, must NOT be retried:
// the order is unresolved and may already have been delivered — resolve it
// with [NativeService.Order] or support.
//
// A refused or unresolved order is a business outcome, not a transport
// failure: the API answers 502 (or 402, when the credits balance did not
// cover the order) / 409 with the order itself as the body, and Buy returns
// it as a regular (*NativeOrder, nil) — branch on Status, Error and
// ErrorCode:
//
//	order, err := c.Native.Buy(ctx, req)
//	if err != nil {
//	    return err // a real failure — no order exists
//	}
//	switch order.Status {
//	case cryptochief.NativeOrderStatusRefused:
//	    // nothing was bought or charged; order.Error / order.ErrorCode say why
//	case cryptochief.NativeOrderStatusUnresolved:
//	    // order.NeedsAttention — may already be delivered; DO NOT retry,
//	    // resolve with c.Native.Order(ctx, key) or support
//	}
//
// Errors with no order to report (QUOTE_EXPIRED / QUOTE_ALREADY_USED (409 —
// re-quote), QUOTE_NOT_FOUND (404), a gateway refusal, ...) surface as a
// regular *APIError.
func (s *NativeService) Buy(ctx context.Context, in *NativeBuyRequest) (*NativeOrder, error) {
	if IdempotencyKeyFromContext(ctx) == "" {
		return nil, errors.New("cryptochief: native buy: an Idempotency-Key is required — set it with WithIdempotencyKey; without it a retry after a timeout would buy the coins twice")
	}
	var out NativeOrder
	if err := s.c.do(ctx, "/v1/native/buy", in, &out); err != nil {
		if raw, ok := orderViewFromError(err); ok && json.Unmarshal(raw, &out) == nil {
			return &out, nil
		}
		return nil, err
	}
	return &out, nil
}

// Order fetches the current state of one native-coin order by the idempotency
// key it was placed with. Another project's key answers 404 NOT_FOUND, like a
// key that never existed.
func (s *NativeService) Order(ctx context.Context, key string) (*NativeOrder, error) {
	var out NativeOrder
	if err := s.c.do(ctx, "/v1/native/order", map[string]string{"key": key}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
