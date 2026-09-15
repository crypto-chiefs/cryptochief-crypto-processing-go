// withdrawal_status prints the latest page of treasury withdrawals, or, with
// WITHDRAWAL_UUID set, polls one withdrawal until it is final.
//
// Run this from the examples/ directory — it is a separate Go module:
//
//	MERCHANT_ID=... API_KEY=... go run ./withdrawal_status
//	MERCHANT_ID=... API_KEY=... WITHDRAWAL_UUID=... go run ./withdrawal_status
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

	if uuid := os.Getenv("WITHDRAWAL_UUID"); uuid != "" {
		follow(c, uuid)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, err := c.Withdrawals.History(ctx, cryptochief.HistoryQuery{PageSize: 20})
	if err != nil {
		log.Fatalf("withdrawal/history: %v", err)
	}
	fmt.Printf("%d withdrawal(s) in total, showing %d\n", page.Meta.Total, len(page.Items))
	for _, w := range page.Items {
		fmt.Printf("  %s  %-16s %s %s → %s  %s\n", w.UUID, w.Status, w.Amount, w.Coin, w.ToAddress, progress(w))
	}
}

// follow polls one withdrawal until it reaches a final state.
func follow(c *cryptochief.Client, uuid string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	for {
		w, err := c.Withdrawals.Info(ctx, uuid)
		if err != nil {
			log.Fatalf("withdrawal/info: %v", err)
		}
		fmt.Printf("%s  %-16s %s\n", time.Now().Format(time.TimeOnly), w.Status, progress(*w))

		if w.IsTerminal() {
			switch {
			case w.Succeeded():
				fmt.Printf("settled at %s, tx %s\n", w.CompletedAt, w.TxHash)
			default:
				// failed
				fmt.Printf("not settled: %s (%s)\n", w.Status, w.ErrorReason)
			}
			return
		}

		select {
		case <-ctx.Done():
			log.Fatalf("still %s when the wait ran out", w.Status)
		case <-time.After(15 * time.Second):
		}
	}
}

// progress prints the confirmation count against RequiredConfirmations. A final
// withdrawal without a count prints "-".
func progress(w cryptochief.Withdrawal) string {
	if w.Confirmations == nil {
		if w.IsTerminal() {
			return "-"
		}
		return fmt.Sprintf("needs %d confirmation(s)", w.RequiredConfirmations)
	}
	return fmt.Sprintf("%d/%d confirmations", *w.Confirmations, w.RequiredConfirmations)
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("set %s in env", name)
	}
	return v
}
