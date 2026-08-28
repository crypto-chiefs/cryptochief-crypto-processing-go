// payout shows the end-to-end single payout flow: estimate → execute → wait
// for terminal status.
//
// Set DRY_RUN=1 to stop after Estimate without actually moving funds — the
// default is to broadcast.
//
// Run this from the examples/ directory — it is a separate Go module:
//
//	MERCHANT_ID=... API_KEY=... TO_ADDRESS=0x... go run ./payout
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

	req := &cryptochief.EstimatePayoutRequest{
		Network:   cryptochief.ChainEthSepolia,
		Coin:      "ETH",
		Amount:    "0.0001",
		ToAddress: mustEnv("TO_ADDRESS"),
	}

	est, err := c.Payouts.Estimate(ctx, req)
	if err != nil {
		log.Fatalf("estimate: %v", err)
	}
	fmt.Printf("estimate: to_receive=%s sources=%d fee≈$%s\n",
		est.AmountToReceive, len(est.Sources),
		fee(est.FeeInfo))

	if os.Getenv("DRY_RUN") != "" {
		fmt.Println("DRY_RUN=1 → stopping after estimate")
		return
	}

	orderID := fmt.Sprintf("demo-%d", time.Now().UnixNano())
	exec, err := c.Payouts.Execute(ctx, &cryptochief.ExecutePayoutRequest{
		OrderID:     orderID,
		UserID:      "demo-user",
		Network:     req.Network,
		Coin:        req.Coin,
		Amount:      req.Amount,
		ToAddress:   req.ToAddress,
		URLCallback: "https://example.com/webhook",
	})
	if err != nil {
		log.Fatalf("execute: %v", err)
	}
	fmt.Printf("queued: uuid=%s order_id=%s\n", exec.UUID, exec.OrderID)

	final, err := cryptochief.WaitForPayout(ctx, c, exec.UUID, cryptochief.PollOptions{
		Interval: 4 * time.Second,
		Timeout:  4 * time.Minute,
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
	fmt.Printf("terminal: status=%s txid=%s\n", final.Status, final.TxID)
}

func fee(f *cryptochief.PayoutFeeInfo) string {
	if f == nil {
		return "?"
	}
	return f.EstimatedFiat
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("set %s in env", name)
	}
	return v
}
