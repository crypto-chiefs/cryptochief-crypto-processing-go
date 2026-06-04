// batch_payout shows the mass-payout flow: up to 50 items in one call,
// per-item statuses, and polling each accepted item to terminal.
//
//	MERCHANT_ID=... API_KEY=... TO_ADDRESS=0x... go run ./examples/batch_payout
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
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

	to := mustEnv("TO_ADDRESS")
	mkItem := func(i int) cryptochief.ExecutePayoutRequest {
		return cryptochief.ExecutePayoutRequest{
			OrderID:     fmt.Sprintf("demo-batch-%d-%d", time.Now().UnixNano(), i),
			UserID:      "demo-user",
			Network:     cryptochief.ChainEthSepolia,
			Coin:        "ETH",
			Amount:      "0.0001",
			ToAddress:   to,
			URLCallback: "https://example.com/webhook",
		}
	}

	resp, err := c.Payouts.BatchExecute(ctx, &cryptochief.BatchExecuteRequest{
		Items: []cryptochief.ExecutePayoutRequest{mkItem(0), mkItem(1), mkItem(2)},
	})
	if err != nil {
		var apiErr *cryptochief.APIError
		if errors.As(err, &apiErr) {
			log.Fatalf("batch rejected: %s (%s)", apiErr.Code, apiErr.Message)
		}
		log.Fatal(err)
	}
	fmt.Printf("batch %s: total=%d accepted=%d rejected=%d\n",
		resp.BatchUUID, resp.Total, resp.Accepted, resp.Rejected)

	var wg sync.WaitGroup
	for _, it := range resp.Items {
		if it.UUID == "" || it.Error != "" {
			fmt.Printf("  [%d] %s skipped: status=%s err=%s\n", it.Index, it.OrderID, it.Status, it.Error)
			continue
		}
		wg.Add(1)
		go func(it cryptochief.BatchItemResult) {
			defer wg.Done()
			final, err := cryptochief.WaitForPayout(ctx, c, it.UUID, cryptochief.PollOptions{
				Interval: 4 * time.Second,
				Timeout:  4 * time.Minute,
			})
			if err != nil {
				fmt.Printf("  [%d] %s wait err: %v\n", it.Index, it.OrderID, err)
				return
			}
			fmt.Printf("  [%d] %s → %s tx=%s\n", it.Index, it.OrderID, final.Status, final.TxID)
		}(it)
	}
	wg.Wait()
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("set %s in env", name)
	}
	return v
}
