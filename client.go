package cryptochief

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Logger is the minimal logging surface the client uses for debug-level
// request/response tracing. It is intentionally tiny so the SDK does not
// force a particular logging library — or a minimum Go version — on you.
//
// A *slog.Logger satisfies it out of the box: pass slog.Default(). Any type
// with a matching Debug(string, ...any) method works just as well.
type Logger interface {
	Debug(msg string, args ...any)
}

// DefaultBaseURL is the production processing API endpoint. Test-mode
// projects share the same URL — the test flag is per-project in the
// dashboard, not a separate host.
const DefaultBaseURL = "https://api-processing.crypto-chief.com"

// Client is the entry point to the Crypto Chief processing API.
//
// Construct one with [New] and reuse it across goroutines — it is safe for
// concurrent use. Methods are grouped by domain on the exported service
// fields (c.Payouts, c.Transactions, ...).
type Client struct {
	merchantID string
	apiKey     string
	baseURL    string
	httpClient *http.Client
	userAgent  string
	logger     Logger
	retry      retryConfig

	// tonRPCBaseURL is set by [WithTONRPCBaseURL]; empty = default host.
	tonRPCBaseURL  string
	tonRPCInternal *tonRPC // built on first use; shares the merchant credential

	// rsaPrivateKey, set by [WithRSAPrivateKey] or [WithRSAPrivateKeyPEM],
	// decrypts the private_key_encrypted field on generated wallets.
	rsaPrivateKey *rsa.PrivateKey
	// rsaInitErr stashes a load error so it surfaces on the first decrypt
	// attempt rather than silently disabling the feature.
	rsaInitErr error

	// Domain sub-clients. Populated by New.
	Payouts        *PayoutsService
	Transactions   *TransactionsService
	PayIns         *PayInsService
	Wallets        *WalletsService
	Sweeps         *SweepsService
	Withdrawals    *WithdrawalsService
	StaticDeposits *StaticDepositsService
	Blockchain     *BlockchainService
	Currencies     *CurrenciesService
	Credits        *CreditsService
}

type retryConfig struct {
	max       int
	baseDelay time.Duration
	maxDelay  time.Duration
}

// Option mutates the [Client] during construction. See the With* helpers.
type Option func(*Client)

// WithBaseURL overrides the API base URL. Useful for staging; leave at
// default for production.
func WithBaseURL(u string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(u, "/")
	}
}

// WithHTTPClient replaces the underlying *http.Client. The default uses
// a 60-second timeout, which fits every endpoint including batch payout
// (which can hold the connection for tens of seconds on large batches).
//
// If you customise the client, keep the timeout at 60s or higher.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// WithUserAgent sets the User-Agent header. The default identifies the
// library and its version.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// WithLogger attaches a [Logger] for debug-level request/response tracing.
// Pass slog.Default() (a *slog.Logger satisfies [Logger]) or any compatible
// logger. Nil (the default) disables logging.
func WithLogger(l Logger) Option {
	return func(c *Client) { c.logger = l }
}

// WithRetries configures automatic retry for transport-level failures and
// 5xx responses. max=0 disables retry; the default is 3 attempts with
// exponential backoff (200ms → 400ms → 800ms, jittered, capped at 5s).
//
// Idempotent requests are safe to retry. Crypto Chief's payout/execute and
// batch/execute use the merchant-supplied order_id for idempotency — a
// duplicate retry returns the same uuid instead of creating a second payout.
func WithRetries(max int) Option {
	return func(c *Client) { c.retry.max = max }
}

// WithRetryBackoff overrides the initial backoff delay and its ceiling.
func WithRetryBackoff(base, max time.Duration) Option {
	return func(c *Client) {
		c.retry.baseDelay = base
		c.retry.maxDelay = max
	}
}

// WithTONRPCBaseURL overrides the TON RPC base URL (default
// `https://rpc.crypto-chief.com`). Useful for staging — production
// callers can ignore it.
func WithTONRPCBaseURL(baseURL string) Option {
	return func(c *Client) { c.tonRPCBaseURL = baseURL }
}

// WithRSAPrivateKey loads the RSA private key from a PEM file at the
// given path. Once configured, [WalletsService.DecryptPrivateKey] will
// decrypt the `private_key_encrypted` field returned by
// [WalletsService.Generate] and [WalletsService.Info].
//
// Accepts PKCS#1 (`-----BEGIN RSA PRIVATE KEY-----`, the openssl genrsa
// default) and PKCS#8 (`-----BEGIN PRIVATE KEY-----`) encodings.
//
// The corresponding RSA public key must be uploaded to the project in
// the dashboard (Project Settings → RSA Key) — that's the key the API
// uses to encrypt wallet private keys.
func WithRSAPrivateKey(path string) Option {
	return func(c *Client) {
		k, err := LoadRSAPrivateKeyFile(path)
		if err != nil {
			c.rsaInitErr = err
			return
		}
		c.rsaPrivateKey = k
	}
}

// WithRSAPrivateKeyPEM is the in-memory variant of [WithRSAPrivateKey] —
// supply the PEM bytes directly instead of pointing at a file. Useful
// when secrets come from a vault, env, or HSM-style loader.
func WithRSAPrivateKeyPEM(pemBytes []byte) Option {
	return func(c *Client) {
		k, err := LoadRSAPrivateKeyPEM(pemBytes)
		if err != nil {
			c.rsaInitErr = err
			return
		}
		c.rsaPrivateKey = k
	}
}

// ton returns the internal RPC helper, building it lazily on first use.
// The Merchant ID is shared with the processing API, so it's always
// available — TON helpers work out of the box, no extra configuration
// required.
func (c *Client) ton() *tonRPC {
	if c.tonRPCInternal == nil {
		c.tonRPCInternal = newTONRPC(c.merchantID, c.tonRPCBaseURL, c.httpClient, c.userAgent)
	}
	return c.tonRPCInternal
}

// New constructs a [Client] for the given merchant credentials.
//
// Both merchantID and apiKey come from the Crypto Chief merchant dashboard
// (Integration tab). The API key is the signing secret — keep it server-side.
//
// Returns an error if either credential is empty.
func New(merchantID, apiKey string, opts ...Option) (*Client, error) {
	if merchantID == "" {
		return nil, errors.New("cryptochief: merchant ID is required")
	}
	if apiKey == "" {
		return nil, errors.New("cryptochief: API key is required")
	}

	c := &Client{
		merchantID: merchantID,
		apiKey:     apiKey,
		baseURL:    DefaultBaseURL,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		userAgent:  fmt.Sprintf("cryptochief-go/%s", Version),
		retry: retryConfig{
			max:       3,
			baseDelay: 200 * time.Millisecond,
			maxDelay:  5 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}

	c.Payouts = &PayoutsService{c: c}
	c.Transactions = &TransactionsService{c: c}
	c.PayIns = &PayInsService{c: c}
	c.Wallets = &WalletsService{c: c}
	c.Sweeps = &SweepsService{c: c}
	c.Withdrawals = &WithdrawalsService{c: c}
	c.StaticDeposits = &StaticDepositsService{c: c}
	c.Blockchain = &BlockchainService{c: c}
	c.Currencies = &CurrenciesService{c: c}
	c.Credits = &CreditsService{c: c}

	return c, nil
}

// MerchantID returns the merchant ID this client was configured with. Useful
// for logging.
func (c *Client) MerchantID() string { return c.merchantID }

// BaseURL returns the configured API base URL.
func (c *Client) BaseURL() string { return c.baseURL }
