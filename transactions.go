package cryptochief

import "context"

// TransactionsService groups the two-phase signature/execute endpoints —
// merchants can sign and broadcast arbitrary transactions from any of their
// own project wallets. Access via Client.Transactions.
type TransactionsService struct{ c *Client }

// TxType is the discriminator the API uses to pick a signing path.
type TxType string

const (
	// TxTypeNative — native-asset transfer. Body: to_address + value.
	TxTypeNative TxType = "native"
	// TxTypeToken — ERC-20-style token transfer. Body: to_address + value + contract.
	TxTypeToken TxType = "token"
	// TxTypeContract — arbitrary contract call(s). Body: calls[].
	// Supported on EVM/TRON/Solana/TON (XRP and UTXO families reject).
	TxTypeContract TxType = "contract"
)

// Transaction status values surface in TransactionInfo.Status.
const (
	TxStatusSigned       = "signed"
	TxStatusBroadcasting = "broadcasting"
	TxStatusBroadcasted  = "broadcasted"
	TxStatusConfirmed    = "confirmed"
	TxStatusFailed       = "failed"
	TxStatusExpired      = "expired"
)

// ContractCall is one instruction in a TxTypeContract request.
//
// Per-family encoding:
//
//   - EVM/TRON  — Data is hex calldata (0x...), single call per request.
//   - TON       — Data is base64 BoC body cell, single call, Bounce default true.
//   - Solana    — To is the program id, Data is base64 instruction data,
//     Accounts lists the metas, multiple instructions per request
//     are allowed (only the From wallet signs).
type ContractCall struct {
	To       string          `json:"to"`
	Value    string          `json:"value,omitempty"`
	Data     string          `json:"data"`
	Accounts []SolanaAccount `json:"accounts,omitempty"`
	Bounce   *bool           `json:"bounce,omitempty"`
}

// SolanaAccount mirrors Solana's AccountMeta.
type SolanaAccount struct {
	Pubkey     string `json:"pubkey"`
	IsSigner   bool   `json:"is_signer"`
	IsWritable bool   `json:"is_writable"`
}

// SignTransactionRequest is the body of POST /v1/transaction/signature.
//
// Mode is the explicit discriminator: supplying fields foreign to the
// chosen Type produces a validation error (TRANSFER_FIELDS_NOT_ALLOWED_FOR_CONTRACT,
// CONTRACT_REQUIRED_FOR_TOKEN, CALLS_NOT_ALLOWED_FOR_TRANSFER, ...).
type SignTransactionRequest struct {
	Network     Chain  `json:"network"`
	FromAddress string `json:"from_address"`
	Type        TxType `json:"type"`

	// Transfer-mode fields (TxTypeNative / TxTypeToken).
	ToAddress string `json:"to_address,omitempty"`
	Value     string `json:"value,omitempty"`    // BASE units, e.g. wei
	Contract  string `json:"contract,omitempty"` // token contract for TxTypeToken

	// Contract-mode (TxTypeContract).
	Calls []ContractCall `json:"calls,omitempty"`

	// URLCallback receives transaction.confirmed / transaction.failed events.
	URLCallback string `json:"url_callback,omitempty"`
}

// SignTransactionResponse is what /transaction/signature returns. The signed
// bytes are NOT yet broadcast — call Execute (or pass the uuid + signed_tx_hex
// to it) before the TTL elapses.
type SignTransactionResponse struct {
	UUID        string `json:"uuid"`
	Status      string `json:"status"`
	SignedTxHex string `json:"signed_tx_hex"`
	TxHash      string `json:"tx_hash"`
	ExpiresAt   string `json:"expires_at"`
	ChainFamily string `json:"chain_family"`
	Network     Chain  `json:"network,omitempty"`
}

// ExecuteTransactionRequest is the body of POST /v1/transaction/execute.
// SignedTxHex is optional — pass it only when you want the API to verify
// the bytes haven't been altered between Sign and Execute. The default is
// to broadcast the reserved signed bytes by uuid.
type ExecuteTransactionRequest struct {
	UUID        string `json:"uuid"`
	SignedTxHex string `json:"signed_tx_hex,omitempty"`
}

// TransactionInfo describes the persistent record of one signed/broadcast
// transaction.
type TransactionInfo struct {
	UUID          string  `json:"uuid"`
	Status        string  `json:"status"`
	Network       Chain   `json:"network"`
	ChainFamily   string  `json:"chain_family,omitempty"`
	FromAddress   string  `json:"from_address"`
	ToAddress     string  `json:"to_address,omitempty"`
	Type          TxType  `json:"type,omitempty"`
	Value         string  `json:"value,omitempty"`
	Coin          string  `json:"coin,omitempty"`
	Contract      string  `json:"contract,omitempty"`
	TxHash        string  `json:"tx_hash,omitempty"`
	SignedTxHex   string  `json:"signed_tx_hex,omitempty"`
	ExpiresAt     string  `json:"expires_at,omitempty"`
	Nonce         *uint64 `json:"nonce,omitempty"`
	ActualFee     string  `json:"actual_fee,omitempty"`
	ActualFeeFiat string  `json:"actual_fee_fiat,omitempty"`
	CreatedAt     string  `json:"created_at,omitempty"`
	UpdatedAt     string  `json:"updated_at,omitempty"`
	Error         string  `json:"error,omitempty"`
}

// IsTerminal reports whether the tx reached a final state.
func (t TransactionInfo) IsTerminal() bool {
	switch t.Status {
	case TxStatusConfirmed, TxStatusFailed, TxStatusExpired:
		return true
	}
	return false
}

// Succeeded reports whether the tx was confirmed on-chain.
func (t TransactionInfo) Succeeded() bool { return t.Status == TxStatusConfirmed }

// TransactionHistoryResponse is the page of transactions.
type TransactionHistoryResponse struct {
	Items []TransactionInfo `json:"items"`
	Meta  HistoryMeta       `json:"meta"`
}

// Sign builds and signs a transaction WITHOUT broadcasting. Returns the uuid
// to reference in Execute, plus the signed hex bytes for inspection. The
// signature has a per-family TTL (EVM 10m, UTXO 15m, TRON 45s, Solana 60s,
// XRP 90s, TON 300s) — call Execute before it elapses.
func (s *TransactionsService) Sign(ctx context.Context, in *SignTransactionRequest) (*SignTransactionResponse, error) {
	var out SignTransactionResponse
	if err := s.c.do(ctx, "/v1/transaction/signature", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Execute broadcasts a previously-signed transaction. Pass just the uuid;
// SignedTxHex is optional and only used for a client-vs-server byte match
// check.
func (s *TransactionsService) Execute(ctx context.Context, in *ExecuteTransactionRequest) (*TransactionInfo, error) {
	var out TransactionInfo
	if err := s.c.do(ctx, "/v1/transaction/execute", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Info fetches the current state of one transaction by uuid.
func (s *TransactionsService) Info(ctx context.Context, uuid string) (*TransactionInfo, error) {
	var out TransactionInfo
	if err := s.c.do(ctx, "/v1/transaction/info", map[string]string{"uuid": uuid}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// History returns a paged list of merchant-owned transactions.
func (s *TransactionsService) History(ctx context.Context, q HistoryQuery) (*TransactionHistoryResponse, error) {
	var out TransactionHistoryResponse
	if err := s.c.do(ctx, "/v1/transaction/history", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
