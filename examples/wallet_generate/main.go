// wallet_generate creates a new project wallet and decrypts its private
// key locally with the merchant's RSA key.
//
// The corresponding RSA public key must be uploaded to your project
// (Dashboard → Project Settings → RSA Key) — the API uses it to encrypt
// every wallet private key it returns.
//
//	# generate an RSA keypair once and upload rsa_public.pem to the dashboard
//	openssl genrsa -out rsa_private.pem 2048
//	openssl rsa -in rsa_private.pem -pubout -out rsa_public.pem
//
//	MERCHANT_ID=... API_KEY=... RSA_KEY=./rsa_private.pem \
//	go run ./examples/wallet_generate
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
	c, err := cryptochief.New(
		mustEnv("MERCHANT_ID"),
		mustEnv("API_KEY"),
		cryptochief.WithRSAPrivateKey(mustEnv("RSA_KEY")),
	)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	w, err := c.Wallets.Generate(ctx, &cryptochief.GenerateWalletRequest{
		WalletType:  cryptochief.WalletTypeMaster,
		ChainFamily: cryptochief.FamilyEVM,
	})
	if err != nil {
		log.Fatalf("generate: %v", err)
	}
	fmt.Printf("address:        %s\n", w.Address)
	fmt.Printf("chain_family:   %s\n", w.ChainFamily)

	// The API returns the private key encrypted with your RSA public key.
	// DecryptPrivateKey uses the configured private key to recover the
	// chain-native hex form.
	priv, err := c.Wallets.DecryptPrivateKey(w.PrivateKeyEncrypted)
	if err != nil {
		log.Fatalf("decrypt: %v", err)
	}
	fmt.Printf("private_key:    %s\n", priv)
	fmt.Println()
	fmt.Println("Keep the private key safe — anyone with it can sign for this wallet.")
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("set %s in env", name)
	}
	return v
}
