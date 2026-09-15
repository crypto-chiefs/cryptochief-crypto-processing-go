package cryptochief

import "context"

// WithdrawalsService groups the read-only withdrawal endpoints. The
// public API does not create withdrawals directly — only reads. Access
// via Client.Withdrawals.
type WithdrawalsService struct{ c *Client }

// Withdrawal status values surface in Withdrawal.Status. Terminal: completed
// (ok) and failed.
const (
	// WithdrawalStatusQueue: accepted, not started.
	WithdrawalStatusQueue = "queue"
	// WithdrawalStatusRefueling: gas top-up of the source wallet in progress.
	// Not used on BTC-family networks.
	WithdrawalStatusRefueling = "refueling"
	// WithdrawalStatusRefuelConfirmed: the source wallet has gas; the
	// withdrawal transaction is sent next.
	WithdrawalStatusRefuelConfirmed = "refuel_confirmed"
	// WithdrawalStatusSending: the withdrawal transaction is being sent.
	WithdrawalStatusSending = "sending"
	// WithdrawalStatusBroadcasting: waiting to be broadcast. EVM networks only.
	WithdrawalStatusBroadcasting = "broadcasting"
	// WithdrawalStatusInMempool: broadcast, waiting for a block. BTC-family
	// networks only.
	WithdrawalStatusInMempool = "in_mempool"
	// WithdrawalStatusConfirmCheck: sent to the network, below
	// RequiredConfirmations. Confirmations is nil until the transaction is in a
	// block.
	WithdrawalStatusConfirmCheck = "confirm_check"
	// WithdrawalStatusCompleted: reached RequiredConfirmations. Final.
	WithdrawalStatusCompleted = "completed"
	// WithdrawalStatusFailed: failed; see ErrorReason. Final.
	WithdrawalStatusFailed = "failed"
	// Deprecated: not produced by the API.
	WithdrawalStatusCancelled = "cancelled"
)

// Withdrawal is the persistent record of one outbound treasury movement.
type Withdrawal struct {
	UUID string `json:"uuid"`
	// Status is one of the WithdrawalStatus* values.
	Status      string `json:"status"`
	Network     Chain  `json:"network"`
	Coin        string `json:"coin,omitempty"`
	Amount      string `json:"amount"`
	FromAddress string `json:"from_address,omitempty"`
	ToAddress   string `json:"to_address,omitempty"`
	TxHash      string `json:"tx_hash,omitempty"`

	// NeedRefuel says whether a gas top-up was planned at creation. A top-up
	// can also happen later, so RefuelTxHash may be set while it is false.
	NeedRefuel   bool   `json:"need_refuel"`
	RefuelTxHash string `json:"refuel_tx_hash,omitempty"`
	RefuelStatus string `json:"refuel_status,omitempty"`

	// ErrorReason says why a withdrawal failed.
	ErrorReason string `json:"error_reason,omitempty"`

	// Confirmations of the withdrawal transaction. Optional: nil until the
	// transaction is in a block.
	Confirmations *int `json:"confirmations,omitempty"`

	// RequiredConfirmations is the count at which the withdrawal turns
	// completed. Always sent.
	RequiredConfirmations int `json:"required_confirmations,omitempty"`

	EstimatedFeeFiat string `json:"estimated_fee_fiat,omitempty"`
	ActualFeeFiat    string `json:"actual_fee_fiat,omitempty"`
	FeeMode          string `json:"fee_mode,omitempty"`

	CreatedAt string `json:"created_at,omitempty"`
	// CompletedAt is set on completed withdrawals only.
	CompletedAt string `json:"completed_at,omitempty"`

	// Deprecated: never populated.
	Contract string `json:"contract,omitempty"`

	// Deprecated: never populated.
	AmountFiat string `json:"amount_fiat,omitempty"`

	// Deprecated: never populated; use CompletedAt.
	UpdatedAt string `json:"updated_at,omitempty"`

	// Deprecated: never populated; use CompletedAt.
	ConfirmedAt string `json:"confirmed_at,omitempty"`

	// Deprecated: never populated; use ErrorReason.
	Error string `json:"error,omitempty"`
}

// IsTerminal reports whether the withdrawal is completed or failed.
func (w Withdrawal) IsTerminal() bool {
	switch w.Status {
	case WithdrawalStatusCompleted, WithdrawalStatusFailed, WithdrawalStatusCancelled:
		return true
	}
	return false
}

// Succeeded reports whether the withdrawal is completed.
func (w Withdrawal) Succeeded() bool { return w.Status == WithdrawalStatusCompleted }

// WithdrawalHistoryResponse is the page of withdrawals.
type WithdrawalHistoryResponse struct {
	Items []Withdrawal `json:"items"`
	Meta  HistoryMeta  `json:"meta"`
}

// Info fetches one withdrawal by uuid.
func (s *WithdrawalsService) Info(ctx context.Context, uuid string) (*Withdrawal, error) {
	var out Withdrawal
	if err := s.c.do(ctx, "/v1/withdrawal/info", map[string]string{"uuid": uuid}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// History returns a paged list of withdrawals. Only Page, PageSize, DateFrom
// and DateTo apply.
func (s *WithdrawalsService) History(ctx context.Context, q HistoryQuery) (*WithdrawalHistoryResponse, error) {
	var out WithdrawalHistoryResponse
	if err := s.c.do(ctx, "/v1/withdrawal/history", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
