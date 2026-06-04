package cryptochief

import "context"

// WithdrawalsService groups the read-only withdrawal endpoints. The
// public API does not create withdrawals directly — only reads. Access
// via Client.Withdrawals.
type WithdrawalsService struct{ c *Client }

// Withdrawal is the persistent record of one outbound treasury movement.
type Withdrawal struct {
	UUID        string `json:"uuid"`
	Status      string `json:"status"`
	Network     Chain  `json:"network"`
	Coin        string `json:"coin,omitempty"`
	Contract    string `json:"contract,omitempty"`
	Amount      string `json:"amount"`
	AmountFiat  string `json:"amount_fiat,omitempty"`
	FromAddress string `json:"from_address,omitempty"`
	ToAddress   string `json:"to_address,omitempty"`
	TxHash      string `json:"tx_hash,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	ConfirmedAt string `json:"confirmed_at,omitempty"`
	Error       string `json:"error,omitempty"`
}

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

// History returns a paged list of withdrawals.
func (s *WithdrawalsService) History(ctx context.Context, q HistoryQuery) (*WithdrawalHistoryResponse, error) {
	var out WithdrawalHistoryResponse
	if err := s.c.do(ctx, "/v1/withdrawal/history", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
