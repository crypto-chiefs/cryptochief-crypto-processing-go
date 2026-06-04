package cryptochief

import "context"

// StaticDepositsService exposes the static-deposit read endpoints. Static
// deposits are funds received on a per-customer static wallet generated via
// [WalletsService.Generate] with WalletType="static". Access via
// Client.StaticDeposits.
type StaticDepositsService struct{ c *Client }

// StaticDepositStatus values observed in StaticDeposit.Status.
const (
	StaticDepositInMempool    = "in_mempool"
	StaticDepositConfirmCheck = "confirm_check"
	StaticDepositPaid         = "paid"
	StaticDepositDropped      = "dropped"
	StaticDepositReorged      = "reorged"
)

// StaticDeposit is one incoming deposit on a static wallet.
type StaticDeposit struct {
	UUID                  string      `json:"uuid"`
	Status                string      `json:"status"`
	Network               Chain       `json:"network"`
	ChainFamily           ChainFamily `json:"chain_family,omitempty"`
	Coin                  string      `json:"coin"`
	Contract              string      `json:"contract,omitempty"`
	Decimals              int         `json:"decimals,omitempty"`
	ToAddress             string      `json:"to_address"`
	FromAddress           string      `json:"from_address,omitempty"`
	TxHash                string      `json:"tx_hash,omitempty"`
	BlockNumber           int64       `json:"block_number,omitempty"`
	Amount                string      `json:"amount"`
	AmountFiat            string      `json:"amount_fiat,omitempty"`
	Confirmations         int         `json:"confirmations,omitempty"`
	RequiredConfirmations int         `json:"required_confirmations,omitempty"`
	FoundInMempool        bool        `json:"found_in_mempool,omitempty"`
	LogType               string      `json:"log_type,omitempty"`
	CreatedAt             string      `json:"created_at,omitempty"`
	UpdatedAt             string      `json:"updated_at,omitempty"`
	ConfirmedAt           string      `json:"confirmed_at,omitempty"`
	PaidAt                string      `json:"paid_at,omitempty"`
}

// StaticDepositHistoryQuery is the filter for /v1/static-deposit/history.
type StaticDepositHistoryQuery struct {
	Address  string `json:"address,omitempty"`
	Status   string `json:"status,omitempty"`
	Coin     string `json:"coin,omitempty"`
	Network  Chain  `json:"network,omitempty"`
	DateFrom string `json:"date_from,omitempty"`
	DateTo   string `json:"date_to,omitempty"`
	Page     int    `json:"page,omitempty"`
	PageSize int    `json:"page_size,omitempty"`
}

// StaticDepositHistoryResponse is the page of deposits.
type StaticDepositHistoryResponse struct {
	Items []StaticDeposit `json:"items"`
	Meta  HistoryMeta     `json:"meta"`
}

// Info fetches one deposit by uuid.
func (s *StaticDepositsService) Info(ctx context.Context, uuid string) (*StaticDeposit, error) {
	var out StaticDeposit
	if err := s.c.do(ctx, "/v1/static-deposit/info", map[string]string{"uuid": uuid}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// History returns a paged list of static deposits.
func (s *StaticDepositsService) History(ctx context.Context, q StaticDepositHistoryQuery) (*StaticDepositHistoryResponse, error) {
	var out StaticDepositHistoryResponse
	if err := s.c.do(ctx, "/v1/static-deposit/history", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
