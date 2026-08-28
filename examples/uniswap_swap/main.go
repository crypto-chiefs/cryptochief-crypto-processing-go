// uniswap_swap shows a real Uniswap V2 token-for-token swap on Ethereum.
// The SDK encodes the calldata from a Solidity-style signature — you never
// touch the ABI yourself.
//
// Run this from the examples/ directory — it is a separate Go module:
//
//	MERCHANT_ID=... API_KEY=... \
//	FROM_ADDRESS=0xYourWallet \
//	TOKEN_IN=0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2  /* WETH */ \
//	TOKEN_OUT=0xdAC17F958D2ee523a2206206994597C13D831ec7 /* USDT */ \
//	ROUTER=0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D    /* Uniswap V2 */ \
//	go run ./uniswap_swap
//
// Set BROADCAST=1 to actually send (default = sign only, signature expires
// by TTL). Set NETWORK=ETH_SEPOLIA + matching addresses to test on Sepolia.
package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/crypto-chiefs/cryptochief-crypto-processing-go"
)

func main() {
	c, err := cryptochief.New(mustEnv("MERCHANT_ID"), mustEnv("API_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	from := mustEnv("FROM_ADDRESS")
	router := envOr("ROUTER", "0x7a250d5630B4cF539739dF2C5dAcb4c659F2488D")
	tokenIn := mustEnv("TOKEN_IN")
	tokenOut := mustEnv("TOKEN_OUT")
	network := cryptochief.Chain(envOr("NETWORK", string(cryptochief.ChainEthMainnet)))

	broadcast := os.Getenv("BROADCAST") != ""

	// Swap 0.01 of the source token. The amount needs to be in TOKEN_IN's
	// base units — most ERC-20s are 18 decimals; USDT is 6. Adjust to your token.
	amountIn, _ := cryptochief.HumanToBase("0.01", 18)
	deadline := big.NewInt(time.Now().Add(10 * time.Minute).Unix())
	path := []string{tokenIn, tokenOut}

	// The slippage floor, in TOKEN_OUT base units. Zero accepts whatever the
	// pool gives back, which on a public mempool is an invitation to be
	// sandwiched — so broadcasting on mainnet without one is refused rather
	// than quietly demonstrated.
	amountOutMin := big.NewInt(0)
	if v := os.Getenv("MIN_OUT"); v != "" {
		parsed, ok := new(big.Int).SetString(v, 10)
		if !ok {
			log.Fatalf("MIN_OUT=%q is not an integer in TOKEN_OUT base units", v)
		}
		amountOutMin = parsed
	} else if broadcast && network == cryptochief.ChainEthMainnet {
		log.Fatal("set MIN_OUT (TOKEN_OUT base units) before broadcasting a mainnet swap")
	}

	// Uniswap's router moves TOKEN_IN with transferFrom, so it needs an
	// allowance on TOKEN_IN before the swap will do anything but revert. This
	// is a plain ERC-20 approve, encoded from its signature like any other call.
	approve, err := c.Transactions.SignEVMCall(ctx, &cryptochief.EVMCallRequest{
		Network:     network,
		FromAddress: from,
		Contract:    tokenIn,
		Method:      "approve(address,uint256)",
		Args:        []any{router, amountIn},
		URLCallback: "https://example.com/webhooks/transaction",
	})
	if err != nil {
		log.Fatalf("sign approve: %v", err)
	}
	fmt.Printf("approve signed: uuid=%s tx_hash=%s\n", approve.UUID, approve.TxHash)

	if broadcast {
		if _, err := c.Transactions.Execute(ctx, &cryptochief.ExecuteTransactionRequest{
			UUID: approve.UUID,
		}); err != nil {
			log.Fatalf("execute approve: %v", err)
		}
		// The allowance has to be on chain before the swap is broadcast, or the
		// swap lands first and reverts.
		approved, err := cryptochief.WaitForTransaction(ctx, c, approve.UUID, cryptochief.PollOptions{
			Interval: 5 * time.Second,
			Timeout:  8 * time.Minute,
		})
		if err != nil {
			status := "unknown"
			if approved != nil {
				status = approved.Status
			}
			log.Fatalf("wait approve: last status=%s err=%v", status, err)
		}
		if approved.Status != cryptochief.TxStatusConfirmed {
			log.Fatalf("approve did not confirm: status=%s", approved.Status)
		}
		fmt.Printf("approved: tx=%s\n", approved.TxHash)
	}

	// One-shot: encode and sign in a single call.
	signed, err := c.Transactions.SignEVMCall(ctx, &cryptochief.EVMCallRequest{
		Network:     network,
		FromAddress: from,
		Contract:    router,
		Method:      "swapExactTokensForTokens(uint256,uint256,address[],address,uint256)",
		Args:        []any{amountIn, amountOutMin, path, from, deadline},
		URLCallback: "https://example.com/webhooks/transaction",
	})
	if err != nil {
		log.Fatalf("sign: %v", err)
	}
	fmt.Printf("signed: uuid=%s tx_hash=%s expires_at=%s\n",
		signed.UUID, signed.TxHash, signed.ExpiresAt)

	if !broadcast {
		fmt.Println("set BROADCAST=1 to actually swap (approve included)")
		return
	}

	if _, err := c.Transactions.Execute(ctx, &cryptochief.ExecuteTransactionRequest{
		UUID: signed.UUID,
	}); err != nil {
		log.Fatalf("execute: %v", err)
	}
	final, err := cryptochief.WaitForTransaction(ctx, c, signed.UUID, cryptochief.PollOptions{
		Interval: 5 * time.Second,
		Timeout:  8 * time.Minute,
	})
	if err != nil {
		// The helper returns a nil snapshot when no poll ever succeeded, so
		// there is not always a last status to report.
		status := "unknown"
		if final != nil {
			status = final.Status
		}
		log.Fatalf("wait: last status=%s err=%v", status, err)
	}
	fmt.Printf("terminal: status=%s tx=%s fee=$%s\n",
		final.Status, final.TxHash, final.ActualFeeFiat)
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("set %s in env", name)
	}
	return v
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
