// sign_execute demonstrates the two-phase transaction flow: build & sign a
// native transfer (no broadcast), then broadcast it by uuid. The Sign step
// is cheap and reversible (signature simply expires); only Execute moves
// funds on-chain.
//
// Run this from the examples/ directory — it is a separate Go module:
//
//	MERCHANT_ID=... API_KEY=... FROM_ADDRESS=0x... TO_ADDRESS=0x... go run ./sign_execute
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// "value" is in BASE units (wei). Convert from a human amount with
	// HumanToBase so we never touch float64.
	wei, err := cryptochief.HumanToBase("0.0001", 18)
	if err != nil {
		log.Fatal(err)
	}

	signed, err := c.Transactions.Sign(ctx, &cryptochief.SignTransactionRequest{
		Network:     cryptochief.ChainEthSepolia,
		FromAddress: mustEnv("FROM_ADDRESS"),
		Type:        cryptochief.TxTypeNative,
		ToAddress:   mustEnv("TO_ADDRESS"),
		Value:       wei.String(),
		URLCallback: "https://example.com/webhook",
	})
	if err != nil {
		log.Fatalf("sign: %v", err)
	}
	fmt.Printf("signed: uuid=%s tx_hash=%s expires_at=%s\n",
		signed.UUID, signed.TxHash, signed.ExpiresAt)

	if os.Getenv("BROADCAST") == "" {
		fmt.Println("set BROADCAST=1 to actually send (signature expires by TTL otherwise)")
		return
	}

	exec, err := c.Transactions.Execute(ctx, &cryptochief.ExecuteTransactionRequest{
		UUID: signed.UUID,
	})
	if err != nil {
		log.Fatalf("execute: %v", err)
	}
	fmt.Printf("broadcasted: status=%s tx_hash=%s\n", exec.Status, exec.TxHash)

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
	fmt.Printf("terminal: status=%s fee=%s ($%s)\n",
		final.Status, final.ActualFee, final.ActualFeeFiat)
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("set %s in env", name)
	}
	return v
}
