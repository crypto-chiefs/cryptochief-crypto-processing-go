// quickstart shows the smallest useful program: list a project's enabled
// assets and print one wallet's balance.
//
// Run this from the examples/ directory — it is a separate Go module:
//
//	go run ./quickstart
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
	merchant := mustEnv("MERCHANT_ID")
	apiKey := mustEnv("API_KEY")

	c, err := cryptochief.New(merchant, apiKey)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	assets, err := c.Blockchain.ContractsAvailable(ctx, cryptochief.ChainEthSepolia)
	if err != nil {
		log.Fatalf("contracts/available: %v", err)
	}
	fmt.Printf("ETH_SEPOLIA: %d enabled asset(s)\n", len(assets.Items))
	for _, a := range assets.Items {
		fmt.Printf("  %-8s decimals=%-2d contract=%s\n", a.Coin, a.Decimals, a.Contract)
	}

	if from := os.Getenv("FROM_ADDRESS"); from != "" {
		bal, err := c.Blockchain.WalletBalance(ctx, cryptochief.ChainEthSepolia, []string{from})
		if err != nil {
			log.Fatalf("balance: %v", err)
		}
		fmt.Printf("\n%s balances:\n", from)
		for _, b := range bal {
			label := "native"
			if b.Contract != "" {
				label = b.Contract
			}
			fmt.Printf("  %-46s %s\n", label, b.HumanValue)
		}
	}
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("set %s in env", name)
	}
	return v
}
