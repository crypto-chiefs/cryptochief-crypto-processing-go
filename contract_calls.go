package cryptochief

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/tvm/cell"
)

// EVMCallRequest is the high-level form of a contract call on EVM or TRON.
// Instead of hand-encoding the data field, pass a Solidity-style signature
// and a list of argument values — the library encodes calldata for you.
//
//	signed, _ := c.Transactions.SignEVMCall(ctx, &cryptochief.EVMCallRequest{
//	    Network:     cryptochief.ChainEthSepolia,
//	    FromAddress: "0xYourWallet…",
//	    Contract:    "0xUniswapV2Router…",
//	    Method:      "swapExactTokensForTokens(uint256,uint256,address[],address,uint256)",
//	    Args:        []any{amountIn, amountOutMin, path, to, deadline},
//	    URLCallback: "https://your.app/webhooks/transaction",
//	})
type EVMCallRequest struct {
	// Network is the chain to broadcast on. EVM and TRON chains both use
	// the EVM-ABI encoding; pass either family.
	Network Chain
	// FromAddress is one of the project wallets that owns the call.
	FromAddress string
	// Contract is the destination contract address (hex 0x… for EVM, T…
	// base58 or 0x41-prefixed hex for TRON — both forms work).
	Contract string
	// Method is the Solidity-style canonical signature, e.g.
	//   "transfer(address,uint256)"
	//   "swapExactTokensForTokens(uint256,uint256,address[],address,uint256)"
	Method string
	// Args matches the order of types in Method. See EncodeEVMCall for the
	// accepted value forms per type.
	Args []any
	// Value is the native amount (in base units — wei/sun) attached to the
	// call. Empty or "0" sends nothing.
	Value string
	// URLCallback receives the transaction.* webhook.
	URLCallback string
}

// SignEVMCall encodes the call's calldata and signs a TxTypeContract
// transaction without broadcasting. Returns the same SignTransactionResponse
// as [TransactionsService.Sign] — pass its UUID to [TransactionsService.Execute]
// when you want to send.
//
// Works for every EVM chain and for TRON (TRON shares the ABI encoding).
func (s *TransactionsService) SignEVMCall(ctx context.Context, in *EVMCallRequest) (*SignTransactionResponse, error) {
	dataHex, err := EncodeEVMCallHex(in.Method, in.Args...)
	if err != nil {
		return nil, fmt.Errorf("cryptochief: encode call %q: %w", in.Method, err)
	}
	value := in.Value
	if value == "" {
		value = "0"
	}
	return s.Sign(ctx, &SignTransactionRequest{
		Network:     in.Network,
		FromAddress: in.FromAddress,
		Type:        TxTypeContract,
		URLCallback: in.URLCallback,
		Calls: []ContractCall{{
			To:    in.Contract,
			Value: value,
			Data:  dataHex,
		}},
	})
}

// SignTronCall is an alias for SignEVMCall — TRON uses the same ABI
// encoding. Provided so calling code reads naturally.
func (s *TransactionsService) SignTronCall(ctx context.Context, in *EVMCallRequest) (*SignTransactionResponse, error) {
	return s.SignEVMCall(ctx, in)
}

// ERC20Transfer is a one-liner for the most common token operation. The
// recipient amount must be in token base units (use [HumanToBase] with the
// token's decimals to convert from a human-readable amount).
//
//	wei, _ := cryptochief.HumanToBase("12.5", 6)            // USDT has 6 decimals
//	signed, _ := c.Transactions.ERC20Transfer(ctx, &cryptochief.ERC20TransferRequest{
//	    Network: cryptochief.ChainEthMainnet, FromAddress: "0x…",
//	    TokenContract: "0xdAC17F958D2ee523a2206206994597C13D831ec7",
//	    Recipient: "0x…", Amount: wei,
//	})
type ERC20TransferRequest struct {
	Network       Chain
	FromAddress   string
	TokenContract string
	Recipient     string
	Amount        any // *big.Int, uint64, string, ...
	URLCallback   string
}

// ERC20Transfer signs an ERC-20 / TRC-20 transfer(address,uint256) call.
func (s *TransactionsService) ERC20Transfer(ctx context.Context, in *ERC20TransferRequest) (*SignTransactionResponse, error) {
	return s.SignEVMCall(ctx, &EVMCallRequest{
		Network:     in.Network,
		FromAddress: in.FromAddress,
		Contract:    in.TokenContract,
		Method:      "transfer(address,uint256)",
		Args:        []any{in.Recipient, in.Amount},
		URLCallback: in.URLCallback,
	})
}

// AnchorCallRequest is the high-level form of an Anchor (Solana) program
// invocation: instead of hand-building the instruction bytes, give the
// method name and a list of Borsh-typed arguments.
//
//	signed, _ := c.Transactions.SignAnchorCall(ctx, &cryptochief.AnchorCallRequest{
//	    Network:     cryptochief.ChainSolanaDevnet,
//	    FromAddress: "YourWallet…",
//	    Program:     "ProgramId…",
//	    Method:      "initialize",
//	    Args: []cryptochief.BorshValue{
//	        cryptochief.BorshU64(1_000_000),
//	        cryptochief.BorshPubkey("Recipient…"),
//	    },
//	    Accounts: []cryptochief.SolanaAccount{
//	        {Pubkey: "Acc1…", IsSigner: true,  IsWritable: true},
//	        {Pubkey: "Acc2…", IsSigner: false, IsWritable: true},
//	    },
//	    URLCallback: "https://your.app/webhooks/transaction",
//	})
//
// The Accounts list MUST match the order the program expects — Solana has no
// on-chain ABI metadata, so the SDK can't compute it for you.
type AnchorCallRequest struct {
	Network     Chain
	FromAddress string
	Program     string // program ID (base58 pubkey)
	Method      string // Anchor method name (e.g. "initialize")
	Args        []BorshValue
	Accounts    []SolanaAccount
	URLCallback string
}

// SignAnchorCall encodes the instruction data for an Anchor program call
// (8-byte discriminator + Borsh-encoded args) and signs a TxTypeContract
// transaction. Returns the same SignTransactionResponse as [Sign].
//
// For non-Anchor programs use [TransactionsService.SignSolanaCall] with
// raw instruction bytes instead.
func (s *TransactionsService) SignAnchorCall(ctx context.Context, in *AnchorCallRequest) (*SignTransactionResponse, error) {
	data, err := EncodeAnchorInstruction(in.Method, in.Args...)
	if err != nil {
		return nil, fmt.Errorf("cryptochief: encode anchor instruction %q: %w", in.Method, err)
	}
	return s.Sign(ctx, &SignTransactionRequest{
		Network:     in.Network,
		FromAddress: in.FromAddress,
		Type:        TxTypeContract,
		URLCallback: in.URLCallback,
		Calls: []ContractCall{{
			To:       in.Program,
			Data:     base64.StdEncoding.EncodeToString(data),
			Accounts: in.Accounts,
		}},
	})
}

// SolanaCallRequest is the low-level form for non-Anchor Solana programs:
// you supply pre-built instruction bytes and the accounts list.
type SolanaCallRequest struct {
	Network         Chain
	FromAddress     string
	Program         string
	InstructionData []byte
	Accounts        []SolanaAccount
	URLCallback     string
}

// SignSolanaCall signs a Solana program call with raw instruction bytes.
// Use this when the program is not an Anchor program (no discriminator,
// custom argument layout).
func (s *TransactionsService) SignSolanaCall(ctx context.Context, in *SolanaCallRequest) (*SignTransactionResponse, error) {
	return s.Sign(ctx, &SignTransactionRequest{
		Network:     in.Network,
		FromAddress: in.FromAddress,
		Type:        TxTypeContract,
		URLCallback: in.URLCallback,
		Calls: []ContractCall{{
			To:       in.Program,
			Data:     base64.StdEncoding.EncodeToString(in.InstructionData),
			Accounts: in.Accounts,
		}},
	})
}

// TONCallRequest is the high-level form for a TON contract call. The body
// is a base64-encoded BoC cell — TON has no Solidity-style ABI we can
// auto-encode, so the caller supplies the body cell directly.
//
// Build BoC bodies with a battle-tested TON library
// (github.com/xssnick/tonutils-go is the usual pick), then pass the bytes
// here.
type TONCallRequest struct {
	Network     Chain
	FromAddress string
	Contract    string
	BodyCell    []byte // raw BoC bytes; will be base64-encoded
	Value       string
	Bounce      *bool
	URLCallback string
}

// SignTONCall signs a TON contract call. Body must be a BoC cell already.
func (s *TransactionsService) SignTONCall(ctx context.Context, in *TONCallRequest) (*SignTransactionResponse, error) {
	value := in.Value
	if value == "" {
		value = "0"
	}
	return s.Sign(ctx, &SignTransactionRequest{
		Network:     in.Network,
		FromAddress: in.FromAddress,
		Type:        TxTypeContract,
		URLCallback: in.URLCallback,
		Calls: []ContractCall{{
			To:     in.Contract,
			Value:  value,
			Data:   base64.StdEncoding.EncodeToString(in.BodyCell),
			Bounce: in.Bounce,
		}},
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// TON high-level helpers — Jetton transfer, NFT transfer, text comment.
//
// These build the BoC body for you using tonutils-go internally, then go
// through the same Sign() path as every other contract call. You never see
// a Cell or a base64 BoC.
// ─────────────────────────────────────────────────────────────────────────────

// JettonTransferRequest sends Jetton tokens from a project wallet. The
// SDK does the cell encoding (TIP-74 transfer body), optional
// Jetton-wallet-address resolution, and a sensible default gas budget.
//
//	signed, _ := c.Transactions.JettonTransfer(ctx, &cryptochief.JettonTransferRequest{
//	    Network:      cryptochief.ChainTONMainnet,
//	    FromAddress:  "EQYourWallet…",
//	    JettonMaster: "EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs", // USDT
//	    Recipient:    "EQRecipient…",
//	    Amount:       cryptochief.MustHumanToBase("12.5", 6), // USDT decimals = 6
//	    Memo:         "Order #4242",                          // optional
//	    URLCallback:  "https://your.app/webhooks/transaction",
//	})
type JettonTransferRequest struct {
	// Network — must be a TON chain.
	Network Chain
	// FromAddress is the sender's TON wallet (owns the Jetton wallet).
	FromAddress string
	// JettonMaster is the Jetton master contract address (the token's ID).
	JettonMaster string
	// JettonWalletAddress is OPTIONAL — the sender's Jetton wallet for
	// this token. If empty, the SDK resolves it automatically.
	JettonWalletAddress string
	// Recipient is the recipient's main TON wallet — NOT their Jetton
	// wallet. The network handles the wallet-to-wallet hop.
	Recipient string
	// Amount is the Jetton amount to send, in base units (use [HumanToBase]
	// with the Jetton's decimals to convert).
	Amount *big.Int
	// ResponseDestination receives the unused gas back. Defaults to
	// FromAddress (recommended).
	ResponseDestination string
	// AttachedTON is the gas budget for the chain of internal messages
	// (sender → sender's Jetton wallet → recipient's Jetton wallet →
	// notify), in nanoTON as a decimal string.
	//
	// Leave empty for the SDK to pick: 0.07 TON if the recipient already
	// holds a Jetton wallet for this token, 0.15 TON if a new one has
	// to be deployed.
	AttachedTON string
	// ForwardTONAmount is forwarded to the recipient's notification handler
	// in nanoTON (decimal string). Default: 1 nanoTON when Memo is set
	// (otherwise the receiver wouldn't see the text), 0 when Memo is empty.
	ForwardTONAmount string
	// Memo, if non-empty, is attached as the forward_payload of the
	// Jetton transfer — wallets display it as the transfer "message". The
	// SDK encodes it as the canonical text-comment cell (op 0 + UTF-8
	// snake string) and stores it as a ref.
	Memo string
	// QueryID is the Jetton transfer's correlation id; 0 is fine for
	// most flows.
	QueryID     uint64
	URLCallback string
}

// Default gas budgets for a Jetton transfer. The attached TON covers
// the sender's Jetton wallet processing + forward to the receiver's
// Jetton wallet (+ deploy of a fresh receiver wallet if needed) +
// transfer_notification + excess return.
//
//   - 0.07 TON when the receiver already has a Jetton wallet for this token.
//   - 0.15 TON when the receiver's wallet has to be deployed first.
const (
	jettonAttachedExistingWallet = 70_000_000  // 0.07 TON
	jettonAttachedNewWallet      = 150_000_000 // 0.15 TON
)

// JettonTransfer builds the standard transfer body (op 0x0f8a7ea5), resolves
// the sender's Jetton wallet address (via the configured RPC service if you
// didn't provide it), picks a sensible gas budget, and signs the resulting
// contract call. The returned uuid is the same shape as any other
// [TransactionsService.Sign] call — poll with [WaitForTransaction] and
// broadcast with [TransactionsService.Execute].
func (s *TransactionsService) JettonTransfer(ctx context.Context, in *JettonTransferRequest) (*SignTransactionResponse, error) {
	if in.Recipient == "" {
		return nil, fmt.Errorf("cryptochief: JettonTransfer: Recipient required")
	}
	if in.JettonMaster == "" && in.JettonWalletAddress == "" {
		return nil, fmt.Errorf("cryptochief: JettonTransfer: JettonMaster or JettonWalletAddress required")
	}

	rpc := s.c.ton()

	jettonWallet := in.JettonWalletAddress
	if jettonWallet == "" {
		resolved, err := rpc.lookupJettonWallet(ctx, in.JettonMaster, in.FromAddress)
		if err != nil {
			return nil, err
		}
		jettonWallet = resolved
	}

	dest, err := tonaddrParse(in.Recipient)
	if err != nil {
		return nil, fmt.Errorf("cryptochief: JettonTransfer: recipient: %w", err)
	}
	respDest := in.ResponseDestination
	if respDest == "" {
		respDest = in.FromAddress
	}
	resp, err := tonaddrParse(respDest)
	if err != nil {
		return nil, fmt.Errorf("cryptochief: JettonTransfer: response destination: %w", err)
	}

	// Memo encodes as the canonical text-comment payload (op 0 + UTF-8 in
	// Snake form). Stored as a ref so it survives even very long messages
	// — and the receiver wallet sees it as the transfer "comment".
	var fwdPayload *cell.Cell
	if in.Memo != "" {
		fwdPayload = buildTextCommentCell(in.Memo)
	}

	// Forward TON default — a 1-nanoTON forward is enough to deliver the
	// transfer_notification (with the memo) to the receiver. Without it
	// the memo never arrives.
	fwdInput := in.ForwardTONAmount
	if fwdInput == "" && in.Memo != "" {
		fwdInput = "1"
	}
	fwd, err := parseNanoOrZero(fwdInput, "ForwardTONAmount")
	if err != nil {
		return nil, err
	}

	bodyBoC, err := buildJettonTransferBody(in.QueryID, in.Amount, dest, resp, nil, fwd, fwdPayload)
	if err != nil {
		return nil, fmt.Errorf("cryptochief: JettonTransfer: build body: %w", err)
	}

	attached := in.AttachedTON
	if attached == "" {
		attached = strconvFormatInt(jettonAttachedNewWallet)
		if rpc.hasJettonWallet(ctx, in.JettonMaster, in.Recipient) {
			attached = strconvFormatInt(jettonAttachedExistingWallet)
		}
	}
	bounce := true
	return s.SignTONCall(ctx, &TONCallRequest{
		Network:     in.Network,
		FromAddress: in.FromAddress,
		Contract:    jettonWallet,
		BodyCell:    bodyBoC,
		Value:       attached,
		Bounce:      &bounce,
		URLCallback: in.URLCallback,
	})
}

// strconvFormatInt is a tiny indirection so jettonAttached* constants stay
// `const int` instead of becoming variables.
func strconvFormatInt(n int) string { return big.NewInt(int64(n)).String() }

// NFTTransferRequest transfers ownership of an NFT item.
type NFTTransferRequest struct {
	Network             Chain
	FromAddress         string
	NFTItem             string // address of the NFT item contract
	NewOwner            string
	ResponseDestination string
	AttachedTON         string
	ForwardTONAmount    string
	QueryID             uint64
	URLCallback         string
}

// NFTTransfer builds the standard NFT transfer body (op 0x5fcc3d14) and
// signs the call.
func (s *TransactionsService) NFTTransfer(ctx context.Context, in *NFTTransferRequest) (*SignTransactionResponse, error) {
	if in.NFTItem == "" || in.NewOwner == "" {
		return nil, fmt.Errorf("cryptochief: NFTTransfer: NFTItem and NewOwner required")
	}
	owner, err := tonaddrParse(in.NewOwner)
	if err != nil {
		return nil, fmt.Errorf("cryptochief: NFTTransfer: new owner: %w", err)
	}
	respDest := in.ResponseDestination
	if respDest == "" {
		respDest = in.FromAddress
	}
	resp, err := tonaddrParse(respDest)
	if err != nil {
		return nil, fmt.Errorf("cryptochief: NFTTransfer: response destination: %w", err)
	}
	fwd, err := parseNanoOrZero(in.ForwardTONAmount, "ForwardTONAmount")
	if err != nil {
		return nil, err
	}
	bodyBoC, err := buildNFTTransferBody(in.QueryID, owner, resp, nil, fwd, nil)
	if err != nil {
		return nil, fmt.Errorf("cryptochief: NFTTransfer: build body: %w", err)
	}
	attached := in.AttachedTON
	if attached == "" {
		attached = "50000000"
	}
	bounce := true
	return s.SignTONCall(ctx, &TONCallRequest{
		Network:     in.Network,
		FromAddress: in.FromAddress,
		Contract:    in.NFTItem,
		BodyCell:    bodyBoC,
		Value:       attached,
		Bounce:      &bounce,
		URLCallback: in.URLCallback,
	})
}

// TONCommentRequest sends TON with a text comment attached — the kind
// every TON wallet shows in the "message" field of a transfer.
type TONCommentRequest struct {
	Network     Chain
	FromAddress string
	Recipient   string
	Text        string
	// AmountTON is in nanoTON. Empty means 0 (comment-only — but TON
	// requires a positive value; supply at least 1 nanoTON or the message
	// won't reach the wallet).
	AmountTON   string
	URLCallback string
}

// SendTONComment signs a transfer-with-comment to recipient. The body is
// the simple op=0 + UTF-8 text form recognised by every wallet.
func (s *TransactionsService) SendTONComment(ctx context.Context, in *TONCommentRequest) (*SignTransactionResponse, error) {
	if in.Recipient == "" {
		return nil, fmt.Errorf("cryptochief: SendTONComment: Recipient required")
	}
	bodyBoC, err := buildTextCommentBody(in.Text)
	if err != nil {
		return nil, fmt.Errorf("cryptochief: SendTONComment: build body: %w", err)
	}
	amount := in.AmountTON
	if amount == "" {
		amount = "0"
	}
	bounce := false // most wallets are non-bounceable receivers
	return s.SignTONCall(ctx, &TONCallRequest{
		Network:     in.Network,
		FromAddress: in.FromAddress,
		Contract:    in.Recipient,
		BodyCell:    bodyBoC,
		Value:       amount,
		Bounce:      &bounce,
		URLCallback: in.URLCallback,
	})
}

// tonaddrParse accepts any of TON's address forms (user-friendly EQ/UQ,
// raw "workchain:hex") and returns the tonutils-go internal type used by
// the cell encoders. Hidden so callers only see plain strings.
func tonaddrParse(s string) (*address.Address, error) {
	if a, err := address.ParseAddr(s); err == nil {
		return a, nil
	}
	if a, err := address.ParseRawAddr(s); err == nil {
		return a, nil
	}
	return nil, fmt.Errorf("invalid TON address %q (expected EQ/UQ or workchain:hex)", s)
}

// parseNanoOrZero converts a decimal nanoTON string into *big.Int. Empty
// input is allowed and yields 0.
func parseNanoOrZero(s, field string) (*big.Int, error) {
	if s == "" {
		return big.NewInt(0), nil
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("cryptochief: %s: invalid decimal nanoTON %q", field, s)
	}
	return n, nil
}

// NanoTON is a small convenience that converts a human-friendly TON amount
// string ("0.05") into a base-unit nanoTON decimal string ("50000000"),
// matching the format AttachedTON / ForwardTONAmount expect. Panics on
// malformed input — use [HumanToBase] for the non-panicking variant.
func NanoTON(human string) string {
	n, err := HumanToBase(human, 9)
	if err != nil {
		panic("cryptochief.NanoTON: " + err.Error())
	}
	return n.String()
}

// MustHumanToBase is a panicking convenience for use in literal initialisers.
// Returns the *big.Int form of HumanToBase, or panics on bad input.
func MustHumanToBase(human string, decimals int) *big.Int {
	n, err := HumanToBase(human, decimals)
	if err != nil {
		panic("cryptochief.MustHumanToBase: " + err.Error())
	}
	return n
}

// MustHex panics if hex decoding fails. Useful in tests and constants where a
// fixed hex literal should never be invalid; not recommended in request paths
// — use hex.DecodeString and handle the error.
func MustHex(s string) []byte {
	if len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X") {
		s = s[2:]
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("cryptochief.MustHex: " + err.Error())
	}
	return b
}
