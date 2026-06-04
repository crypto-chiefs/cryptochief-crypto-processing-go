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
	"time"
)

const (
	headerMerchant  = "Merchant"
	headerSignature = "Signature"
)

// do is the single transport entry point. It canonicalises the body, signs
// it, sends the request, retries on transient failures, parses the response
// envelope, and unmarshals success into out (which may be nil for endpoints
// that return only a status field).
//
// path must start with "/" — e.g. "/v1/payout/estimate".
func (c *Client) do(ctx context.Context, path string, in, out any) error {
	canonical, err := canonicalJSON(in)
	if err != nil {
		return err
	}
	sig := signBody(canonical, c.apiKey)
	url := c.baseURL + path

	var lastErr error
	attempts := c.retry.max + 1
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			delay := backoffDelay(attempt, c.retry.baseDelay, c.retry.maxDelay)
			if c.logger != nil {
				c.logger.Debug("cryptochief retry", "attempt", attempt, "delay", delay, "path", path)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		// Each attempt gets a fresh body reader — net/http closes the
		// previous one on retry.
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(canonical))
		if err != nil {
			return fmt.Errorf("cryptochief: build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set(headerMerchant, c.merchantID)
		req.Header.Set(headerSignature, sig)
		req.Header.Set("User-Agent", c.userAgent)

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

// errorEnvelope is the API's error response shape.
type errorEnvelope struct {
	Error string `json:"error"`
	Msg   string `json:"msg"`
	OK    bool   `json:"ok"`
}

func parseAPIError(status int, body []byte) *APIError {
	var env errorEnvelope
	_ = json.Unmarshal(body, &env)
	code := env.Msg
	if code == "" {
		code = env.Error
	}
	if code == "" {
		code = fmt.Sprintf("HTTP_%d", status)
	}
	message := env.Error
	if env.Msg != "" && env.Msg != env.Error {
		message = env.Msg
	}
	return &APIError{
		HTTPStatus: status,
		Code:       code,
		Message:    message,
		Raw:        body,
	}
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
