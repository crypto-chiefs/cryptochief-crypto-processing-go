package cryptochief

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrInvalidSignature is returned when a webhook payload's Signature header
// does not match the body. Treat as a hard authentication failure — never
// process the event.
var ErrInvalidSignature = errors.New("cryptochief: invalid webhook signature")

// WebhookHeader is the case-insensitive name Crypto Chief uses for the
// signature header on outgoing webhooks.
const WebhookHeader = "Signature"

// WebhookSenderIPs lists the IP addresses Crypto Chief delivers webhooks
// from. Whitelist these in front of any handler that mutates state.
var WebhookSenderIPs = []string{
	"164.90.231.203",
	"104.248.248.64",
}

// VerifyWebhookSignature checks an incoming webhook against the merchant's
// API key. The body MUST be the exact bytes Crypto Chief sent — DO NOT
// re-encode it before passing it in.
//
//	body, _ := io.ReadAll(r.Body)
//	if err := cryptochief.VerifyWebhookSignature(apiKey, body, r.Header.Get("Signature")); err != nil {
//	    http.Error(w, "bad signature", http.StatusUnauthorized)
//	    return
//	}
//
// The signature is hex(md5(base64(canonicalJSON(body)) + apiKey)) — the
// same algorithm used to sign outgoing requests. We re-canonicalise the
// received bytes before verifying; the result is idempotent for any
// already-canonical body.
func VerifyWebhookSignature(apiKey string, body []byte, signatureHeader string) error {
	if apiKey == "" {
		return errors.New("cryptochief: API key is required for webhook verification")
	}
	if len(body) == 0 || signatureHeader == "" {
		return ErrInvalidSignature
	}

	// Round-trip the body through canonicalJSON so any drift in key order
	// is normalised before hashing. Unmarshal failure means the body
	// isn't JSON at all → fail closed.
	var tmp any
	if err := json.Unmarshal(body, &tmp); err != nil {
		return fmt.Errorf("cryptochief: webhook body is not JSON: %w", err)
	}
	canonical, err := json.Marshal(tmp)
	if err != nil {
		return fmt.Errorf("cryptochief: canonicalise webhook body: %w", err)
	}

	expected := signBody(canonical, apiKey)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(signatureHeader)) != 1 {
		return ErrInvalidSignature
	}
	return nil
}

// WebhookHandler wraps a typed handler with signature verification and JSON
// decoding. T is the expected event shape — pass one of the WebhookEvent*
// types below or your own struct.
//
//	http.Handle("/cc/webhook", cryptochief.WebhookHandler[cryptochief.PayoutWebhookEvent](apiKey,
//	    func(w http.ResponseWriter, r *http.Request, evt cryptochief.PayoutWebhookEvent) {
//	        log.Printf("payout %s → %s", evt.UUID, evt.Status)
//	    }))
func WebhookHandler[T any](apiKey string, handler func(http.ResponseWriter, *http.Request, T)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		if err := VerifyWebhookSignature(apiKey, body, r.Header.Get(WebhookHeader)); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		var evt T
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&evt); err != nil {
			http.Error(w, "decode event: "+err.Error(), http.StatusBadRequest)
			return
		}
		rw := &webhookResponseWriter{ResponseWriter: w}
		handler(rw, r, evt)
		if !rw.wrote {
			// Default to 200 OK if the handler didn't write a response itself.
			rw.WriteHeader(http.StatusOK)
		}
	})
}

// webhookResponseWriter records whether the wrapped handler wrote a status or
// body, so [WebhookHandler] can default to 200 OK only when the handler left
// the response untouched.
type webhookResponseWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *webhookResponseWriter) WriteHeader(code int) {
	w.wrote = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *webhookResponseWriter) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b)
}

// PayoutWebhookEvent is the payload Crypto Chief sends on payout events.
// Only the terminal statuses fire a webhook: "payout.paid" and
// "payout.system_fail". The nested fee_info / sources / service_operations
// objects are left as raw JSON — decode them on demand if you need the
// per-source breakdown.
type PayoutWebhookEvent struct {
	Event             string          `json:"event"` // "payout.paid" | "payout.system_fail"
	UUID              string          `json:"uuid"`
	OrderID           string          `json:"order_id"`
	UserID            string          `json:"user_id,omitempty"`
	Status            string          `json:"status"` // "paid" | "system_fail"
	AmountRequested   string          `json:"amount_requested,omitempty"`
	AmountToReceive   string          `json:"amount_to_receive,omitempty"`
	ToAddress         string          `json:"to_address,omitempty"`
	FeeInfo           json.RawMessage `json:"fee_info,omitempty"`
	Sources           json.RawMessage `json:"sources,omitempty"`
	ServiceOperations json.RawMessage `json:"service_operations,omitempty"`
	CreatedAt         string          `json:"created_at,omitempty"`
	CompletedAt       string          `json:"completed_at,omitempty"`
	ErrorReason       string          `json:"error_reason,omitempty"` // set on payout.system_fail
}

// TransactionWebhookEvent is the payload on transaction events. Only terminal
// statuses fire: "transaction.confirmed", "transaction.failed",
// "transaction.expired".
type TransactionWebhookEvent struct {
	Event       string `json:"event"` // "transaction.confirmed" | "transaction.failed" | ...
	UUID        string `json:"uuid"`
	Status      string `json:"status"`
	Network     Chain  `json:"network,omitempty"`
	ChainFamily string `json:"chain_family,omitempty"`
	Type        TxType `json:"type,omitempty"`
	FromAddress string `json:"from_address,omitempty"`
	ToAddress   string `json:"to_address,omitempty"`
	Value       string `json:"value,omitempty"`
	Contract    string `json:"contract,omitempty"` // token transfers only
	TxHash      string `json:"tx_hash,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	ErrorReason string `json:"error_reason,omitempty"` // set on transaction.failed
}

// PayInWebhookEvent is the payload on pay-in events. The event names carry
// the "invoice." prefix server-side: "invoice.paid", "invoice.paid_over",
// "invoice.paid_less", "invoice.canceled", "invoice.expired",
// "invoice.confirming", "invoice.system_fail".
type PayInWebhookEvent struct {
	Event            string    `json:"event"`
	UUID             string    `json:"uuid"`
	OrderID          string    `json:"order_id"`
	UserID           string    `json:"user_id,omitempty"`
	Status           string    `json:"status"`
	PrevStatus       string    `json:"prev_status,omitempty"`
	Mode             PayInMode `json:"mode,omitempty"`
	AmountCrypto     string    `json:"amount_crypto,omitempty"`
	AmountFiat       string    `json:"amount_fiat,omitempty"`
	FactAmountCrypto string    `json:"fact_amount_crypto,omitempty"`
	FactAmountFiat   string    `json:"fact_amount_fiat,omitempty"`
	Currency         string    `json:"currency,omitempty"`
	PaymentCoin      string    `json:"payment_coin,omitempty"`
	PaymentNetwork   Chain     `json:"payment_network,omitempty"`
	ToAddress        string    `json:"to_address,omitempty"`
	TxID             string    `json:"txid,omitempty"`
}

// StaticDepositWebhookEvent is the payload on static-deposit events. The
// event names carry the "static_deposit." prefix: "static_deposit.mempool",
// "static_deposit.found", "static_deposit.confirming", "static_deposit.paid",
// "static_deposit.reorged".
type StaticDepositWebhookEvent struct {
	Event                 string `json:"event"`
	UUID                  string `json:"uuid"`
	Status                string `json:"status"`
	Network               Chain  `json:"network,omitempty"`
	ChainFamily           string `json:"chain_family,omitempty"`
	Coin                  string `json:"coin,omitempty"`
	Contract              string `json:"contract,omitempty"`
	Decimals              int    `json:"decimals,omitempty"`
	ToAddress             string `json:"to_address,omitempty"`
	FromAddress           string `json:"from_address,omitempty"`
	TxHash                string `json:"tx_hash,omitempty"`
	Amount                string `json:"amount,omitempty"`
	AmountFiat            string `json:"amount_fiat,omitempty"`
	Confirmations         int    `json:"confirmations,omitempty"`
	RequiredConfirmations int    `json:"required_confirmations,omitempty"`
	FoundInMempool        bool   `json:"found_in_mempool,omitempty"`
	LogType               string `json:"log_type,omitempty"`
	BlockNumber           int64  `json:"block_number,omitempty"`
	CreatedAt             string `json:"created_at,omitempty"`
	UpdatedAt             string `json:"updated_at,omitempty"`
	ConfirmedAt           string `json:"confirmed_at,omitempty"`
	PaidAt                string `json:"paid_at,omitempty"`
}

// SweepEventConfirmed is the only sweep event the platform emits.
//
// There is deliberately no sweep.broadcasted. "We sent it" is not something you
// can act on, and an event that means "maybe" is one more thing to reconcile.
const SweepEventConfirmed = "sweep.confirmed"

// SweepWebhookEvent is the payload on "sweep.confirmed": funds that arrived on
// one of your deposit wallets have been swept to your master wallet AND the
// sweep transaction is confirmed on chain.
//
// WHAT THIS IS FOR. A "static_deposit.paid" event tells you a customer paid
// you. This tells you the money has finished moving into your own custody.
// Until it fires, the balance still sits on the deposit address. Reconciliation,
// treasury reporting and "funds available to pay out" all key off this event,
// not off the deposit.
//
// Sweeps run on static deposit wallets AND on the transit wallets issued per
// pay-in order, and both deliver here, to the callback URL configured for the
// wallet the funds left.
//
// WalletAddress is that wallet - the one your customer paid into. ToAddress is
// the master it landed on.
type SweepWebhookEvent struct {
	Event  string `json:"event"`
	TaskID string `json:"task_id"`
	// Status is "completed". A sweep reaches you in no other state.
	Status string `json:"status"`

	WalletAddress string `json:"wallet_address"`
	ToAddress     string `json:"to_address,omitempty"`

	Network       Chain  `json:"network"`
	ChainFamily   string `json:"chain_family,omitempty"`
	AssetSymbol   string `json:"asset_symbol"`
	Contract      string `json:"asset_contract,omitempty"`
	AssetType     string `json:"asset_type,omitempty"` // "native" | "token"
	AmountRaw     string `json:"amount_raw,omitempty"`
	Amount        string `json:"amount_human,omitempty"`
	SweepTxHash   string `json:"sweep_tx_hash"`
	GasPumpTxHash string `json:"gas_pump_tx_hash,omitempty"`

	// Confirmations is what makes this event true rather than hopeful, and it
	// travels with the event rather than being implied by it: "confirmed" is not
	// the same number on every chain, so if you run your own finality policy you
	// need the count to apply it. It is never zero.
	Confirmations int `json:"sweep_confirmations"`

	// ConfirmedAt is when the chain was observed to hold the sweep. It is NOT
	// the task's completion timestamp, which is stamped on every terminal
	// outcome including failures and so says nothing about settlement.
	ConfirmedAt string `json:"confirmed_at,omitempty"`

	// TypeWork is what triggered the sweep: "momentum" (as soon as funds
	// arrived), "threshold" (a balance limit was reached) or "force" (you asked
	// for it).
	TypeWork string `json:"type_work,omitempty"`
	// TotalFeeUSD is what the sweep cost, network fee plus any gas or energy
	// the platform fronted to make it possible.
	TotalFeeUSD string `json:"total_fee_usd,omitempty"`
}
