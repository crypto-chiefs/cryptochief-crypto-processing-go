// webhook_server is a copy-pasteable HTTP server for receiving Crypto
// Chief callbacks. Every handler:
//
//   - verifies the Signature header (done by WebhookHandler),
//   - logs the event with a clear "next action" hint for your backend,
//   - leaves TODO stubs where your business logic plugs in.
//
// Routes (configure these in Dashboard → Project Settings → Webhooks):
//
//	POST /webhook/payout          — PayOut lifecycle
//	POST /webhook/payin           — PayIn lifecycle
//	POST /webhook/transaction     — two-phase sign/execute results
//	POST /webhook/static-deposit  — incoming deposits on static wallets
//
// Run:
//
// Run this from the examples/ directory — it is a separate Go module:
//
//	API_KEY=...  ADDR=:8080  go run ./webhook_server
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/crypto-chiefs/cryptochief-crypto-processing-go"
)

func main() {
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		log.Fatal("set API_KEY in env (the same signing secret the SDK uses)")
	}
	addr := ":8080"
	if v := os.Getenv("ADDR"); v != "" {
		addr = v
	}

	mux := http.NewServeMux()
	mux.Handle("/webhook/payout", cryptochief.WebhookHandler[cryptochief.PayoutWebhookEvent](apiKey, handlePayout))
	mux.Handle("/webhook/payin", cryptochief.WebhookHandler[cryptochief.PayInWebhookEvent](apiKey, handlePayIn))
	mux.Handle("/webhook/transaction", cryptochief.WebhookHandler[cryptochief.TransactionWebhookEvent](apiKey, handleTransaction))
	mux.Handle("/webhook/static-deposit", cryptochief.WebhookHandler[cryptochief.StaticDepositWebhookEvent](apiKey, handleStaticDeposit))

	printBanner(addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func printBanner(addr string) {
	fmt.Println("──────────────────────────────────────────────────────────────")
	fmt.Println(" Crypto Chief webhook server")
	fmt.Println("──────────────────────────────────────────────────────────────")
	fmt.Printf(" Listening on %s\n", addr)
	fmt.Println(" Allow-list at your edge:", cryptochief.WebhookSenderIPs)
	fmt.Println()
	fmt.Println(" Routes & lifecycles:")
	fmt.Println("   /webhook/payout         (terminal-only)  → paid | system_fail")
	fmt.Println("   /webhook/payin          waiting_asset_select → pending → processing → paid | cancel | expired")
	fmt.Println("   /webhook/transaction    (terminal-only)  → confirmed | failed | expired")
	fmt.Println("   /webhook/static-deposit in_mempool → confirm_check → paid | dropped | reorged")
	fmt.Println("──────────────────────────────────────────────────────────────")
}

// ─────────────────────────────────────────────────────────────────────────────
// PayOut — outbound payment lifecycle
//
// The webhook fires ONLY on terminal statuses: `paid` (money left your
// treasury and is on its way) and `system_fail` (it didn't). Per-source tx
// hashes and fees live inside evt.Sources / evt.FeeInfo (raw JSON).
// ─────────────────────────────────────────────────────────────────────────────

func handlePayout(w http.ResponseWriter, r *http.Request, evt cryptochief.PayoutWebhookEvent) {
	log.Printf("[payout] uuid=%s order=%s status=%s amount_requested=%s amount_to_receive=%s to=%s",
		evt.UUID, evt.OrderID, evt.Status, evt.AmountRequested, evt.AmountToReceive, evt.ToAddress)

	switch evt.Status {
	case cryptochief.PayoutStatusPaid:
		log.Printf("  → ACTION: mark order %q PAID in DB; per-source tx hashes are in evt.Sources; notify user", evt.OrderID)
		// TODO: orders.MarkPaid(ctx, evt.OrderID)
		// TODO: mail.SendReceipt(ctx, evt.OrderID)
		// TODO: ledger.Credit(...)

	case cryptochief.PayoutStatusSystemFail,
		cryptochief.PayoutStatusFailed,
		cryptochief.PayoutStatusExpired,
		cryptochief.PayoutStatusCancel:
		log.Printf("  → ACTION: roll back order %q (reason=%s); unlock customer credit; alert ops", evt.OrderID, evt.ErrorReason)
		// TODO: orders.RollBack(ctx, evt.OrderID, evt.Status)
		// TODO: ops.Alert("payout failed", evt)
	}

	w.WriteHeader(http.StatusOK)
}

// ─────────────────────────────────────────────────────────────────────────────
// PayIn — incoming customer payment lifecycle
//
// Sequence:  waiting_asset_select → pending → processing → paid | cancel | expired
// Fulfil orders ONLY on `paid`.
// ─────────────────────────────────────────────────────────────────────────────

func handlePayIn(w http.ResponseWriter, r *http.Request, evt cryptochief.PayInWebhookEvent) {
	log.Printf("[payin] uuid=%s order=%s status=%s coin=%s amount_crypto=%s to=%s",
		evt.UUID, evt.OrderID, evt.Status, evt.PaymentCoin, evt.AmountCrypto, evt.ToAddress)

	switch evt.Status {
	case cryptochief.PayInStatusWaitingAssetSelect:
		log.Printf("  → ACTION (FIAT flow): direct customer to the asset-select UI for order %q", evt.OrderID)

	case cryptochief.PayInStatusPending:
		log.Printf("  → ACTION: render the deposit page (address=%s, amount=%s %s); start the countdown",
			evt.ToAddress, evt.AmountCrypto, evt.PaymentCoin)

	case cryptochief.PayInStatusProcessing, cryptochief.PayInStatusProcess:
		log.Printf("  → ACTION (optional): show 'payment received, waiting for confirmations' UI")

	case cryptochief.PayInStatusPaid:
		log.Printf("  → ACTION: FULFIL order %q; credit user account; send receipt", evt.OrderID)
		// TODO: orders.MarkPaid(ctx, evt.OrderID)
		// TODO: inventory.Ship(ctx, evt.OrderID)
		// TODO: mail.SendReceipt(ctx, evt.OrderID)

	case cryptochief.PayInStatusCancel, cryptochief.PayInStatusExpired:
		log.Printf("  → ACTION: release reserved inventory for order %q; offer 'try again' to customer", evt.OrderID)
		// TODO: inventory.Release(ctx, evt.OrderID)
	}

	w.WriteHeader(http.StatusOK)
}

// ─────────────────────────────────────────────────────────────────────────────
// Transaction — two-phase sign/execute results
//
// Sequence (webhook fires terminal-only):  ... → confirmed | failed | expired
// The intermediate `signed`/`broadcasted` states do NOT trigger webhooks —
// poll Transactions.Info if you need them.
// ─────────────────────────────────────────────────────────────────────────────

func handleTransaction(w http.ResponseWriter, r *http.Request, evt cryptochief.TransactionWebhookEvent) {
	log.Printf("[transaction] uuid=%s status=%s network=%s tx=%s from=%s to=%s value=%s",
		evt.UUID, evt.Status, evt.Network, evt.TxHash, evt.FromAddress, evt.ToAddress, evt.Value)

	switch evt.Status {
	case cryptochief.TxStatusConfirmed:
		log.Printf("  → ACTION: record successful tx %q in DB (confirmed at %s); release any optimistic lock", evt.TxHash, evt.CompletedAt)
		// TODO: txs.RecordSuccess(ctx, evt.UUID, evt.TxHash)

	case cryptochief.TxStatusFailed, cryptochief.TxStatusExpired:
		log.Printf("  → ACTION: mark execute %q failed (status=%s, reason=%s); surface to user; decide retry strategy", evt.UUID, evt.Status, evt.ErrorReason)
		// TODO: txs.RecordFailure(ctx, evt.UUID, evt.Status)
		// TODO: ops.Alert("contract tx failed", evt)
	}

	w.WriteHeader(http.StatusOK)
}

// ─────────────────────────────────────────────────────────────────────────────
// Static deposit — funds arriving on a per-customer static wallet
//
// Sequence:  in_mempool → confirm_check → paid | dropped | reorged
// Only `paid` is final. `dropped`/`reorged` retract a deposit that briefly
// appeared in `in_mempool`/`confirm_check` — roll back any optimistic credit.
// ─────────────────────────────────────────────────────────────────────────────

func handleStaticDeposit(w http.ResponseWriter, r *http.Request, evt cryptochief.StaticDepositWebhookEvent) {
	log.Printf("[static-deposit] uuid=%s status=%s coin=%s amount=%s to=%s from=%s tx=%s",
		evt.UUID, evt.Status, evt.Coin, evt.Amount, evt.ToAddress, evt.FromAddress, evt.TxHash)

	switch evt.Status {
	case cryptochief.StaticDepositInMempool:
		log.Printf("  → ACTION (optional): show an 'incoming deposit' indicator for the wallet owner")

	case cryptochief.StaticDepositConfirmCheck:
		log.Printf("  → ACTION (optional): pending-confirmations UI; do NOT credit yet")

	case cryptochief.StaticDepositPaid:
		log.Printf("  → ACTION: credit user with %s %s (tx %s); persist the deposit", evt.Amount, evt.Coin, evt.TxHash)
		// userID := lookupCustomerByDepositAddress(evt.ToAddress)
		// TODO: balances.Credit(ctx, userID, evt.Coin, evt.Amount, evt.TxHash)
		// TODO: deposits.Persist(ctx, evt)

	case cryptochief.StaticDepositDropped, cryptochief.StaticDepositReorged:
		log.Printf("  → ACTION: roll back any optimistic credit for deposit %s; alert ops if amount > threshold", evt.UUID)
		// TODO: balances.Reverse(ctx, evt.UUID)
		// TODO: ops.Alert("deposit reverted", evt)
	}

	w.WriteHeader(http.StatusOK)
}
