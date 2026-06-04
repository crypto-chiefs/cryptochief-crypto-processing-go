// ton_jetton_transfer demonstrates the one-call Jetton transfer on TON.
//
// The SDK encodes the standard transfer body (op 0x0f8a7ea5), resolves
// the sender's Jetton wallet address, and sizes the gas budget based on
// whether the recipient already holds the Jetton — you only describe
// the transfer in human terms.
//
//	MERCHANT_ID=... API_KEY=... \
//	FROM_ADDRESS=EQYourWallet... \
//	JETTON_MASTER=EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs  /* USDT */ \
//	RECIPIENT=EQRecipient... \
//	go run ./examples/ton_jetton_transfer
//
// Set BROADCAST=1 to actually send (default = sign only, signature
// expires by TTL).
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

	// USDT Jetton has 6 decimals — convert from a human amount.
	amount, err := cryptochief.HumanToBase("0.5", 6)
	if err != nil {
		log.Fatal(err)
	}

	signed, err := c.Transactions.JettonTransfer(ctx, &cryptochief.JettonTransferRequest{
		Network:      cryptochief.ChainTONMainnet,
		FromAddress:  mustEnv("FROM_ADDRESS"),
		JettonMaster: mustEnv("JETTON_MASTER"),
		Recipient:    mustEnv("RECIPIENT"),
		Amount:       amount,
		// Memo, if set, gets attached as the transfer's forward_payload
		// (the "comment" wallets display). The SDK encodes it for you.
		Memo: envOr("MEMO", ""),
		// AttachedTON empty → SDK picks a sensible default based on
		// whether the recipient already has a Jetton wallet for this
		// token.
		URLCallback: "https://example.com/webhooks/transaction",
	})
	if err != nil {
		log.Fatalf("sign: %v", err)
	}
	fmt.Printf("signed: uuid=%s tx_hash=%s expires_at=%s\n",
		signed.UUID, signed.TxHash, signed.ExpiresAt)

	if os.Getenv("BROADCAST") == "" {
		fmt.Println("set BROADCAST=1 to actually send")
		return
	}
	if _, err := c.Transactions.Execute(ctx, &cryptochief.ExecuteTransactionRequest{
		UUID: signed.UUID,
	}); err != nil {
		log.Fatalf("execute: %v", err)
	}
	final, err := cryptochief.WaitForTransaction(ctx, c, signed.UUID, cryptochief.PollOptions{
		Interval: 5 * time.Second,
		Timeout:  4 * time.Minute,
	})
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

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
