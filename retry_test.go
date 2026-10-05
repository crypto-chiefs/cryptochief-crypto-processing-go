package cryptochief

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	urlpkg "net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// Retry rule: network errors and HTTP 502, 503, 504 are retried; every other
// status is returned after one attempt.

func newRetryTestClient(t *testing.T, baseURL string, retries int, opts ...Option) *Client {
	t.Helper()
	opts = append([]Option{
		WithBaseURL(baseURL),
		WithRetries(retries),
		WithRetryBackoff(time.Millisecond, time.Millisecond),
	}, opts...)
	c, err := New("m", "k", opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTransport_StatusNotRetried(t *testing.T) {
	cases := []struct {
		status int
		body   string
		code   string
	}{
		{http.StatusInternalServerError, `{"ok":false,"error":"INTERNAL_ERROR","msg":"internal error"}`, "INTERNAL_ERROR"},
		{http.StatusInternalServerError, `<html>Internal Server Error</html>`, "HTTP_500"},
		// A NETWORK_ERROR code sent by the server is still a 500.
		{http.StatusInternalServerError, `{"ok":false,"error":"SERVICE_ERROR","msg":"NETWORK_ERROR"}`, CodeNetworkError},
		{http.StatusNotImplemented, `{"ok":false,"error":"TOPUP_NOT_CONFIGURED","msg":"top-up is not configured"}`, "TOPUP_NOT_CONFIGURED"},
		{http.StatusHTTPVersionNotSupported, ``, "HTTP_505"},
		{520, ``, "HTTP_520"},
		{http.StatusTooManyRequests, `{"ok":false,"error":"RATE_LIMITED","msg":"too many requests"}`, "RATE_LIMITED"},
		{http.StatusBadRequest, `{"ok":false,"error":"INVALID_PARAMS","msg":"uuid is required"}`, CodeInvalidParams},
		{http.StatusNotFound, `{"ok":false,"error":"NOT_FOUND","msg":"not found"}`, CodeNotFound},
		{http.StatusConflict, `{"ok":false,"error":"ALREADY_EXECUTED","msg":"already executed"}`, CodeAlreadyExecuted},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.status)+" "+tc.code, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(srv.Close)

			c := newRetryTestClient(t, srv.URL, 3)
			_, err := c.Payouts.Info(context.Background(), "u1")
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.HTTPStatus != tc.status || apiErr.Code != tc.code {
				t.Fatalf("err = %v, want %d %s", err, tc.status, tc.code)
			}
			if n := hits.Load(); n != 1 {
				t.Errorf("attempts: %d, want 1", n)
			}
			if IsRetryable(err) {
				t.Error("IsRetryable = true, want false")
			}
		})
	}
}

// TestTransport_500NotRetriedByDefault: the default client (3 retries) sends a
// request answered 500 once.
func TestTransport_500NotRetriedByDefault(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"ok":false,"error":"INTERNAL_ERROR","msg":"internal error"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", "k", WithBaseURL(srv.URL))
	_, err := c.Payouts.Info(context.Background(), "u1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus != http.StatusInternalServerError {
		t.Fatalf("err = %v, want HTTP 500", err)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("attempts: %d, want 1", n)
	}
}

func TestTransport_GatewayStatusesRetried(t *testing.T) {
	for _, status := range []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		status := status
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			for _, retries := range []int{0, 1, 3} {
				t.Run(fmt.Sprintf("keeps failing, %d retries", retries), func(t *testing.T) {
					var hits atomic.Int32
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						hits.Add(1)
						w.WriteHeader(status)
						_, _ = io.WriteString(w, `{"ok":false,"error":"SERVICE_ERROR","msg":"upstream unavailable"}`)
					}))
					t.Cleanup(srv.Close)

					c := newRetryTestClient(t, srv.URL, retries)
					_, err := c.Payouts.Info(context.Background(), "u1")
					var apiErr *APIError
					if !errors.As(err, &apiErr) || apiErr.HTTPStatus != status {
						t.Fatalf("err = %v, want HTTP %d", err, status)
					}
					if n := int(hits.Load()); n != retries+1 {
						t.Errorf("attempts: %d, want %d", n, retries+1)
					}
					if !IsRetryable(err) {
						t.Error("IsRetryable = false, want true")
					}
				})
			}

			t.Run("succeeds after a retry", func(t *testing.T) {
				var hits atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if hits.Add(1) == 1 {
						w.WriteHeader(status)
						_, _ = io.WriteString(w, `{"ok":false,"error":"SERVICE_ERROR","msg":"upstream unavailable"}`)
						return
					}
					_, _ = io.WriteString(w, `{"uuid":"u1","status":"paid"}`)
				}))
				t.Cleanup(srv.Close)

				c := newRetryTestClient(t, srv.URL, 3)
				out, err := c.Payouts.Info(context.Background(), "u1")
				if err != nil {
					t.Fatalf("Info: %v", err)
				}
				if out.UUID != "u1" {
					t.Errorf("UUID = %q", out.UUID)
				}
				if n := hits.Load(); n != 2 {
					t.Errorf("attempts: %d, want 2", n)
				}
			})
		})
	}
}

// failingTransport fails the first `fail` round trips (all when fail < 0)
// without a response, then hands the request to next.
type failingTransport struct {
	fail  int32
	calls atomic.Int32
	next  http.RoundTripper
}

func (f *failingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	n := f.calls.Add(1)
	if f.fail < 0 || n <= f.fail {
		if r.Body != nil {
			r.Body.Close()
		}
		return nil, errors.New("connection refused")
	}
	return f.next.RoundTrip(r)
}

func TestTransport_NetworkErrorRetried(t *testing.T) {
	t.Run("keeps failing", func(t *testing.T) {
		ft := &failingTransport{fail: -1}
		c := newRetryTestClient(t, "http://127.0.0.1:1", 3, WithHTTPClient(&http.Client{Transport: ft}))
		_, err := c.Payouts.Info(context.Background(), "u1")
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != CodeNetworkError || apiErr.HTTPStatus != 0 {
			t.Fatalf("err = %v, want NETWORK_ERROR without a status", err)
		}
		if n := ft.calls.Load(); n != 4 {
			t.Errorf("attempts: %d, want 4", n)
		}
		if !IsRetryable(err) {
			t.Error("IsRetryable = false, want true")
		}
	})

	t.Run("succeeds after a retry", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"uuid":"u1","status":"paid"}`)
		}))
		t.Cleanup(srv.Close)

		ft := &failingTransport{fail: 1, next: http.DefaultTransport}
		c := newRetryTestClient(t, srv.URL, 3, WithHTTPClient(&http.Client{Transport: ft}))
		out, err := c.Payouts.Info(context.Background(), "u1")
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		if out.UUID != "u1" {
			t.Errorf("UUID = %q", out.UUID)
		}
		if n := ft.calls.Load(); n != 2 {
			t.Errorf("attempts: %d, want 2", n)
		}
	})

	t.Run("connection closed without a response", func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			_, _ = io.Copy(io.Discard, r.Body)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			conn.Close()
		}))
		t.Cleanup(srv.Close)

		c := newRetryTestClient(t, srv.URL, 3)
		_, err := c.Payouts.Info(context.Background(), "u1")
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != CodeNetworkError {
			t.Fatalf("err = %v, want NETWORK_ERROR", err)
		}
		if n := hits.Load(); n != 4 {
			t.Errorf("attempts: %d, want 4", n)
		}
	})

	t.Run("response body cut short", func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			_, _ = io.Copy(io.Discard, r.Body)
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"uuid\":")
			_ = buf.Flush()
			conn.Close()
		}))
		t.Cleanup(srv.Close)

		c := newRetryTestClient(t, srv.URL, 3)
		_, err := c.Payouts.Info(context.Background(), "u1")
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != CodeNetworkError || apiErr.HTTPStatus != http.StatusOK {
			t.Fatalf("err = %v, want NETWORK_ERROR with status 200", err)
		}
		if n := hits.Load(); n != 4 {
			t.Errorf("attempts: %d, want 4", n)
		}
		if !IsRetryable(err) {
			t.Error("IsRetryable = false, want true")
		}
	})
}

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"500", &APIError{HTTPStatus: 500, Code: "INTERNAL_ERROR"}, false},
		{"501", &APIError{HTTPStatus: 501, Code: "HTTP_501"}, false},
		{"505", &APIError{HTTPStatus: 505, Code: "HTTP_505"}, false},
		{"520", &APIError{HTTPStatus: 520, Code: "HTTP_520"}, false},
		{"429", &APIError{HTTPStatus: 429, Code: "RATE_LIMITED"}, false},
		{"400", &APIError{HTTPStatus: 400, Code: CodeInvalidParams}, false},
		{"401", &APIError{HTTPStatus: 401, Code: CodeSignatureTimestampOutOfRange}, false},
		{"404", &APIError{HTTPStatus: 404, Code: CodeNotFound}, false},
		{"502", &APIError{HTTPStatus: 502, Code: CodeServiceError}, true},
		{"503", &APIError{HTTPStatus: 503, Code: "HTTP_503"}, true},
		{"504", &APIError{HTTPStatus: 504, Code: "HTTP_504"}, true},
		{"no response", &APIError{Code: CodeNetworkError, Message: "connection refused"}, true},
		{"body read failure", &APIError{HTTPStatus: 200, Code: CodeNetworkError, Message: "unexpected EOF"}, true},
		{"NETWORK_ERROR sent by the server with 500", &APIError{HTTPStatus: 500, Code: CodeNetworkError,
			Raw: []byte(`{"ok":false,"error":"SERVICE_ERROR","msg":"NETWORK_ERROR"}`)}, false},
		{"wrapped 503", fmt.Errorf("call: %w", &APIError{HTTPStatus: 503}), true},
		{"wrapped 500", fmt.Errorf("call: %w", &APIError{HTTPStatus: 500}), false},
		{"sentinel", ErrInsufficientFunds, false},
		{"url.Error", &urlpkg.Error{Op: "Post", URL: "https://api.example", Err: errors.New("connection reset by peer")}, true},
		{"net.OpError", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, true},
		{"local error", ErrIdempotencyKey, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"context canceled", fmt.Errorf("wait: %w", context.Canceled), false},
		{"plain error", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsRetryable(tc.err); got != tc.want {
				t.Errorf("IsRetryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestWaitFor_RetryRule: polling continues after a 503 and stops at a 500.
func TestWaitFor_RetryRule(t *testing.T) {
	t.Run("500 ends the wait", func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"ok":false,"error":"INTERNAL_ERROR","msg":"internal error"}`)
		}))
		t.Cleanup(srv.Close)

		c := newRetryTestClient(t, srv.URL, 0)
		_, err := WaitForTransaction(context.Background(), c, "t1", PollOptions{Interval: time.Millisecond, Timeout: 5 * time.Second})
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.HTTPStatus != http.StatusInternalServerError {
			t.Fatalf("err = %v, want HTTP 500", err)
		}
		if n := hits.Load(); n != 1 {
			t.Errorf("polls: %d, want 1", n)
		}
	})

	t.Run("503 keeps polling", func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if hits.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = io.WriteString(w, `{"uuid":"t1","status":"confirmed","network":"ETH_MAINNET","from_address":"0xa","tx_hash":"0x1","confirmations":13,"required_confirmations":12}`)
		}))
		t.Cleanup(srv.Close)

		c := newRetryTestClient(t, srv.URL, 0)
		tx, err := WaitForTransaction(context.Background(), c, "t1", PollOptions{Interval: time.Millisecond, Timeout: 5 * time.Second})
		if err != nil {
			t.Fatalf("WaitForTransaction: %v", err)
		}
		if tx.Status != "confirmed" {
			t.Errorf("Status = %q", tx.Status)
		}
		if n := hits.Load(); n != 2 {
			t.Errorf("polls: %d, want 2", n)
		}
	})
}
