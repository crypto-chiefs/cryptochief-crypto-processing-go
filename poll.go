package cryptochief

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// PollOptions tunes the [WaitForPayout] / [WaitForTransaction] helpers.
// The zero value is fine for everyday use (5-second interval, 10-minute
// timeout).
type PollOptions struct {
	// Interval between polls. Defaults to 5s. Crypto Chief expects callers
	// to be reasonable here — block confirmation is several seconds at best.
	Interval time.Duration
	// Timeout bounds the whole wait. Defaults to 10m. Use the context
	// instead if you want a hard ceiling shared with the rest of your stack.
	Timeout time.Duration
}

func (o PollOptions) interval() time.Duration {
	if o.Interval <= 0 {
		return 5 * time.Second
	}
	return o.Interval
}

func (o PollOptions) timeout() time.Duration {
	if o.Timeout <= 0 {
		return 10 * time.Minute
	}
	return o.Timeout
}

// WaitForPayout polls /payout/info until the record reaches a terminal state
// (paid / failed / expired / cancel) or the context / timeout elapses. The
// last seen state is returned even on timeout, so callers can decide whether
// to retry.
func WaitForPayout(ctx context.Context, c *Client, uuid string, opts PollOptions) (*PayoutInfo, error) {
	return pollUntilTerminal(ctx, opts,
		func(ctx context.Context) (*PayoutInfo, error) { return c.Payouts.Info(ctx, uuid) },
		func(p *PayoutInfo) bool { return p.IsTerminal() })
}

// WaitForTransaction polls /transaction/info until the record reaches a
// terminal state (confirmed / failed / expired). The last seen state is
// returned even on timeout.
func WaitForTransaction(ctx context.Context, c *Client, uuid string, opts PollOptions) (*TransactionInfo, error) {
	return pollUntilTerminal(ctx, opts,
		func(ctx context.Context) (*TransactionInfo, error) { return c.Transactions.Info(ctx, uuid) },
		func(t *TransactionInfo) bool { return t.IsTerminal() })
}

// WaitForPayIn polls /payments/order/info until the record reaches a terminal
// state (paid / cancel / expired). The last seen state is returned even on
// timeout.
func WaitForPayIn(ctx context.Context, c *Client, uuid string, opts PollOptions) (*PayIn, error) {
	return pollUntilTerminal(ctx, opts,
		func(ctx context.Context) (*PayIn, error) { return c.PayIns.Info(ctx, uuid) },
		func(p *PayIn) bool { return p.IsTerminal() })
}

func pollUntilTerminal[T any](
	ctx context.Context,
	opts PollOptions,
	fetch func(context.Context) (*T, error),
	terminal func(*T) bool,
) (*T, error) {
	ctx, cancel := context.WithTimeout(ctx, opts.timeout())
	defer cancel()

	ticker := time.NewTicker(opts.interval())
	defer ticker.Stop()

	var last *T
	for {
		obj, err := fetch(ctx)
		if err != nil {
			// Soft-tolerate transient errors and try again next tick — the
			// transport already retried 5xx; if we're here on a 4xx (e.g.
			// uuid not yet visible) the next poll might succeed.
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return last, err
			}
			if !IsRetryable(err) {
				return last, err
			}
		} else {
			last = obj
			if terminal(obj) {
				return obj, nil
			}
		}

		select {
		case <-ctx.Done():
			if last != nil {
				return last, fmt.Errorf("cryptochief: poll did not reach terminal in %s: %w", opts.timeout(), ctx.Err())
			}
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
