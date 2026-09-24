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
	// TxStatusCancelled — EVM: replaced by a newer signature from the same
	// address before it was executed; ErrorReason is SUPERSEDED_BY:<new uuid>.
	TxStatusCancelled = "cancelled"
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

// EstimateTransactionRequest is the body of POST /v1/transaction/estimate —
// the Sign request without the callback: nothing is signed or broadcast, so
// no webhooks can follow. TxTypeContract is refused by the API with
// CONTRACT_ESTIMATE_UNSUPPORTED — contract calls have no fee-quote mode.
type EstimateTransactionRequest struct {
	Network     Chain  `json:"network"`
	FromAddress string `json:"from_address"`
	Type        TxType `json:"type"`

	// Transfer-mode fields (TxTypeNative / TxTypeToken).
	ToAddress string `json:"to_address,omitempty"`
	Value     string `json:"value,omitempty"`    // BASE units, e.g. wei
	Contract  string `json:"contract,omitempty"` // token contract for TxTypeToken
}

// EstimateTransactionResponse is what /transaction/estimate returns: a fee
// quote for a transaction that is neither signed nor broadcast and leaves no
// record.
type EstimateTransactionResponse struct {
	Network     Chain  `json:"network"`
	ChainFamily string `json:"chain_family"`
	Type        TxType `json:"type"`
	FromAddress string `json:"from_address"`
	ToAddress   string `json:"to_address"`

	// EstimatedFee is the network fee in the chain's native coin, human units.
	EstimatedFee string `json:"estimated_fee"`
	// EstimatedFeeFiat is the same in USD — "" when the rate is unavailable.
	EstimatedFeeFiat string `json:"estimated_fee_fiat"`
	// Required is the total native coin the from-wallet must hold for the
	// transfer to go through: fee + value for a native transfer, fee alone for
	// a token one (the token amount itself is not native coin).
	Required string `json:"required"`
	// RequiredFiat is Required in USD — "" when the rate is unavailable.
	RequiredFiat string `json:"required_fiat"`

	// TRON-only fee breakdown, human TRX (Energy in energy units). The API
	// omits these entirely on other chains, so they decode as "" / 0 there.
	//
	// FeeExpected is the expected burn given the wallet's current energy pool
	// (staked / delegated / rented energy netted off). It is NOT a funding
	// guarantee — the pool can expire or be consumed by another transfer
	// before this transaction broadcasts. Fund EstimatedFee / Required, which
	// price the transfer as if the pool were empty. FeeLimit is the on-chain
	// cap written into the transaction's fee_limit field; a native TRX
	// transfer carries no fee_limit at all, so it stays "" there. The *_fee
	// components are the gross burn with an empty pool and always sum to
	// EstimatedFee: energy_fee + bandwidth_fee + activation_fee. ActivationFee
	// appears only on a native transfer to an address the chain has not seen
	// yet; a zero component (e.g. EnergyFee when rented energy covers the
	// whole call) stays off the wire.
	FeeExpected   string `json:"fee_expected,omitempty"`
	FeeLimit      string `json:"fee_limit,omitempty"`
	Energy        int64  `json:"energy,omitempty"`
	EnergyFee     string `json:"energy_fee,omitempty"`
	BandwidthFee  string `json:"bandwidth_fee,omitempty"`
	ActivationFee string `json:"activation_fee,omitempty"`
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

	// URLCallback receives the transaction.confirmed, transaction.failed,
	// transaction.expired and transaction.cancelled events.
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
	// SupersededUUIDs — EVM: the earlier unexecuted signatures from the same
	// address that this one replaced; they turn TxStatusCancelled. Empty when
	// there were none.
	SupersededUUIDs []string `json:"superseded_uuids,omitempty"`
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
	UUID        string `json:"uuid"`
	Status      string `json:"status"`
	Network     Chain  `json:"network"`
	ChainFamily string `json:"chain_family,omitempty"`
	FromAddress string `json:"from_address"`
	ToAddress   string `json:"to_address,omitempty"`
	Type        TxType `json:"type,omitempty"`
	Value       string `json:"value,omitempty"`
	Contract    string `json:"contract,omitempty"`
	TxHash      string `json:"tx_hash,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`

	// Deprecated: never populated.
	Coin string `json:"coin,omitempty"`
	// Deprecated: never populated; the signed hex is in SignTransactionResponse.
	SignedTxHex string `json:"signed_tx_hex,omitempty"`
	// Deprecated: never populated.
	Nonce *uint64 `json:"nonce,omitempty"`
	// Deprecated: never populated.
	ActualFee string `json:"actual_fee,omitempty"`
	// Deprecated: never populated.
	ActualFeeFiat string `json:"actual_fee_fiat,omitempty"`

	// CompletedAt is set on final statuses: for confirmed, the moment
	// RequiredConfirmations was reached.
	CompletedAt string `json:"completed_at,omitempty"`
	// ErrorReason is set on failed, expired and cancelled (SUPERSEDED_BY:<uuid>),
	// and on a signed transaction that could not be executed yet
	// ("NONCE_GAP: missing_nonce=<n> blocking_uuid=<uuid>",
	// "NONCE_ALREADY_USED: chain_nonce=<n>").
	ErrorReason string `json:"error_reason,omitempty"`

	// Deprecated: never populated; use CompletedAt.
	UpdatedAt string `json:"updated_at,omitempty"`
	// Deprecated: never populated; use ErrorReason.
	Error string `json:"error,omitempty"`

	// Confirmations is 0 until the transaction is in a block, then rises while
	// Status is broadcasted.
	Confirmations int `json:"confirmations"`
	// RequiredConfirmations is the count at which the transaction is confirmed.
	RequiredConfirmations int `json:"required_confirmations"`
}

// IsTerminal reports whether the tx reached a final state.
func (t TransactionInfo) IsTerminal() bool {
	switch t.Status {
	case TxStatusConfirmed, TxStatusFailed, TxStatusExpired, TxStatusCancelled:
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

// Estimate quotes the network fee for a would-be transaction WITHOUT signing
// or broadcasting anything and without leaving a record — a dry run of Sign
// for the "can this wallet afford it" question. Required tells how much of the
// chain's native coin the from-wallet must hold: fee + value for a native
// transfer, fee alone for a token one. Both fiat fields are "" when the rate
// is unavailable — an annotation, not a failure. TxTypeContract is refused
// with CONTRACT_ESTIMATE_UNSUPPORTED.
func (s *TransactionsService) Estimate(ctx context.Context, in *EstimateTransactionRequest) (*EstimateTransactionResponse, error) {
	var out EstimateTransactionResponse
	if err := s.c.do(ctx, "/v1/transaction/estimate", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
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
