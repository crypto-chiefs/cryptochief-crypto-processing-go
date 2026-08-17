package cryptochief

import "context"

// CreditsService groups the billing-credits endpoints. Access via
// Client.Credits.
type CreditsService struct{ c *Client }

// CreditsBalance is the response of /v1/credits/balance. Credits are the
// API's internal billing unit: 10_000_000 credits = 1 USD.
type CreditsBalance struct {
	// CreditsBalance is the current balance in credits. Negative when a
	// postpaid project is in debt.
	CreditsBalance int64 `json:"credits_balance"`
	// USDBalance is the balance pre-formatted as USD with 2 decimals.
	// Can be negative, e.g. "-1.52".
	USDBalance string `json:"usd_balance"`
	// IsPostpaid reports whether the project is on postpaid billing.
	IsPostpaid bool `json:"is_postpaid"`
	// DebtLimitCredits is the effective debt limit in credits. Postpaid
	// only; 0 for prepaid projects.
	DebtLimitCredits int64 `json:"debt_limit_credits"`
	// CanExecuteGasOperations reports whether gas-paying operations
	// (/v1/transaction/execute and friends) would pass the billing gate
	// right now.
	CanExecuteGasOperations bool `json:"can_execute_gas_operations"`
	// GasOpsMinCredits is the minimum credits balance required for
	// gas-paying operations.
	GasOpsMinCredits int64 `json:"gas_ops_min_credits"`
	// Timestamp is the server time of the snapshot, RFC 3339.
	Timestamp string `json:"timestamp"`
}

// Balance returns the project's current credits balance and the state of
// the gas-operations billing gate. The endpoint is free of charge — it
// exists so integrations can check credits without spending a paid call —
// and answers even at zero or negative balance, so it is safe to poll
// from monitoring within its rate limit of 60 requests per minute per
// project.
func (s *CreditsService) Balance(ctx context.Context) (*CreditsBalance, error) {
	var out CreditsBalance
	if err := s.c.do(ctx, "/v1/credits/balance", struct{}{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreditsTopupRequest is the body of POST /v1/credits/topup.
type CreditsTopupRequest struct {
	// Amount is the top-up amount as a positive decimal string, at most
	// 100000 (USD-pegged). Required.
	Amount string `json:"amount"`
	// Currency is the stablecoin to pay in: "USDT" or "USDC". Required.
	Currency string `json:"currency"`
	// URLSuccess is an absolute http(s) URL the payer's browser is
	// redirected to after a successful payment. Optional.
	URLSuccess string `json:"url_success,omitempty"`
	// URLError is an absolute http(s) URL the payer's browser is
	// redirected to when the payment fails. Optional.
	URLError string `json:"url_error,omitempty"`
}

// CreditsTopup is the response of /v1/credits/topup: the created billing
// invoice plus the hosted payment page to complete it on.
type CreditsTopup struct {
	// InvoiceID is the billing invoice id.
	InvoiceID int64 `json:"invoice_id"`
	// PaymentLink is the hosted payment page URL (QR code, network
	// selection, live status).
	PaymentLink string `json:"payment_link"`
	// Amount echoes the requested top-up amount.
	Amount string `json:"amount"`
	// Currency echoes the requested stablecoin.
	Currency string `json:"currency"`
	// Status is "pending" on creation.
	Status string `json:"status"`
	// OrderUUID identifies the underlying payment order, when the server
	// reports one.
	OrderUUID string `json:"order_uuid,omitempty"`
	// ExpiredAt is when the payment link expires, unix seconds. 0 when
	// the server does not report an expiry.
	ExpiredAt int64 `json:"expired_at,omitempty"`
}

// Topup creates a billing invoice for the given amount and returns a hosted
// payment link (QR code, network selection, live status) to complete it.
// Like Balance, the endpoint is free of charge and rate-limited to 60
// requests per minute per project. Notable error codes:
// AMOUNT_OUT_OF_RANGE, UNSUPPORTED_CURRENCY, INVALID_URL (400),
// TOPUP_NOT_CONFIGURED (501), RATE_LIMITED (429).
func (s *CreditsService) Topup(ctx context.Context, req CreditsTopupRequest) (*CreditsTopup, error) {
	var out CreditsTopup
	if err := s.c.do(ctx, "/v1/credits/topup", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
