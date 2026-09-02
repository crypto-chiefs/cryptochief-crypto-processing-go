package cryptochief

import "context"

// BlockchainService groups read-only blockchain queries: supported chains,
// enabled assets, wallet balance, on-chain tx status. Access via
// Client.Blockchain.
type BlockchainService struct{ c *Client }

// SupportedChain is one chain the platform's scanner is currently connected to,
// as returned by [BlockchainService.SupportedChains].
type SupportedChain struct {
	// Name is the CHAIN code, the same value that goes into every "network" /
	// "chain" / "network_code" field.
	Name Chain `json:"name"`
	// Type is the protocol family the scanner reads the chain with — "evm",
	// "tron", "solana" and so on. It is lowercase and is NOT the [ChainFamily]
	// value ("EVM") that responses elsewhere carry; compare it case-insensitively
	// if you compare it at all.
	Type string `json:"type"`
}

// AvailableContract describes one coin/contract on a chain — an asset enabled
// for the project ([BlockchainService.ContractsAvailable]) or one the platform
// supports at all ([BlockchainService.ContractsList]). Both endpoints send the
// same shape.
type AvailableContract struct {
	Network Chain  `json:"network"`
	Coin    string `json:"coin"`
	// Contract is the token contract address, and an EMPTY STRING for a native
	// coin — the platform sends "" rather than omitting the field.
	Contract string `json:"contract,omitempty"`
	// ChainFamily is the protocol family of Network, e.g. [FamilyEVM].
	ChainFamily ChainFamily `json:"chain_family,omitempty"`
	Type        string      `json:"type,omitempty"` // "native" or "token"
	// IsTest marks an asset that lives on a test network. It is how the two
	// environments are told apart in a catalogue that carries both.
	//
	// Deliberately without omitempty: false is the mainnet answer, and omitting
	// it when you re-marshal a row would turn "this is a real asset" into "this
	// row says nothing", on the one field that separates a test asset from a
	// real one.
	IsTest   bool `json:"is_test"`
	Decimals int  `json:"decimals"`
}

// AvailableContractsResponse is the response of
// /v1/blockchain/contracts/available and of /v1/blockchain/contracts/list.
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

// SupportedChains lists the chains the platform's blockchain scanner is
// currently connected to — infrastructure-level information, not your project's
// asset catalogue. For what your project can actually be paid in, use
// [BlockchainService.ContractsAvailable].
//
// The endpoint answers with a bare JSON array rather than an {"items": …}
// envelope, which is why this returns a slice.
//
// An empty result can arrive as a literal JSON null rather than []. That is not
// an error and never yields a nil slice here: the result is always a usable
// slice, empty when there is nothing to report, so re-marshalling it writes []
// and not null.
//
//	chains, err := c.Blockchain.SupportedChains(ctx)
//	for _, ch := range chains {
//	    fmt.Println(ch.Name, ch.Type) // ETH_MAINNET evm
//	}
func (s *BlockchainService) SupportedChains(ctx context.Context) ([]SupportedChain, error) {
	var out []SupportedChain
	if err := s.c.do(ctx, "/v1/blockchains/list", struct{}{}, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return []SupportedChain{}, nil
	}
	return out, nil
}

// ContractsList returns every coin and token the platform supports, on every
// network, regardless of what this project has enabled — the catalogue to build
// a "which assets could we turn on" picker from.
//
// It is platform-wide, so there is nothing to filter by. For what the project
// can be paid in right now — the list that governs orders, sweeps and payouts —
// use [BlockchainService.ContractsAvailable]; the items are the same shape.
//
// The catalogue spans both environments: read [AvailableContract.IsTest] to tell
// a test-network asset from a real one.
func (s *BlockchainService) ContractsList(ctx context.Context) (*AvailableContractsResponse, error) {
	var out AvailableContractsResponse
	if err := s.c.do(ctx, "/v1/blockchain/contracts/list", struct{}{}, &out); err != nil {
		return nil, err
	}
	return &out, nil
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
