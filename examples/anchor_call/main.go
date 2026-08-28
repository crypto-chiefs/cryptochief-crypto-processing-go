// anchor_call invokes an Anchor program on Solana. The 8-byte discriminator
// and Borsh-encoded arguments are produced automatically — you only describe
// the args by Borsh type.
//
// Run this from the examples/ directory — it is a separate Go module:
//
//	MERCHANT_ID=... API_KEY=... \
//	FROM_ADDRESS=YourWallet... \
//	PROGRAM=YourProgramId... \
//	METHOD=initialize \
//	go run ./anchor_call
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

	signed, err := c.Transactions.SignAnchorCall(ctx, &cryptochief.AnchorCallRequest{
		Network:     cryptochief.ChainSolanaDevnet,
		FromAddress: mustEnv("FROM_ADDRESS"),
		Program:     mustEnv("PROGRAM"),
		Method:      envOr("METHOD", "initialize"),
		// Anchor expects positional Borsh args matching the method's Rust signature.
		// e.g. pub fn initialize(ctx: Context<Init>, amount: u64, label: String) → these:
		Args: []cryptochief.BorshValue{
			cryptochief.BorshU64(1_000_000),
			cryptochief.BorshString("demo"),
		},
		// Solana has no on-chain ABI — you MUST supply the accounts in the
		// order the program expects (refer to your program's IDL/source).
		Accounts: []cryptochief.SolanaAccount{
			{Pubkey: mustEnv("FROM_ADDRESS"), IsSigner: true, IsWritable: true},
			{Pubkey: "11111111111111111111111111111111", IsSigner: false, IsWritable: false}, // System program
		},
		URLCallback: "https://example.com/webhooks/transaction",
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

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
