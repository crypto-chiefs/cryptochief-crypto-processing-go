package cryptochief

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Signature headers of requests and webhooks.
const (
	// HeaderWebhookDelivery carries the delivery id of a webhook: 1–128
	// characters [A-Za-z0-9_-]. It is the same on every attempt and resend of
	// one delivery, so it serves as the receiver's idempotency key, and it is
	// what [WebhooksService.Info] and [WebhooksService.Resend] take.
	HeaderWebhookDelivery = "X-Webhook-Delivery"
	// HeaderTimestamp carries the Unix time of the signature in seconds.
	HeaderTimestamp = "X-CC-Timestamp"
	// HeaderSignature carries "v1=" and 64 hex characters of HMAC-SHA256.
	HeaderSignature = "X-CC-Signature"
)

// SignatureV1Prefix starts every X-CC-Signature value — the one a request
// carries and the one a webhook carries: the scheme version and "=".
// [SignHMACv1] and [SignWebhookV1] return values that already carry it.
const SignatureV1Prefix = "v1="

const (
	webhookV1Scope          = "CC-HMAC-SHA256-WEBHOOK-V1"
	defaultWebhookTolerance = 300 * time.Second
	webhookDeliveryIDMaxLen = 128
)

// Webhook verification refusals. [VerifyWebhook] returns exactly one of them
// for a webhook it rejects; match with [errors.Is]. Answer the sender with
// HTTP 401.
var (
	// ErrWebhookHeaders: X-CC-Timestamp, X-Webhook-Delivery or X-CC-Signature
	// is missing, repeated or malformed.
	ErrWebhookHeaders = errors.New("cryptochief: webhook signature headers are missing, repeated or malformed")
	// ErrWebhookTimestamp: X-CC-Timestamp is outside the tolerance.
	ErrWebhookTimestamp = errors.New("cryptochief: webhook timestamp is outside the tolerance")
	// ErrWebhookSignature: X-CC-Signature does not match the body.
	ErrWebhookSignature = errors.New("cryptochief: webhook signature does not match")
)

var (
	errWebhookTimestamp  = errors.New("cryptochief: webhook timestamp must be positive")
	errWebhookDeliveryID = errors.New("cryptochief: webhook delivery id must be 1-128 characters [A-Za-z0-9_-]")
)

// WebhookSenderIPs lists the IP addresses Crypto Chief delivers webhooks
// from. Whitelist these in front of any handler that mutates state.
var WebhookSenderIPs = []string{
	"164.90.231.203",
	"104.248.248.64",
}

// WebhookV1StringToSign builds the string to sign of a webhook. Lines are
// joined by "\n", with no trailing newline:
//
//	CC-HMAC-SHA256-WEBHOOK-V1
//	<timestamp>
//	<delivery id>
//	<lowercase hex SHA-256 of body>
//
// timestamp must be positive; deliveryID must be 1–128 characters
// [A-Za-z0-9_-].
func WebhookV1StringToSign(timestamp int64, deliveryID string, body []byte) (string, error) {
	if timestamp <= 0 {
		return "", errWebhookTimestamp
	}
	if !validWebhookDeliveryID(deliveryID) {
		return "", errWebhookDeliveryID
	}
	sum := sha256.Sum256(body)

	var b strings.Builder
	b.Grow(len(webhookV1Scope) + 20 + len(deliveryID) + 2*sha256.Size + 3)
	b.WriteString(webhookV1Scope)
	b.WriteByte('\n')
	b.WriteString(strconv.FormatInt(timestamp, 10))
	b.WriteByte('\n')
	b.WriteString(deliveryID)
	b.WriteByte('\n')
	b.WriteString(hex.EncodeToString(sum[:]))
	return b.String(), nil
}

// SignWebhookV1 returns the X-CC-Signature value of a webhook: "v1=" and
// lowercase hex HMAC-SHA256(key = apiKey, message =
// [WebhookV1StringToSign](timestamp, deliveryID, body)). body is the bytes
// sent. An apiKey that is empty or only spaces and tabs returns
// [ErrEmptyAPIKey].
func SignWebhookV1(apiKey string, timestamp int64, deliveryID string, body []byte) (string, error) {
	if blankAPIKey(apiKey) {
		return "", ErrEmptyAPIKey
	}
	sts, err := WebhookV1StringToSign(timestamp, deliveryID, body)
	if err != nil {
		return "", err
	}
	return SignatureV1Prefix + hex.EncodeToString(webhookMAC(apiKey, sts)), nil
}

// WebhookOption configures [VerifyWebhook] and [WebhookHandler].
type WebhookOption func(*webhookConfig)

type webhookConfig struct {
	tolerance time.Duration
	now       func() time.Time
}

// WithWebhookTolerance sets the allowed difference between X-CC-Timestamp and
// the current time. d <= 0 means the default, 300 seconds.
func WithWebhookTolerance(d time.Duration) WebhookOption {
	return func(c *webhookConfig) { c.tolerance = d }
}

// WithWebhookClock sets the source of the current time. nil means time.Now.
func WithWebhookClock(now func() time.Time) WebhookOption {
	return func(c *webhookConfig) { c.now = now }
}

// VerifyWebhook checks an incoming webhook against the API key. body is the
// raw request body, read before any JSON decoding; header is the request
// headers.
//
//	body, err := io.ReadAll(r.Body)
//	if err != nil { /* 400 */ }
//	if err := cryptochief.VerifyWebhook(apiKey, body, r.Header); err != nil {
//	    http.Error(w, "bad signature", http.StatusUnauthorized)
//	    return
//	}
//
// Checks, in order:
//
//  1. X-CC-Timestamp, X-Webhook-Delivery and X-CC-Signature are each present
//     once (names match ignoring ASCII case), with spaces and tabs trimmed at
//     the edges and no CR or LF; the timestamp is decimal digits with no
//     leading zero, the delivery id is 1–128 characters [A-Za-z0-9_-], the
//     signature is "v1=" and 64 hex characters. Otherwise
//     [ErrWebhookHeaders].
//  2. |now − timestamp| <= tolerance (default 300 seconds). Otherwise
//     [ErrWebhookTimestamp].
//  3. The signature equals [SignWebhookV1] over body, compared in constant
//     time, hex in any case. Otherwise [ErrWebhookSignature].
//
// An apiKey that is empty or only spaces and tabs refuses the webhook with
// [ErrEmptyAPIKey], which matches none of the three.
func VerifyWebhook(apiKey string, body []byte, header http.Header, opts ...WebhookOption) error {
	if blankAPIKey(apiKey) {
		return ErrEmptyAPIKey
	}
	cfg := webhookConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.tolerance <= 0 {
		cfg.tolerance = defaultWebhookTolerance
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}

	tsValue, ok := singleWebhookHeader(header, HeaderTimestamp)
	if !ok || !isCanonicalDecimal(tsValue) {
		return ErrWebhookHeaders
	}
	timestamp, err := strconv.ParseInt(tsValue, 10, 64)
	if err != nil {
		return ErrWebhookHeaders
	}

	deliveryID, ok := singleWebhookHeader(header, HeaderWebhookDelivery)
	if !ok || !validWebhookDeliveryID(deliveryID) {
		return ErrWebhookHeaders
	}

	sigValue, ok := singleWebhookHeader(header, HeaderSignature)
	if !ok || !strings.HasPrefix(sigValue, SignatureV1Prefix) {
		return ErrWebhookHeaders
	}
	hexSig := sigValue[len(SignatureV1Prefix):]
	if len(hexSig) != 2*sha256.Size {
		return ErrWebhookHeaders
	}
	got, err := hex.DecodeString(hexSig)
	if err != nil {
		return ErrWebhookHeaders
	}

	tol := int64(cfg.tolerance / time.Second)
	now := cfg.now().Unix()
	if timestamp < now-tol || timestamp > now+tol {
		return ErrWebhookTimestamp
	}

	sts, err := WebhookV1StringToSign(timestamp, deliveryID, body)
	if err != nil {
		return ErrWebhookHeaders
	}
	if !hmac.Equal(webhookMAC(apiKey, sts), got) {
		return ErrWebhookSignature
	}
	return nil
}

func webhookMAC(apiKey, stringToSign string) []byte {
	m := hmac.New(sha256.New, []byte(apiKey))
	m.Write([]byte(stringToSign))
	return m.Sum(nil)
}

// singleWebhookHeader returns the only value of the header name, with spaces
// and tabs trimmed. Keys are matched to name ignoring ASCII case only, across
// all spellings of the key. ok is false when the header is absent, repeated or
// contains CR or LF.
func singleWebhookHeader(header http.Header, name string) (string, bool) {
	var values []string
	for k, vs := range header {
		if asciiEqualFold(k, name) {
			values = append(values, vs...)
		}
	}
	if len(values) != 1 {
		return "", false
	}
	v := strings.Trim(values[0], " \t")
	if strings.ContainsAny(v, "\r\n") {
		return "", false
	}
	return v, true
}

// asciiEqualFold reports whether a and b are equal byte by byte, with A-Z
// matching a-z. Unlike strings.EqualFold it does not fold non-ASCII
// characters: U+017F does not match "s", U+212A does not match "k".
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// isCanonicalDecimal reports whether s is a non-empty run of digits with no
// leading zero ("0" itself passes). The string to sign carries the number, so
// a header value that is not its canonical spelling would let one signature
// stand for two different header bytes.
func isCanonicalDecimal(s string) bool {
	if s == "" {
		return false
	}
	if len(s) > 1 && s[0] == '0' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func validWebhookDeliveryID(s string) bool {
	if len(s) == 0 || len(s) > webhookDeliveryIDMaxLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// isWebhookRefusal reports whether err is one of the three verification
// refusals.
func isWebhookRefusal(err error) bool {
	return errors.Is(err, ErrWebhookHeaders) ||
		errors.Is(err, ErrWebhookTimestamp) ||
		errors.Is(err, ErrWebhookSignature)
}

// WebhookHandler wraps a typed handler with [VerifyWebhook] and JSON
// decoding. T is the expected event shape — pass one of the *WebhookEvent
// types below or your own struct. opts are passed to [VerifyWebhook].
//
//	http.Handle("/cc/webhook", cryptochief.WebhookHandler[cryptochief.PayoutWebhookEvent](apiKey,
//	    func(w http.ResponseWriter, r *http.Request, evt cryptochief.PayoutWebhookEvent) {
//	        log.Printf("payout %s → %s", evt.UUID, evt.Status)
//	    }))
//
// Responses: 405 for a method other than POST; 400 when the body cannot be
// read (limit 1 MiB) or decoded into T; 401 on a verification refusal; 500
// when apiKey is empty or only spaces and tabs. The handler's own status is
// kept; 200 when it writes nothing.
func WebhookHandler[T any](apiKey string, handler func(http.ResponseWriter, *http.Request, T), opts ...WebhookOption) http.Handler {
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
		if err := VerifyWebhook(apiKey, body, r.Header, opts...); err != nil {
			status := http.StatusUnauthorized
			if !isWebhookRefusal(err) {
				status = http.StatusInternalServerError
			}
			http.Error(w, err.Error(), status)
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
// per-source breakdown: Sources into []PayoutSource, ServiceOperations into
// []PayoutServiceOperation. "payout.paid" is sent once every source reached
// RequiredConfirmations.
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

	// Confirmations and RequiredConfirmations: see PayoutInfo. Both optional.
	Confirmations         *int `json:"confirmations,omitempty"`
	RequiredConfirmations int  `json:"required_confirmations,omitempty"`
}

// TransactionWebhookEvent is the payload on transaction events. Only terminal
// statuses fire: "transaction.confirmed", "transaction.failed",
// "transaction.expired", "transaction.cancelled".
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
	ErrorReason string `json:"error_reason,omitempty"` // set on transaction.failed, .expired and .cancelled

	// Confirmations and RequiredConfirmations: see TransactionInfo.
	Confirmations         int `json:"confirmations"`
	RequiredConfirmations int `json:"required_confirmations"`
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
// sweep transaction reached RequiredConfirmations.
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
	// need the count to apply it. It is never zero, and at least
	// RequiredConfirmations.
	Confirmations int `json:"sweep_confirmations"`

	// RequiredConfirmations is the count the sweep waited for. Optional: 0 when
	// not sent.
	RequiredConfirmations int `json:"required_confirmations,omitempty"`

	// ConfirmedAt is when the sweep was observed at RequiredConfirmations. It is
	// not Sweep.CompletedAt, which is the broadcast time.
	ConfirmedAt string `json:"confirmed_at,omitempty"`

	// TypeWork is what triggered the sweep: "momentum" (as soon as funds
	// arrived), "threshold" (a balance limit was reached) or "force" (you asked
	// for it).
	TypeWork string `json:"type_work,omitempty"`
	// TotalFeeUSD is what the sweep cost, network fee plus any gas or energy
	// the platform fronted to make it possible.
	TotalFeeUSD string `json:"total_fee_usd,omitempty"`
}
