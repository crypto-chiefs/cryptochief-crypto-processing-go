package cryptochief

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	urlpkg "net/url"
	"strconv"
	"strings"
	"time"
)

const (
	headerMerchant       = "Merchant"
	headerNonce          = "X-CC-Nonce"
	headerIdempotencyKey = "Idempotency-Key"
)

// ErrIdempotencyKey is returned by a call whose context carries an idempotency
// key that cannot be sent as it is.
var ErrIdempotencyKey = errors.New("cryptochief: idempotency key must be printable ASCII without a leading or trailing space or tab")

type idempotencyKeyCtxKey struct{}

// WithIdempotencyKey returns a copy of ctx that makes every call made with it
// send Idempotency-Key.
//
//	ctx := cryptochief.WithIdempotencyKey(ctx, "payout-2026-09-16-0001")
//	res, err := c.Payouts.Execute(ctx, req)
//
// The header is part of the string to sign, so it has to be set before the
// client signs: a header added by an [http.RoundTripper] is not covered by the
// signature and the server answers 401 INVALID_SIGNATURE. This is a per-call
// setting, unlike the With* [Option] helpers, which configure a [Client].
//
// The server keeps the value in the billing record of the call, up to 255
// bytes. It does not deduplicate payouts — [ExecutePayoutRequest.OrderID] does
// that.
//
// The key must be printable ASCII with no space or tab at either edge; the
// server trims those before signing, so an untrimmed value would be signed in
// a form it never sees. A key that does not qualify fails the call with
// [ErrIdempotencyKey]; an empty key sends no header.
func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, idempotencyKeyCtxKey{}, key)
}

// IdempotencyKeyFromContext returns the key set by [WithIdempotencyKey], or an
// empty string.
func IdempotencyKeyFromContext(ctx context.Context) string {
	key, _ := ctx.Value(idempotencyKeyCtxKey{}).(string)
	return key
}

// validIdempotencyKey reports whether key can be sent and signed as it is.
func validIdempotencyKey(key string) bool {
	if key == "" || key[0] == ' ' || key[len(key)-1] == ' ' {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] > 0x7e {
			return false
		}
	}
	return true
}

// do sends a POST — every route of the processing API takes one.
//
// path must start with "/" — e.g. "/v1/payout/estimate".
func (c *Client) do(ctx context.Context, path string, in, out any) error {
	return c.send(ctx, http.MethodPost, path, in, out)
}

// Request sends a signed request to path and decodes the JSON response into
// out (nil discards it). It is the low-level entry point behind every service
// method: same signing, retries, clock correction and error envelope.
//
// Use it for a route the SDK has no method for, on any Crypto Chief API that
// takes the same credentials. The energy API answers two of them on GET:
//
//	c, _ := cryptochief.New(merchantID, apiKey,
//	    cryptochief.WithBaseURL("https://energy.crypto-chief.com"))
//
//	var balance struct {
//	    Credits string `json:"credits"`
//	}
//	err := c.Request(ctx, http.MethodGet, "/v1/balance", nil, &balance)
//
// method is signed and sent in upper case, over a–z only — an HTTP method is
// an RFC 9110 token, and every other byte goes on the wire and into the string
// to sign as it is. path starts with "/" and holds the route without the base
// URL; a query goes on it as "?a=1&b=2" and is signed as written, while the
// path itself is signed percent-decoded — the form the server reads. in is the
// request body, encoded with encoding/json; nil sends none, which is what a
// GET takes.
func (c *Client) Request(ctx context.Context, method, path string, in, out any) error {
	if method == "" {
		return errors.New("cryptochief: request method is required")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("cryptochief: request path must start with \"/\": %q", path)
	}
	return c.send(ctx, upperASCII(method), path, in, out)
}

// send is the single transport entry point. It encodes in with encoding/json
// (nil gives an empty body), signs each attempt, sends the request, retries on
// transient failures, parses the response envelope, and unmarshals success
// into out (which may be nil for endpoints that return only a status field).
//
// method is used as given; path must start with "/" — e.g. "/v1/payout/estimate".
func (c *Client) send(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("cryptochief: marshal: %w", err)
		}
		body = b
	}
	url := c.baseURL + path
	// The route ends at the query or the fragment; neither is part of the
	// path the server reads, and a fragment is not sent at all.
	rawPath := path
	if i := strings.IndexAny(rawPath, "?#"); i >= 0 {
		rawPath = rawPath[:i]
	}
	// The servers sign the percent-decoded path, the form net/http hands a
	// handler in r.URL.Path. The escaped spelling is what goes on the wire.
	routePath, err := urlpkg.PathUnescape(rawPath)
	if err != nil {
		return fmt.Errorf("cryptochief: request path %q: %w", rawPath, err)
	}

	idempotencyKey := IdempotencyKeyFromContext(ctx)
	if idempotencyKey != "" && !validIdempotencyKey(idempotencyKey) {
		return fmt.Errorf("%w: %q", ErrIdempotencyKey, idempotencyKey)
	}

	var lastErr error
	attempts := c.retry.max + 1
	retries := 0
	clockAdjusted := false
	repeatNow := false
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 && !repeatNow {
			retries++
			delay := backoffDelay(retries, c.retry.baseDelay, c.retry.maxDelay)
			if c.logger != nil {
				c.logger.Debug("cryptochief retry", "attempt", attempt, "delay", delay, "path", path)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		repeatNow = false

		// Each attempt gets a fresh body reader — net/http closes the
		// previous one on retry.
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("cryptochief: build request: %w", err)
		}
		if len(body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set(headerMerchant, c.merchantID)
		req.Header.Set("User-Agent", c.userAgent)
		if idempotencyKey != "" {
			req.Header.Set(headerIdempotencyKey, idempotencyKey)
		}
		if err := c.signHMACv1(req, routePath, body); err != nil {
			return err
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = &APIError{Code: CodeNetworkError, Message: err.Error()}
			if !IsRetryable(lastErr) {
				return lastErr
			}
			continue
		}

		// Read the body unconditionally so we can close the connection and
		// reuse it from the pool.
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = &APIError{HTTPStatus: resp.StatusCode, Code: CodeNetworkError, Message: readErr.Error()}
			if !IsRetryable(lastErr) {
				return lastErr
			}
			continue
		}

		if c.logger != nil {
			c.logger.Debug("cryptochief response",
				"path", path, "status", resp.StatusCode, "bytes", len(body))
		}

		// 2xx → unwrap into out. Anything else → parse error envelope.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out == nil || len(body) == 0 {
				return nil
			}
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("cryptochief: decode %s response: %w (raw=%s)", path, err, truncate(body, 512))
			}
			return nil
		}

		apiErr := parseAPIError(resp.StatusCode, body)
		// Clock skew: set the offset from server_time and repeat once, outside
		// the retry budget.
		if !clockAdjusted && apiErr.Code == CodeSignatureTimestampOutOfRange {
			if apiErr.serverTime > 0 {
				clockAdjusted = true
				offset := apiErr.serverTime - c.now().Unix()
				c.clockOffset.Store(offset)
				if c.logger != nil {
					c.logger.Debug("cryptochief clock offset", "seconds", offset, "path", path)
				}
				lastErr = apiErr
				attempts++
				repeatNow = true
				continue
			}
		}
		// 5xx are retryable; 4xx are not (validation/billing — caller fault).
		if resp.StatusCode >= 500 {
			lastErr = apiErr
			continue
		}
		return apiErr
	}
	if lastErr == nil {
		lastErr = errors.New("cryptochief: retry budget exhausted")
	}
	return lastErr
}

// signHMACv1 sets X-CC-Timestamp, X-CC-Nonce and X-CC-Signature on req.
// Merchant, Idempotency-Key and the query are taken from req; path is the API
// route without the base URL.
func (c *Client) signHMACv1(req *http.Request, path string, body []byte) error {
	nonce, err := newHMACv1Nonce()
	if err != nil {
		return err
	}
	ts := strconv.FormatInt(c.now().Unix()+c.clockOffset.Load(), 10)
	sig, err := SignHMACv1(c.apiKey, HMACv1Input{
		Timestamp:      ts,
		Nonce:          nonce,
		Method:         req.Method,
		Path:           path,
		Query:          req.URL.RawQuery,
		Merchant:       trimOWS(req.Header.Get(headerMerchant)),
		IdempotencyKey: trimOWS(req.Header.Get(headerIdempotencyKey)),
		Body:           body,
	})
	if err != nil {
		return err
	}
	req.Header.Set(HeaderTimestamp, ts)
	req.Header.Set(headerNonce, nonce)
	req.Header.Set(HeaderSignature, "v1="+sig)
	return nil
}

// trimOWS strips the spaces and tabs a server drops from a header value.
func trimOWS(s string) string {
	return strings.Trim(s, " \t")
}

// errorEnvelope is the top level of an error response in either format.
// "error" is a string in the gateway format and an object in the
// installation format.
type errorEnvelope struct {
	Error      json.RawMessage `json:"error"`
	Msg        string          `json:"msg"`
	ServerTime json.RawMessage `json:"server_time"`
}

// installationError is the "error" object of the installation format.
type installationError struct {
	Name    string          `json:"name"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details"`
}

// installationErrorDetails is the part of "details" that carries the code.
type installationErrorDetails struct {
	Code       string          `json:"code"`
	ServerTime json.RawMessage `json:"server_time"`
}

// parseAPIError reads the code, message and server_time of an error response.
//
// Gateway format. The gateway's own refusals put the code in "error" and the
// sentence in "msg"; refusals relayed from an upstream service set "error" to
// SERVICE_ERROR and put the code in "msg":
//
//	{"ok":false,"error":"LABEL_TOO_LONG","msg":"label is longer than 255 characters"}
//	{"ok":false,"error":"SERVICE_ERROR","msg":"wallet_not_found"}
//
// Installation format. The code is in error.details.code, the sentence in
// error.message:
//
//	{"data":null,"error":{"status":401,"name":"UnauthorizedError","message":"...","details":{"code":"SIGNATURE_TIMESTAMP_OUT_OF_RANGE","server_time":1789430400}},"server_time":1789430400}
//
// server_time is taken from the top level, then from error.details.
func parseAPIError(status int, body []byte) *APIError {
	var env errorEnvelope
	_ = json.Unmarshal(body, &env)

	var code, message string
	serverTime := unixSeconds(env.ServerTime)
	if raw := bytes.TrimSpace(env.Error); len(raw) > 0 && raw[0] == '{' {
		var e installationError
		_ = json.Unmarshal(raw, &e)
		var d installationErrorDetails
		if dr := bytes.TrimSpace(e.Details); len(dr) > 0 && dr[0] == '{' {
			_ = json.Unmarshal(dr, &d)
		}
		code = d.Code
		if code == "" {
			code = e.Name
		}
		message = e.Message
		if message == "" {
			message = code
		}
		if serverTime == 0 {
			serverTime = unixSeconds(d.ServerTime)
		}
	} else {
		var errField string
		_ = json.Unmarshal(raw, &errField)
		code = errField
		if code == "" || code == CodeServiceError {
			code = env.Msg
		}
		if code == "" {
			code = errField
		}
		message = env.Msg
		if message == "" {
			message = errField
		}
	}
	if code == "" {
		code = fmt.Sprintf("HTTP_%d", status)
	}
	return &APIError{
		HTTPStatus: status,
		Code:       code,
		Message:    message,
		Raw:        body,
		serverTime: serverTime,
	}
}

// unixSeconds reads a positive Unix time in seconds from a JSON number; 0 if
// raw is not one.
func unixSeconds(raw json.RawMessage) int64 {
	n := string(bytes.TrimSpace(raw))
	if v, err := strconv.ParseInt(n, 10, 64); err == nil {
		if v > 0 {
			return v
		}
		return 0
	}
	if f, err := strconv.ParseFloat(n, 64); err == nil && f >= 1 && f < 1<<62 {
		return int64(f)
	}
	return 0
}

// backoffDelay returns an exponential-with-full-jitter delay capped at max.
// attempt is 1-indexed (first retry = attempt 1).
func backoffDelay(attempt int, base, max time.Duration) time.Duration {
	if base <= 0 {
		base = 200 * time.Millisecond
	}
	if max <= 0 {
		max = 5 * time.Second
	}
	d := base << (attempt - 1)
	if d <= 0 || d > max {
		d = max
	}
	// Full jitter — randomise uniformly in [0, d].
	return time.Duration(rand.Int63n(int64(d) + 1))
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
