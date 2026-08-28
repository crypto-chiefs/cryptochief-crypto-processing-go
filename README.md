# Crypto Chief Go SDK — Crypto Processing API Client

[![Go Reference](https://pkg.go.dev/badge/github.com/crypto-chiefs/cryptochief-crypto-processing-go.svg)](https://pkg.go.dev/github.com/crypto-chiefs/cryptochief-crypto-processing-go)
[![SDK Docs](https://img.shields.io/badge/docs-SDK%20guide-2ea44f)](https://docs-sdk.crypto-chief.com/processing/go)
[![CI](https://github.com/crypto-chiefs/cryptochief-crypto-processing-go/actions/workflows/ci.yml/badge.svg)](https://github.com/crypto-chiefs/cryptochief-crypto-processing-go/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/crypto-chiefs/cryptochief-crypto-processing-go)](https://goreportcard.com/report/github.com/crypto-chiefs/cryptochief-crypto-processing-go)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Crypto Chief Go SDK** is the official Go client library for the
[Crypto Chief](https://crypto-chief.com/processing/) **crypto processing API** — a unified
crypto payment gateway for accepting crypto payments, sending crypto payouts
(single and mass), signing on-chain transactions, managing wallets, and
verifying webhooks across **Ethereum, Tron, TON, Solana, Bitcoin and 20+ more
blockchains**.

Drop it into any Go backend to add cryptocurrency payment processing —
stablecoin (USDT / USDC) payouts, pay-ins, swaps, and smart-contract calls —
with typed requests, arbitrary-precision amounts, and `errors.Is`-friendly
error codes.

- One-line setup; reusable, goroutine-safe `*Client`.
- Typed request/response structs for every documented endpoint.
- **Contract calls without hand-encoded calldata** — Solidity ABI for EVM
  and TRON, Anchor + Borsh for Solana, Jetton / NFT / comment helpers for TON.
- **Local RSA decryption** of generated wallet private keys (opt-in).
- Stable error codes, `errors.Is`-friendly sentinels, automatic retry on
  transient failures.
- Arbitrary-precision amounts via `math/big` — no `float64`, ever.
- Webhook verification + generic typed `http.Handler` middleware.
- Polling helpers that block until a payout / transaction / pay-in is terminal.
- Requires Go 1.20+.

## Install

```bash
go get github.com/crypto-chiefs/cryptochief-crypto-processing-go@latest
```

## Quick start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/crypto-chiefs/cryptochief-crypto-processing-go"
)

func main() {
    c, err := cryptochief.New("MERCHANT_ID", "API_KEY")
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()
    est, err := c.Payouts.Estimate(ctx, &cryptochief.EstimatePayoutRequest{
        Network:   cryptochief.ChainEthSepolia,
        Coin:      "ETH",
        Amount:    "0.0001",
        ToAddress: "0xRecipient...",
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("amount to receive:", est.AmountToReceive)
}
```

Both credentials come from the dashboard → Integration tab. The API key
is the **signing secret** — keep it server-side.

## What you can do with it

| Domain | Service | Key methods |
|---|---|---|
| Single payout (incl. auto-convert swap) | `c.Payouts` | `Estimate`, `Execute`, `Info`, `History` |
| Mass payout (up to 50 items) | `c.Payouts` | `BatchEstimate`, `BatchExecute` |
| Two-phase sign / broadcast for arbitrary txs | `c.Transactions` | `Sign`, `Execute`, `Info`, `History` |
| EVM / TRON contract calls (incl. ERC-20 / TRC-20) | `c.Transactions` | `SignEVMCall`, `SignTronCall`, `ERC20Transfer` |
| Solana programs | `c.Transactions` | `SignAnchorCall`, `SignSolanaCall` |
| TON contract calls (Jetton / NFT / text) | `c.Transactions` | `JettonTransfer`, `NFTTransfer`, `SendTONComment`, `SignTONCall` |
| Accept incoming payments | `c.PayIns` | `Create`, `SelectAsset`, `ResetAsset`, `Cancel`, `Info`, `History` |
| Wallet management + RSA decrypt | `c.Wallets` | `Generate`, `List`, `Info`, `Freeze`, `DecryptPrivateKey` |
| Treasury sweeps | `c.Sweeps` | `Force`, `History`, `WalletHistory`, `Settings`, `UpdateSettings` |
| Withdrawals (read-only) | `c.Withdrawals` | `Info`, `History` |
| Static-deposit history | `c.StaticDeposits` | `Info`, `History` |
| On-chain queries | `c.Blockchain` | `ContractsAvailable`, `WalletBalance`, `TransactionStatus` |
| Fiat ↔ crypto rate quote | `c.Currencies` | `FiatToCrypto`, `CryptoToFiat` |
| Billing credits (free endpoints) | `c.Credits` | `Balance`, `Topup` |

## End-to-end example: payout with confirmation

```go
exec, err := c.Payouts.Execute(ctx, &cryptochief.ExecutePayoutRequest{
    OrderID:     "order-42",        // idempotency key — safe to retry
    UserID:      "u-7",
    Network:     cryptochief.ChainEthSepolia,
    Coin:        "ETH",
    Amount:      "0.0001",
    ToAddress:   "0xRecipient...",
    URLCallback: "https://your.app/webhooks/payout",
})
if err != nil {
    var apiErr *cryptochief.APIError
    if errors.As(err, &apiErr) && apiErr.Code == cryptochief.CodeInsufficientFunds {
        // top up and try again
    }
    return err
}

final, err := cryptochief.WaitForPayout(ctx, c, exec.UUID, cryptochief.PollOptions{
    Interval: 5 * time.Second,
    Timeout:  5 * time.Minute,
})
if err != nil {
    return err
}
if final.Succeeded() {
    log.Printf("paid: tx=%s", final.TxID)
}
```

## Two-phase sign + execute

`Transactions.Sign` builds and cryptographically signs a transaction
**without broadcasting**. The TTL of the signed reservation varies by
chain (EVM 10m, UTXO 15m, TRON 45s, Solana 60s, XRP 90s, TON 300s) — call
`Execute` before it expires.

```go
wei, _ := cryptochief.HumanToBase("0.0001", 18)

signed, err := c.Transactions.Sign(ctx, &cryptochief.SignTransactionRequest{
    Network:     cryptochief.ChainEthSepolia,
    FromAddress: "0xYourWallet...",
    Type:        cryptochief.TxTypeNative,
    ToAddress:   "0xRecipient...",
    Value:       wei.String(), // base units (wei)
    URLCallback: "https://your.app/webhooks/transaction",
})

_, err = c.Transactions.Execute(ctx, &cryptochief.ExecuteTransactionRequest{
    UUID: signed.UUID,
})
```

## Contract calls — the easy way

Most real-world transactions are smart-contract calls (token transfers,
DEX swaps, Anchor program instructions, Jetton transfers). You **never**
have to encode the `data` field by hand: give the library a typed
description, get back a signed reservation.

### EVM — Uniswap V2 swap

```go
amountIn,   _ := cryptochief.HumanToBase("0.01", 18)
amountOutMin := big.NewInt(0)
deadline    := big.NewInt(time.Now().Add(10 * time.Minute).Unix())
path        := []string{tokenIn, tokenOut}

signed, _ := c.Transactions.SignEVMCall(ctx, &cryptochief.EVMCallRequest{
    Network:     cryptochief.ChainEthMainnet,
    FromAddress: "0xYourWallet…",
    Contract:    "0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D", // V2 router
    Method:      "swapExactTokensForTokens(uint256,uint256,address[],address,uint256)",
    Args:        []any{amountIn, amountOutMin, path, "0xYourWallet…", deadline},
    URLCallback: "https://your.app/webhooks/transaction",
})
```

The encoder supports `uint/int<M>`, `address`, `bool`, `bytes`,
`bytes<N>`, `string`, and fixed / dynamic arrays of any of those.
Argument values accept `*big.Int`, plain Go ints/uints, decimal / hex
strings, `[]byte`, and slices of those. Function-name aliases
(`uint` → `uint256`) and parameter names (`uint256 amount`) are
normalised before hashing.

ERC-20 / TRC-20 transfers have a one-liner:

```go
amount, _ := cryptochief.HumanToBase("12.5", 6) // USDT decimals = 6

c.Transactions.ERC20Transfer(ctx, &cryptochief.ERC20TransferRequest{
    Network:       cryptochief.ChainEthMainnet,
    FromAddress:   "0xYourWallet…",
    TokenContract: "0xdAC17F958D2ee523a2206206994597C13D831ec7",
    Recipient:     "0x…",
    Amount:        amount,
})
```

### TRON — same encoder, base58 addresses

TRON shares the EVM ABI. `SignEVMCall` (or its alias `SignTronCall`)
accepts both base58 (`T…`) and 0x41-prefixed hex addresses transparently:

```go
c.Transactions.SignTronCall(ctx, &cryptochief.EVMCallRequest{
    Network:     cryptochief.ChainTronMainnet,
    FromAddress: "TYourWallet…",
    Contract:    "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", // USDT TRC-20 base58
    Method:      "transfer(address,uint256)",
    Args:        []any{"TRecipient…", amount},
})
```

Need to convert addresses outside a call? `cryptochief.TronToHex` /
`cryptochief.HexToTron` are public.

### Solana — Anchor program call

Anchor programs use an 8-byte SHA-256 discriminator (`global:<method>`)
followed by Borsh-encoded arguments. The SDK builds both:

```go
signed, _ := c.Transactions.SignAnchorCall(ctx, &cryptochief.AnchorCallRequest{
    Network:     cryptochief.ChainSolanaMainnet,
    FromAddress: "YourWallet…",
    Program:     "YourProgramId…",
    Method:      "initialize",
    Args: []cryptochief.BorshValue{
        cryptochief.BorshU64(1_000_000),
        cryptochief.BorshString("hello"),
        cryptochief.BorshPubkey("Recipient…"),
    },
    Accounts: []cryptochief.SolanaAccount{
        {Pubkey: "YourWallet…", IsSigner: true,  IsWritable: true},
        {Pubkey: "DataAcct…",   IsSigner: false, IsWritable: true},
        {Pubkey: "11111111111111111111111111111111", IsSigner: false, IsWritable: false},
    },
})
```

Borsh primitives: `BorshU8/16/32/64/128`, `BorshI8/16/32/64`,
`BorshBool`, `BorshString`, `BorshBytes`, `BorshFixedBytes`,
`BorshPubkey`, `BorshOption`, `BorshVec`, `BorshStruct`.

Non-Anchor program? Pass pre-built instruction bytes with
`SignSolanaCall(&cryptochief.SolanaCallRequest{InstructionData: …, Accounts: …})`.

### TON — Jetton / NFT / comment in one call

TON contract bodies are program-specific cells with no Solidity-style
ABI, so the SDK encodes them for you behind high-level helpers. You
describe the operation in human terms.

```go
amount, _ := cryptochief.HumanToBase("0.5", 6) // USDT Jetton has 6 decimals

signed, _ := c.Transactions.JettonTransfer(ctx, &cryptochief.JettonTransferRequest{
    Network:      cryptochief.ChainTONMainnet,
    FromAddress:  "EQYourWallet…",
    JettonMaster: "EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs", // USDT
    Recipient:    "EQRecipient…",
    Amount:       amount,
    Memo:         "Order #4242", // optional — wallets show this as the comment
    // AttachedTON empty → SDK picks 0.07 TON if the receiver already
    // has a Jetton wallet for this token, 0.15 TON if a new one must
    // be deployed.
    // ForwardTONAmount empty → 1 nanoTON when Memo is set (delivers
    // the comment), 0 otherwise.
})
```

The sender's Jetton wallet address and the gas budget are resolved
automatically — no extra configuration. If you've already pre-resolved
them, pass `JettonWalletAddress` and `AttachedTON` explicitly and no
network lookup happens.

NFT transfer and text comments use the same pattern:

```go
c.Transactions.NFTTransfer(ctx, &cryptochief.NFTTransferRequest{
    Network:     cryptochief.ChainTONMainnet,
    FromAddress: "EQYourWallet…",
    NFTItem:     "EQItemAddr…",
    NewOwner:    "EQRecipient…",
    AttachedTON: cryptochief.NanoTON("0.05"),
})

c.Transactions.SendTONComment(ctx, &cryptochief.TONCommentRequest{
    Network:     cryptochief.ChainTONMainnet,
    FromAddress: "EQYourWallet…",
    Recipient:   "EQRecipient…",
    Text:        "Thanks for the coffee!",
    AmountTON:   cryptochief.NanoTON("1"),
})
```

For non-Jetton / non-NFT contracts, build the body cell yourself with a
TON library and pass the bytes to the lower-level `SignTONCall`:

```go
signed, _ := c.Transactions.SignTONCall(ctx, &cryptochief.TONCallRequest{
    Network: cryptochief.ChainTONMainnet, FromAddress: "EQ…",
    Contract: "EQ…", Value: "50000000", BodyCell: bocBytes,
})
```

`ParseTONAddress` is provided for offline address validation /
round-tripping the `EQ…` / `UQ…` / `workchain:hex` forms.

## Wallets and RSA-encrypted private keys

When the API generates a wallet it returns the private key encrypted
with the RSA public key you uploaded in the dashboard
(Project Settings → RSA Key). The SDK can decrypt it locally:

```bash
# one-time setup: generate a keypair and upload rsa_public.pem to the dashboard
openssl genrsa -out rsa_private.pem 2048
openssl rsa -in rsa_private.pem -pubout -out rsa_public.pem
```

```go
c, _ := cryptochief.New(merchant, apiKey,
    cryptochief.WithRSAPrivateKey("./rsa_private.pem"),
    // Or load from memory:
    // cryptochief.WithRSAPrivateKeyPEM(pemBytes),
)

w, _ := c.Wallets.Generate(ctx, &cryptochief.GenerateWalletRequest{
    WalletType:  cryptochief.WalletTypeMaster,
    ChainFamily: cryptochief.FamilyEVM,
})

// w.PrivateKeyEncrypted is base64-encoded RSA-OAEP / SHA-256 ciphertext.
privHex, err := c.Wallets.DecryptPrivateKey(w.PrivateKeyEncrypted)
// privHex is the chain-native hex form — keep it safe.
```

`WithRSAPrivateKey` accepts both PKCS#1 (`openssl genrsa` default) and
PKCS#8 (`-----BEGIN PRIVATE KEY-----`) PEM formats. A malformed PEM
surfaces on the first `DecryptPrivateKey` call rather than at `New`, so
you can load credentials lazily without a startup panic.

If you skip the option, `Wallets.DecryptPrivateKey` returns
`ErrRSAKeyNotConfigured` and the rest of the SDK continues to work —
decryption is purely opt-in.

## Webhooks

Outbound webhooks are signed with the same algorithm used for outgoing
requests. The library ships both a primitive checker and a generic
typed handler:

```go
mux.Handle("/webhook/payout", cryptochief.WebhookHandler[cryptochief.PayoutWebhookEvent](
    apiKey,
    func(w http.ResponseWriter, r *http.Request, evt cryptochief.PayoutWebhookEvent) {
        log.Printf("payout %s → %s (tx=%s)", evt.UUID, evt.Status, evt.TxID)
    },
))
```

For a custom HTTP stack:

```go
body, _ := io.ReadAll(r.Body)
if err := cryptochief.VerifyWebhookSignature(apiKey, body, r.Header.Get("Signature")); err != nil {
    http.Error(w, "bad signature", http.StatusUnauthorized)
    return
}
```

`cryptochief.WebhookSenderIPs` lists the addresses webhooks are delivered
from — whitelist them at your edge for defence in depth.

Typed event payloads: `PayoutWebhookEvent`, `TransactionWebhookEvent`,
`PayInWebhookEvent`, `StaticDepositWebhookEvent`.

## Error handling

Errors from the API are `*APIError` with a stable `Code` field:

```go
_, err := c.Payouts.Execute(ctx, req)
var apiErr *cryptochief.APIError
if errors.As(err, &apiErr) {
    switch apiErr.Code {
    case cryptochief.CodeInsufficientFunds:        // need top-up
    case cryptochief.CodeAssetNotEnabled:          // unsupported coin/network
    case cryptochief.CodeDebtLimitExceeded:        // postpaid debt cap hit
    case cryptochief.CodeFromWalletNotOwned:       // wallet doesn't belong to this project
    case cryptochief.CodeAlreadyExecuted:          // duplicate execute
    case cryptochief.CodeBatchDuplicateOrderID:    // batch validation
    default:                                       // anything else
    }
}
```

`errors.Is(err, cryptochief.ErrInsufficientFunds)` works too — the package
exposes sentinel `*APIError` values for the same codes.

## Amounts

**Never use `float64` for crypto amounts.** Use `HumanToBase` / `BaseToHuman`:

```go
wei, _ := cryptochief.HumanToBase("1.5", 18)
// wei is *big.Int = 1500000000000000000

human := cryptochief.BaseToHuman(wei, 18)
// human = "1.5"
```

The API accepts both human strings (the `amount` field on most
endpoints) and base-unit integer strings (the `value` field on
`/transaction/signature`). `HumanToBase` is precise to the last digit
via `math/big`; sub-base-unit precision is truncated to match every
blockchain client's behaviour.

For TON specifically, `cryptochief.NanoTON("0.05")` returns the
nanoTON decimal string `AttachedTON` and friends expect.

## Configuration

```go
c, err := cryptochief.New("MERCHANT_ID", "API_KEY",
    cryptochief.WithBaseURL("https://api-processing.crypto-chief.com"), // default
    cryptochief.WithHTTPClient(&http.Client{Timeout: 60 * time.Second}),
    cryptochief.WithRetries(3),                          // 5xx + transport
    cryptochief.WithRetryBackoff(200*time.Millisecond, 5*time.Second),
    cryptochief.WithUserAgent("my-service/1.0"),
    cryptochief.WithLogger(slog.Default()),
    cryptochief.WithRSAPrivateKey("./rsa_private.pem"),  // optional — wallet decryption
)
```

**Test mode** is a per-project toggle in the dashboard, not a separate
base URL — point a test-mode project's credentials at the same client.

## Idempotency

`Payouts.Execute` and `Payouts.BatchExecute` are idempotent on `OrderID`:
re-submitting the same `order_id` returns the same `uuid` rather than
creating a second payout. The library's automatic retry on 5xx relies on
this — your callers don't need any extra ceremony.

## Runnable examples

The [`examples/`](./examples) directory has runnable programs you can
copy from:

- [`quickstart`](./examples/quickstart) — list enabled assets, read a wallet balance.
- [`payout`](./examples/payout) — single payout end-to-end.
- [`batch_payout`](./examples/batch_payout) — mass payout with concurrent per-item polling.
- [`sign_execute`](./examples/sign_execute) — two-phase tx sign + broadcast.
- [`uniswap_swap`](./examples/uniswap_swap) — Uniswap V2 swap via one-line ABI encoding.
- [`trc20_transfer`](./examples/trc20_transfer) — TRC-20 transfer with TRON base58 addresses.
- [`anchor_call`](./examples/anchor_call) — Solana Anchor program invocation.
- [`ton_jetton_transfer`](./examples/ton_jetton_transfer) — TON Jetton transfer with auto-resolved Jetton wallet.
- [`wallet_generate`](./examples/wallet_generate) — generate a project wallet and decrypt its private key with your RSA key.
- [`webhook_server`](./examples/webhook_server) — HTTP server that verifies signatures, logs the lifecycle of each event type, and prints copy-pasteable "next action" hints with TODO stubs for your business logic.

```bash
cd examples
MERCHANT_ID=... API_KEY=... TO_ADDRESS=0x... go run ./payout
```

## FAQ — common crypto-processing tasks in Go

**How do I accept a crypto payment in Go?**
Create a pay-in (invoice) with `c.PayIns.Create(...)`; the customer gets a
deposit address and you receive a signed webhook when it's paid. See the
`PayIns` service.

**How do I send a crypto payout (withdrawal) in Go?**
`c.Payouts.Execute(...)` with `Coin` / `Network` / `Amount` / `ToAddress`. Pass
`OrderID` as an idempotency key and use `WaitForPayout` to block until it's
confirmed. Works for native coins and ERC-20 / TRC-20 stablecoins (USDT, USDC).

**How do I send a mass / batch crypto payout?**
`c.Payouts.BatchExecute(...)` — up to 50 recipients in one signed request,
processed sequentially so the double-spend invariant holds.

**How do I call a smart contract (ERC-20, Uniswap) without encoding calldata?**
`c.Transactions.SignEVMCall(...)`, or `ERC20Transfer` for a token-transfer
one-liner. Give it a Solidity signature plus args and the SDK ABI-encodes the
`data` field for you.

**How do I transfer USDT on TON (a Jetton) in Go?**
`c.Transactions.JettonTransfer(...)` — pass the Jetton master, recipient, and
amount; the sender's Jetton wallet address and gas budget are resolved
automatically.

**How do I verify a Crypto Chief webhook signature?**
`cryptochief.VerifyWebhookSignature(apiKey, body, sig)`, or wrap a typed handler
with `cryptochief.WebhookHandler[...]`.

**Which blockchains does the crypto processing API support?**
Ethereum, BNB Smart Chain, Polygon, Tron, TON, Solana, Bitcoin, Litecoin,
Dogecoin, XRP and more — 25 chains in total. The constants live in `chains.go`.

**How do I control when a deposit wallet is swept?**
`c.Sweeps.Settings(...)` reads the policy in force for one wallet and
`c.Sweeps.UpdateSettings(...)` changes it — sweep on arrival (`SweepModeMomentum`),
sweep once the balance reaches an amount (`SweepModeThreshold` plus
`ThresholdUSD`), or never on its own (`SweepModeOff`, force still works). The
read comes back in three layers — what will happen, what this wallet overrides,
and what it inherits from the project — so you can tell a value of your own from
an inherited one:

```go
mode, threshold := cryptochief.SweepModeThreshold, "250"
s, err := c.Sweeps.UpdateSettings(ctx, cryptochief.SweepSettingsUpdate{
    Address:      depositAddress,
    TypeWork:     &mode,
    ThresholdUSD: &threshold,
})
// s.Effective is the resolved policy; s.Effective.Source says which layer it came from.
```

Inheritance is per field: overriding the mode leaves the fee mode inherited. To
stop overriding a field, name it in `Fields` and leave its value nil.

**How do I know a sweep actually settled?**
Check `Status`. `SweepStatusBroadcasted` means the transaction is out and not yet
confirmed; `SweepStatusCompleted` means confirmed, with `SweepConfirmations` and
`CompletedAt` filled in. Earlier platform versions reported `completed` at
broadcast, so a sweep could read completed while its transaction was still
unconfirmed.

**How do I keep test payments off real chains?**
Set `Environment` on `PayIns.Create` to `cryptochief.EnvironmentTestnet` or
`EnvironmentMainnet`. It constrains the asset the platform picks when you have
not named a concrete network — fiat mode and `ANY` — so an unconstrained pick
cannot put a real payment on a test chain. Omit it to use the project's default.

**How do I avoid floating-point rounding bugs with crypto amounts?**
Never use `float64`. Convert with `cryptochief.HumanToBase` / `BaseToHuman`,
which are backed by `math/big`.

## Documentation

**Full guides, tutorials, and recipes → [docs-sdk.crypto-chief.com/processing/go](https://docs-sdk.crypto-chief.com/processing/go)**

Reference material:

- API reference (auto-generated from source): [pkg.go.dev](https://pkg.go.dev/github.com/crypto-chiefs/cryptochief-crypto-processing-go)
- REST / HTTP API: [docs-processing.crypto-chief.com](https://docs-processing.crypto-chief.com)

## Contributing

PRs welcome. Please run `go test -race ./...` and `go vet ./...` before
opening; new endpoints should come with a test that exercises the wire
shape through `httptest`.

## License

MIT — see [LICENSE](LICENSE).
