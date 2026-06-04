package cryptochief

import "context"

// BlockchainService groups read-only blockchain queries: enabled assets,
// wallet balance, on-chain tx status. Access via Client.Blockchain.
type BlockchainService struct{ c *Client }

// AvailableContract describes one enabled coin/contract on a chain.
type AvailableContract struct {
	Network  Chain  `json:"network"`
	Coin     string `json:"coin"`
	Contract string `json:"contract,omitempty"`
	Type     string `json:"type,omitempty"` // "native" or "token"
	Decimals int    `json:"decimals"`
}

// AvailableContractsResponse is the response of /v1/blockchain/contracts/available.
type AvailableContractsResponse struct {
	Items []AvailableContract `json:"items"`
}

// WalletBalanceRow is one address/coin balance returned by
// /v1/blockchain/wallet/balance.
type WalletBalanceRow struct {
	Contract   string `json:"contract,omitempty"`
	Address    string `json:"address"`
	Value      string `json:"value"`
	HumanValue string `json:"human_value"`
	Decimals   int    `json:"decimals"`
}

// TxStatusRow is one row returned by /v1/blockchain/transaction/status.
type TxStatusRow struct {
	Confirmations int    `json:"confirmations"`
	Fee           string `json:"fee,omitempty"`
	HumanFee      string `json:"human_fee,omitempty"`
	BlockNumber   int64  `json:"block_number,omitempty"`
	Status        string `json:"status,omitempty"`
}

// ContractsAvailable lists the coins/tokens this project is allowed to use.
// Pass network="" for the full set, or scope to one chain. The result's
// Decimals field is what you need for [HumanToBase] / [BaseToHuman].
func (s *BlockchainService) ContractsAvailable(ctx context.Context, network Chain) (*AvailableContractsResponse, error) {
	var body any = struct{}{}
	if network != "" {
		body = map[string]Chain{"network": network}
	}
	var out AvailableContractsResponse
	if err := s.c.do(ctx, "/v1/blockchain/contracts/available", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WalletBalance fetches native + token balances for one or more addresses.
// Pass contracts to limit to specific tokens; empty includes native + every
// recognised token.
func (s *BlockchainService) WalletBalance(ctx context.Context, chain Chain, addresses []string, contracts ...string) ([]WalletBalanceRow, error) {
	body := map[string]any{
		"chain":     chain,
		"addresses": addresses,
	}
	if len(contracts) > 0 {
		body["contracts"] = contracts
	}
	var out []WalletBalanceRow
	if err := s.c.do(ctx, "/v1/blockchain/wallet/balance", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// TransactionStatus reads the current on-chain state of a tx by hash. Useful
// to verify a payout/transaction made it into a block after we report it
// confirmed.
func (s *BlockchainService) TransactionStatus(ctx context.Context, chain Chain, hash string) ([]TxStatusRow, error) {
	body := map[string]any{
		"chain": chain,
		"hash":  hash,
	}
	var out []TxStatusRow
	if err := s.c.do(ctx, "/v1/blockchain/transaction/status", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}
