// trc20_transfer shows a TRC-20 token transfer on TRON. Same convenience
// SignEVMCall flow as EVM — TRON shares the ABI encoding, so the SDK accepts
// TRON base58 addresses transparently.
//
// Run this from the examples/ directory — it is a separate Go module:
//
//	MERCHANT_ID=... API_KEY=... \
//	FROM_ADDRESS=TYourWallet... \
//	TOKEN=TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t  /* USDT TRC-20 */ \
//	RECIPIENT=TRecipient... \
//	go run ./trc20_transfer
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/crypto-chiefs/cryptochief-crypto-processing-go"
)

func main() {
	c, err := cryptochief.New(mustEnv("MERCHANT_ID"), mustEnv("API_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// USDT TRC-20 has 6 decimals — convert from human form correctly.
	amount, err := cryptochief.HumanToBase("12.5", 6)
	if err != nil {
		log.Fatal(err)
	}

	signed, err := c.Transactions.ERC20Transfer(ctx, &cryptochief.ERC20TransferRequest{
		Network:       cryptochief.ChainTronMainnet,
		FromAddress:   mustEnv("FROM_ADDRESS"),
		TokenContract: mustEnv("TOKEN"),
		Recipient:     mustEnv("RECIPIENT"),
		Amount:        amount,
		URLCallback:   "https://example.com/webhooks/transaction",
	})
	if err != nil {
		log.Fatalf("sign: %v", err)
	}
	fmt.Printf("signed: uuid=%s tx_hash=%s\n", signed.UUID, signed.TxHash)

	if os.Getenv("BROADCAST") == "" {
		fmt.Println("set BROADCAST=1 to actually send")
		return
	}
	if _, err := c.Transactions.Execute(ctx, &cryptochief.ExecuteTransactionRequest{
		UUID: signed.UUID,
	}); err != nil {
		log.Fatalf("execute: %v", err)
	}
	final, err := cryptochief.WaitForTransaction(ctx, c, signed.UUID, cryptochief.PollOptions{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("terminal: status=%s tx=%s\n", final.Status, final.TxHash)
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("set %s in env", name)
	}
	return v
}
