package cryptochief

import "context"

// SweepsService groups treasury sweep endpoints. A sweep moves funds from a
// transit wallet to the project's master wallet. Access via Client.Sweeps.
type SweepsService struct{ c *Client }

// SweepMode filters Sweeps.History by trigger source.
type SweepMode string

const (
	SweepModeAuto  SweepMode = "auto"
	SweepModeForce SweepMode = "force"
)

// SweepHistoryQuery is the body of /v1/sweeps/history.
type SweepHistoryQuery struct {
	Mode     SweepMode `json:"mode,omitempty"`
	Page     int       `json:"page,omitempty"`
	PageSize int       `json:"page_size,omitempty"`
}

// SweepWalletHistoryQuery is the body of /v1/sweeps/wallet/history — same
// shape plus the wallet address filter.
type SweepWalletHistoryQuery struct {
	Address  string    `json:"address"`
	Mode     SweepMode `json:"mode,omitempty"`
	Page     int       `json:"page,omitempty"`
	PageSize int       `json:"page_size,omitempty"`
}

// Sweep is one transit→master movement.
type Sweep struct {
	TaskID         string      `json:"task_id"`
	SweepTxHash    string      `json:"sweep_tx_hash,omitempty"`
	Status         string      `json:"status"`
	WalletAddress  string      `json:"wallet_address"`
	Chain          Chain       `json:"chain"`
	ChainFamily    ChainFamily `json:"chain_family,omitempty"`
	AssetSymbol    string      `json:"asset_symbol,omitempty"`
	AssetType      string      `json:"asset_type,omitempty"`
	AmountHuman    string      `json:"amount_human,omitempty"`
	GasFeeHuman    string      `json:"gas_fee_human,omitempty"`
	GasFeeFiat     string      `json:"gas_fee_fiat,omitempty"`
	ServiceFeeFiat string      `json:"service_fee_fiat,omitempty"`
	CreatedAt      string      `json:"created_at,omitempty"`
	UpdatedAt      string      `json:"updated_at,omitempty"`
}

// SweepHistoryResponse is the page of sweeps.
type SweepHistoryResponse struct {
	Items []Sweep     `json:"items"`
	Meta  HistoryMeta `json:"meta"`
}

// ForceSweepResponse is the synchronous ack of /v1/sweeps/force. The actual
// transit→master movement happens asynchronously; poll WalletHistory to
// observe the resulting Sweep record.
type ForceSweepResponse struct {
	Status string `json:"status"`
}

// Force triggers an immediate transit→master sweep for one address. The
// status acknowledges acceptance; the resulting Sweep record appears via
// WalletHistory once the on-chain tx is built and submitted.
func (s *SweepsService) Force(ctx context.Context, address string, network Chain) (*ForceSweepResponse, error) {
	body := map[string]string{
		"address":      address,
		"network_code": string(network),
	}
	var out ForceSweepResponse
	if err := s.c.do(ctx, "/v1/sweeps/force", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// History returns recent sweeps across the whole project.
func (s *SweepsService) History(ctx context.Context, q SweepHistoryQuery) (*SweepHistoryResponse, error) {
	var out SweepHistoryResponse
	if err := s.c.do(ctx, "/v1/sweeps/history", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WalletHistory returns recent sweeps scoped to one wallet.
func (s *SweepsService) WalletHistory(ctx context.Context, q SweepWalletHistoryQuery) (*SweepHistoryResponse, error) {
	var out SweepHistoryResponse
	if err := s.c.do(ctx, "/v1/sweeps/wallet/history", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
