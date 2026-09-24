package cryptochief

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// EVM signature supersede: the cancelled status, SupersededUUIDs on the sign
// answer, ErrorReason on the transaction, and the new error codes.

const (
	supersededOld = "0c1d9f3e-5a7b-4c2e-9f1a-3b6d8e2f4a10"
	supersededNew = "b4ee6a7a-f7c2-474d-b002-e83ebe3e78db"
)

func supersedeServer(t *testing.T, status int, body string) (*Client, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	c, err := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	return c, &calls
}

func TestTransactionCancelledIsTerminal(t *testing.T) {
	if !(TransactionInfo{Status: TxStatusCancelled}).IsTerminal() {
		t.Error("cancelled must be terminal")
	}
	if (TransactionInfo{Status: TxStatusCancelled}).Succeeded() {
		t.Error("cancelled must not read as succeeded")
	}
	for _, s := range []string{TxStatusSigned, TxStatusBroadcasting, TxStatusBroadcasted} {
		if (TransactionInfo{Status: s}).IsTerminal() {
			t.Errorf("%s must not be terminal", s)
		}
	}
}

// A superseded signature is final: the waiter returns it at once instead of
// polling until the timeout.
func TestWaitForTransactionReturnsCancelled(t *testing.T) {
	c, calls := supersedeServer(t, 200,
		`{"uuid":"`+supersededOld+`","status":"cancelled","error_reason":"SUPERSEDED_BY:`+supersededNew+`"}`)

	tx, err := WaitForTransaction(context.Background(), c, supersededOld,
		PollOptions{Interval: 10 * time.Millisecond, Timeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatalf("WaitForTransaction: %v", err)
	}
	if tx.Status != TxStatusCancelled || tx.ErrorReason != "SUPERSEDED_BY:"+supersededNew {
		t.Errorf("got %+v", tx)
	}
	if n := atomic.LoadInt32(calls); n != 1 {
		t.Errorf("polled %d times, want 1", n)
	}
}

func TestSignReportsSupersededUUIDs(t *testing.T) {
	c, _ := supersedeServer(t, 200, `{"uuid":"`+supersededNew+`","status":"signed","network":"ETH_MAINNET","chain_family":"EVM","signed_tx_hex":"0x02","tx_hash":"0xabc","expires_at":"2026-06-01T12:10:00Z","superseded_uuids":["`+supersededOld+`"]}`)

	res, err := c.Transactions.Sign(context.Background(), &SignTransactionRequest{
		Network: ChainEthMainnet, FromAddress: "0xfrom", Type: TxTypeNative, ToAddress: "0xto", Value: "1",
	})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !reflect.DeepEqual(res.SupersededUUIDs, []string{supersededOld}) {
		t.Errorf("SupersededUUIDs = %v", res.SupersededUUIDs)
	}
}

func TestExecuteNonceCodes(t *testing.T) {
	for _, tc := range []struct {
		msg      string
		sentinel error
	}{
		{CodeNonceGap, ErrNonceGap},
		{CodeNonceAlreadyUsed, ErrNonceAlreadyUsed},
	} {
		t.Run(tc.msg, func(t *testing.T) {
			c, _ := supersedeServer(t, 400, `{"error":"SERVICE_ERROR","msg":"`+tc.msg+`","ok":false}`)
			_, err := c.Transactions.Execute(context.Background(), &ExecuteTransactionRequest{UUID: supersededNew})
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("err = %v, want %v", err, tc.sentinel)
			}
			var ae *APIError
			if !errors.As(err, &ae) || ae.HTTPStatus != 400 {
				t.Errorf("err = %#v", err)
			}
		})
	}
}

func TestSignRefusedWhileExecuteUnresolved(t *testing.T) {
	c, _ := supersedeServer(t, 400,
		`{"error":"SERVICE_ERROR","msg":"PREVIOUS_EXECUTE_UNRESOLVED: uuid=`+supersededOld+`","ok":false}`)

	_, err := c.Transactions.Sign(context.Background(), &SignTransactionRequest{
		Network: ChainEthMainnet, FromAddress: "0xfrom", Type: TxTypeNative, ToAddress: "0xto", Value: "1",
	})
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v", err)
	}
	if !strings.HasPrefix(ae.Code, CodePreviousExecuteUnresolved) || !strings.HasSuffix(ae.Code, supersededOld) {
		t.Errorf("Code = %q", ae.Code)
	}
}
