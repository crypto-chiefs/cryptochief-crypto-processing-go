// Package cryptochief is the official Go SDK for the Crypto Chief crypto
// processing API: accept crypto payments, send crypto payouts, sign on-chain
// transactions, manage wallets, and verify webhooks across Ethereum, Tron,
// TON, Solana, Bitcoin and 20+ more blockchains.
//
// SDK guide: https://docs-sdk.crypto-chief.com/processing/go
//
// REST API reference: https://docs-processing.crypto-chief.com
//
// # Quick start
//
//	c, err := cryptochief.New("MERCHANT_ID", "API_KEY")
//	if err != nil { /* ... */ }
//
//	est, err := c.Payouts.Estimate(ctx, &cryptochief.EstimatePayoutRequest{
//	    Network:   cryptochief.ChainEthSepolia,
//	    Coin:      "ETH",
//	    Amount:    "0.0001",
//	    ToAddress: "0xRecipient...",
//	})
//
// # Authentication
//
// Every request carries these headers:
//
//	Merchant:       <merchant ID>
//	X-CC-Timestamp: <Unix seconds>
//	X-CC-Nonce:     <32 hex, random>
//	X-CC-Signature: v1=hex(hmac_sha256(API_KEY, string_to_sign))
//
// string_to_sign is described at [StringToSignHMACv1]; it covers the SHA-256
// of the body bytes sent. The body is the encoding/json output of the request
// struct. Timestamp, nonce and signature are computed on every attempt.
// [WithIdempotencyKey] adds a signed Idempotency-Key header to the calls made
// with the context it returns.
//
// [Client.Request] sends a signed request to a route the SDK has no method
// for, on any Crypto Chief API that takes the same credentials. It takes the
// HTTP method: the energy API answers GET /v1/balance and
// GET /v1/orders/{key} with them.
//
// # Domain services
//
// Methods are grouped on the [Client] by domain:
//
//   - c.Payouts        — single and batch payouts, including auto-convert swaps
//   - c.Transactions   — two-phase sign / execute for arbitrary merchant-owned txs,
//     plus one-call helpers for EVM/TRON contract calls, Solana Anchor
//     programs, and TON Jetton / NFT / comment transfers
//   - c.PayIns         — accept incoming payments
//   - c.Wallets        — generate (optionally labelled), list, info, freeze,
//     re-bind to another master, rename, set/clear a static wallet's deposit
//     webhook, the pay-ins one deposit address received; decrypt RSA-encrypted
//     private keys
//   - c.Sweeps         — force transit→master sweep, filterable history,
//     per-wallet auto-sweep policy
//   - c.Withdrawals    — read-only history / info for treasury withdrawals
//   - c.StaticDeposits — history / info for static-address deposits
//   - c.Blockchain     — supported chains, the platform's asset catalogue and
//     the project's enabled assets, on-chain balance, tx status
//   - c.Currencies     — fiat ↔ crypto rate calculator, and the fiat codes /
//     crypto tickers there are rates for (rate availability, not the asset
//     catalogue — that is c.Blockchain)
//   - c.Credits        — billing credits balance + gas-ops gate (free endpoint)
//   - c.Energy         — TRON energy rental: quote, rent, order status
//   - c.Native         — native-coin purchase out of the platform's liquidity:
//     quote, buy, order status
//
// # Contract calls without hand-encoded calldata
//
// EVM and TRON contracts are called by Solidity-style signature:
//
//	c.Transactions.SignEVMCall(ctx, &cryptochief.EVMCallRequest{
//	    Network: cryptochief.ChainEthMainnet, FromAddress: "0x…",
//	    Contract: "0x…", Method: "transfer(address,uint256)",
//	    Args:     []any{"0xRecipient…", amount},
//	})
//
// Solana Anchor programs are called by method name + Borsh-typed args:
//
//	c.Transactions.SignAnchorCall(ctx, &cryptochief.AnchorCallRequest{
//	    Network: cryptochief.ChainSolanaMainnet, FromAddress: "…",
//	    Program: "…", Method: "initialize",
//	    Args:    []cryptochief.BorshValue{cryptochief.BorshU64(1_000_000)},
//	    Accounts: []cryptochief.SolanaAccount{ /* … */ },
//	})
//
// TON Jetton transfers take the master + recipient + amount; the
// sender's Jetton wallet and the gas budget are resolved automatically:
//
//	c.Transactions.JettonTransfer(ctx, &cryptochief.JettonTransferRequest{
//	    Network: cryptochief.ChainTONMainnet, FromAddress: "EQ…",
//	    JettonMaster: "EQ…", Recipient: "EQ…",
//	    Amount: amount, Memo: "Order #4242",
//	})
//
// # Generated wallets and RSA decryption
//
// Wallet generation returns the private key encrypted with the RSA public
// key uploaded to your project. Configure the corresponding private key
// at init to decrypt it locally:
//
//	c, _ := cryptochief.New(merchant, apiKey,
//	    cryptochief.WithRSAPrivateKey("./rsa_private.pem"),
//	)
//	w, _   := c.Wallets.Generate(ctx, &cryptochief.GenerateWalletRequest{ /* … */ })
//	hex, _ := c.Wallets.DecryptPrivateKey(w.PrivateKeyEncrypted)
//
// Scheme: RSA-OAEP / SHA-256 over base64-encoded ciphertext. PKCS#1 and
// PKCS#8 PEM keys are both accepted. Without the option the rest of the
// SDK works untouched; only [WalletsService.DecryptPrivateKey] requires
// it.
//
// # Polling
//
// [WaitForPayout], [WaitForTransaction], [WaitForPayIn] block until a
// record reaches a terminal state (or the context / [PollOptions]
// timeout elapses). The last observed state is always returned, so
// callers can decide whether to retry.
//
// # Webhooks
//
// Webhooks carry X-Webhook-Delivery, X-CC-Timestamp and X-CC-Signature:
//
//	X-CC-Signature: v1=hex(hmac_sha256(API_KEY, string_to_sign))
//
// string_to_sign is described at [WebhookV1StringToSign]. Verify the raw body
// and headers with [VerifyWebhook], or wrap a typed handler with
// [WebhookHandler]:
//
//	mux.Handle("/webhook/payout", cryptochief.WebhookHandler[cryptochief.PayoutWebhookEvent](
//	    apiKey,
//	    func(w http.ResponseWriter, r *http.Request, evt cryptochief.PayoutWebhookEvent) {
//	        /* … */
//	    }))
//
// # Errors
//
// API errors arrive as [*APIError] with a stable Code field. Compare
// with the Code* constants or [errors.Is] against the Err* sentinels.
//
// # Amounts
//
// All on-chain amounts are passed as decimal strings (e.g. "0.0001") or,
// for base units (wei / satoshi / lamports / nanoTON), as decimal integer
// strings. Use [HumanToBase] / [BaseToHuman] to convert with arbitrary
// precision via math/big — never use float64 for crypto amounts.
package cryptochief

// Version is the library version reported in the User-Agent header.
const Version = "0.12.0"
