package cryptochief

import "context"

// PayoutsService groups payout-related endpoints. Access via Client.Payouts.
type PayoutsService struct{ c *Client }

// Payout status values surface in PayoutInfo.Status. Terminal: paid (ok),
// failed/system_fail/expired/cancel (fail).
const (
	PayoutStatusQueue      = "queue"
	PayoutStatusProcess    = "process"
	PayoutStatusPaid       = "paid"
	PayoutStatusFailed     = "failed"
	PayoutStatusSystemFail = "system_fail"
	PayoutStatusExpired    = "expired"
	PayoutStatusCancel     = "cancel"
)

// EstimatePayoutRequest is the body of POST /v1/payout/estimate. Used to
// preview fees and the chosen source(s) before locking funds.
type EstimatePayoutRequest struct {
	// Network is the destination chain (e.g. ETH_SEPOLIA). Required.
	Network Chain `json:"network"`
	// Coin is the destination coin symbol (e.g. ETH, USDT). Required.
	Coin string `json:"coin"`
	// Amount is the human-readable amount to deliver to the recipient
	// (e.g. "0.5"). Required.
	Amount string `json:"amount"`
	// ToAddress is the recipient. Required.
	ToAddress string `json:"to_address"`
	// FromAddresses constrains the source wallets the API may draw from.
	// Empty = let the API pick.
	FromAddresses []string `json:"from_addresses,omitempty"`
	// AllowMultipleSources lets the API combine multiple wallets to
	// reach the target amount.
	AllowMultipleSources bool `json:"allow_multiple_sources,omitempty"`
	// AutoConvert turns the payout into a swap: source asset gets converted
	// on the fly. The estimate's ServiceOperations show the convert leg.
	AutoConvert bool `json:"auto_convert,omitempty"`
	// AutoConvertPolicy restricts which source assets the auto-convert may
	// draw from (allow / exclude lists). Nil = let the API choose freely.
	AutoConvertPolicy *AssetsPolicy `json:"auto_convert_policy,omitempty"`
	// MaxFeeAmountFiat caps the acceptable network fee (in USD-equivalent).
	MaxFeeAmountFiat string `json:"max_fee_amount_fiat,omitempty"`
	// Memo is appended to chains that support memos (XRP, TON, ...).
	Memo string `json:"memo,omitempty"`
}

// ExecutePayoutRequest is the body of POST /v1/payout/execute.
//
// OrderID is the idempotency key: re-submitting with the same OrderID
// returns the same uuid, so retries are safe.
type ExecutePayoutRequest struct {
	OrderID              string        `json:"order_id"`
	UserID               string        `json:"user_id"`
	Network              Chain         `json:"network"`
	Coin                 string        `json:"coin"`
	Amount               string        `json:"amount"`
	ToAddress            string        `json:"to_address"`
	URLCallback          string        `json:"url_callback"`
	FromAddresses        []string      `json:"from_addresses,omitempty"`
	AllowMultipleSources bool          `json:"allow_multiple_sources,omitempty"`
	AutoConvert          bool          `json:"auto_convert,omitempty"`
	AutoConvertPolicy    *AssetsPolicy `json:"auto_convert_policy,omitempty"`
	MaxFeeAmountFiat     string        `json:"max_fee_amount_fiat,omitempty"`
	Memo                 string        `json:"memo,omitempty"`
}

// EstimatePayoutResponse contains the fee preview and selected source(s).
type EstimatePayoutResponse struct {
	Network            Chain            `json:"network"`
	Coin               string           `json:"coin"`
	Amount             string           `json:"amount"`
	AmountToReceive    string           `json:"amount_to_receive"`
	ToAddress          string           `json:"to_address"`
	FeeInfo            *PayoutFeeInfo   `json:"fee_info,omitempty"`
	Sources            []PayoutSource   `json:"sources,omitempty"`
	ServiceOperations  []map[string]any `json:"service_operations,omitempty"`
	AutoConvertApplied bool             `json:"auto_convert_applied,omitempty"`
}

// PayoutFeeInfo carries the fee numbers shown by /payout/estimate.
type PayoutFeeInfo struct {
	FeeMode        string `json:"fee_mode"`
	EstimatedFiat  string `json:"estimated_fiat"`
	EstimatedCoin  string `json:"estimated_coin"`
	EstimatedAsset string `json:"estimated_asset,omitempty"`
}

// PayoutSource is one wallet the API plans to draw funds from.
type PayoutSource struct {
	Address string `json:"address"`
	Amount  string `json:"amount"`
	Coin    string `json:"coin,omitempty"`
}

// PayoutInfo is the persistent record of a single payout.
type PayoutInfo struct {
	UUID        string         `json:"uuid"`
	OrderID     string         `json:"order_id"`
	Status      string         `json:"status"`
	Network     Chain          `json:"network"`
	Coin        string         `json:"coin"`
	Amount      string         `json:"amount"`
	ToAddress   string         `json:"to_address"`
	TxID        string         `json:"txid,omitempty"`
	Sources     []PayoutSource `json:"sources,omitempty"`
	URLCallback string         `json:"url_callback,omitempty"`
	CreatedAt   string         `json:"created_at,omitempty"`
	UpdatedAt   string         `json:"updated_at,omitempty"`
	Error       string         `json:"error,omitempty"`
}

// IsTerminal reports whether the payout reached a final state (no further
// transitions). Use this in polling loops.
func (p PayoutInfo) IsTerminal() bool {
	switch p.Status {
	case PayoutStatusPaid, PayoutStatusFailed, PayoutStatusSystemFail, PayoutStatusExpired, PayoutStatusCancel:
		return true
	}
	return false
}

// Succeeded reports whether the payout settled successfully.
func (p PayoutInfo) Succeeded() bool { return p.Status == PayoutStatusPaid }

// BatchExecuteRequest is the body of POST /v1/payout/batch/execute (and
// /batch/estimate — the same shape). Up to 50 items per call (default cap;
// per-merchant overrides apply).
type BatchExecuteRequest struct {
	URLCallback string                 `json:"url_callback,omitempty"`
	Items       []ExecutePayoutRequest `json:"items"`
}

// BatchItemResult is the per-item outcome of a batch call.
type BatchItemResult struct {
	Index   int    `json:"index"`
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
	UUID    string `json:"uuid,omitempty"`
	Error   string `json:"error,omitempty"`
}

// BatchExecuteResponse is the response of /payout/batch/{estimate,execute}.
type BatchExecuteResponse struct {
	BatchUUID string            `json:"batch_uuid,omitempty"`
	Total     int               `json:"total"`
	Accepted  int               `json:"accepted"`
	Rejected  int               `json:"rejected"`
	Items     []BatchItemResult `json:"items"`
}

// HistoryQuery is shared by every history endpoint with simple pagination
// (PayIns, Payouts, Withdrawals, StaticDeposits, Sweeps). Empty fields are
// omitted from the request.
type HistoryQuery struct {
	Page     int    `json:"page,omitempty"`
	PageSize int    `json:"page_size,omitempty"`
	Status   string `json:"status,omitempty"`
	Coin     string `json:"coin,omitempty"`
	Network  Chain  `json:"network,omitempty"`
	DateFrom string `json:"date_from,omitempty"`
	DateTo   string `json:"date_to,omitempty"`
}

// HistoryMeta is the pagination envelope returned by every history endpoint.
type HistoryMeta struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages,omitempty"`
}

// PayoutHistoryResponse is the page of payouts.
type PayoutHistoryResponse struct {
	Items []PayoutInfo `json:"items"`
	Meta  HistoryMeta  `json:"meta"`
}

// Estimate previews fees and the selected source(s) for a payout without
// locking funds. Use it to surface "amount to receive" before showing a
// merchant the execute button.
func (s *PayoutsService) Estimate(ctx context.Context, in *EstimatePayoutRequest) (*EstimatePayoutResponse, error) {
	var out EstimatePayoutResponse
	if err := s.c.do(ctx, "/v1/payout/estimate", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Execute creates and dispatches a payout. Funds are locked immediately;
// re-sending the same order_id returns the same uuid (idempotent).
func (s *PayoutsService) Execute(ctx context.Context, in *ExecutePayoutRequest) (*PayoutInfo, error) {
	var out PayoutInfo
	if err := s.c.do(ctx, "/v1/payout/execute", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Info fetches the current state of one payout by uuid.
func (s *PayoutsService) Info(ctx context.Context, uuid string) (*PayoutInfo, error) {
	var out PayoutInfo
	if err := s.c.do(ctx, "/v1/payout/info", map[string]string{"uuid": uuid}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// History returns a paged list of payouts matching the filter.
func (s *PayoutsService) History(ctx context.Context, q HistoryQuery) (*PayoutHistoryResponse, error) {
	var out PayoutHistoryResponse
	if err := s.c.do(ctx, "/v1/payout/history", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BatchEstimate previews fees and selected sources for up to 50 payouts in
// one call. Per-item errors come back inside items[]; only structural errors
// (BATCH_EMPTY / BATCH_TOO_LARGE / BATCH_DUPLICATE_ORDER_ID) cause a 400.
func (s *PayoutsService) BatchEstimate(ctx context.Context, in *BatchExecuteRequest) (*BatchExecuteResponse, error) {
	var out BatchExecuteResponse
	if err := s.c.do(ctx, "/v1/payout/batch/estimate", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BatchExecute creates up to 50 payouts atomically per call. The batch is
// best-effort: a bad item returns its code in items[].error and does not
// block the others. Funds are locked sequentially so an intra-batch
// double-spend can never occur — do not parallelise.
func (s *PayoutsService) BatchExecute(ctx context.Context, in *BatchExecuteRequest) (*BatchExecuteResponse, error) {
	var out BatchExecuteResponse
	if err := s.c.do(ctx, "/v1/payout/batch/execute", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
