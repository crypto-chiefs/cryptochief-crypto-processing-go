package cryptochief

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A payout in flight: one source confirmed deep, one broadcast and counted
// shallow, and a gas top-up. The top-level count is the platform's lowest
// among the sources, and every count must reach the caller as sent.
func TestPayoutInfo_ConfirmationsDecode(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{
		"uuid":"p1","order_id":"o1","user_id":"u1","status":"confirm_check",
		"amount_requested":"10","amount_to_receive":"10","to_address":"0xdest",
		"fee_info":{"fee_mode":"client","estimated_fiat":"0.40"},
		"sources":[
			{"address":"0xa","network":"ETH_MAINNET","coin":"USDT","amount_crypto":"6","need_refuel":false,"refuel_amount":"0","estimated_fee":"0.0001","estimated_fee_fiat":"0.20","txid":"0x1","confirmations":14},
			{"address":"0xb","network":"ETH_MAINNET","coin":"USDT","amount_crypto":"4","need_refuel":true,"refuel_amount":"0.001","estimated_fee":"0.0001","estimated_fee_fiat":"0.20","txid":"0x2","confirmations":3}
		],
		"service_operations":[
			{"type":"gas_refuel","context":"payout_prepare","status":"done","network":"ETH_MAINNET","coin":"ETH","amount_native":"0.001","from_address":"0xsvc","to_address":"0xb","estimated_fee":"0.00002","estimated_fee_fiat":"0.05","confirmations":20}
		],
		"confirmations":3,
		"created_at":"2026-09-14T10:00:00Z","completed_at":null
	}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Payouts.Info(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if path != "/v1/payout/info" {
		t.Errorf("path = %q", path)
	}
	if len(out.Sources) != 2 {
		t.Fatalf("sources = %d", len(out.Sources))
	}
	if got := out.Sources[0].Confirmations; got == nil || *got != 14 {
		t.Errorf("sources[0].confirmations = %v, want 14", derefInt(got))
	}
	if got := out.Sources[1].Confirmations; got == nil || *got != 3 {
		t.Errorf("sources[1].confirmations = %v, want 3", derefInt(got))
	}
	s0, s1 := out.Sources[0], out.Sources[1]
	if s0.TxID != "0x1" || s0.AmountCrypto != "6" || s0.Network != ChainEthMainnet || s0.EstimatedFeeFiat != "0.20" {
		t.Errorf("sources[0] = %+v", s0)
	}
	if s1.TxID != "0x2" || s1.AmountCrypto != "4" || !s1.NeedRefuel || s1.RefuelAmount != "0.001" {
		t.Errorf("sources[1] = %+v", s1)
	}
	if len(out.ServiceOperations) != 1 {
		t.Fatalf("service_operations = %d", len(out.ServiceOperations))
	}
	op := out.ServiceOperations[0]
	if op.Type != "gas_refuel" || op.Status != "done" || op.Network != ChainEthMainnet || op.ToAddress != "0xb" {
		t.Errorf("service operation = %+v", op)
	}
	if got := op.Confirmations; got == nil || *got != 20 {
		t.Errorf("service_operations[0].confirmations = %v, want 20", derefInt(got))
	}
	if got := out.Confirmations; got == nil || *got != 3 {
		t.Errorf("confirmations = %v, want 3", derefInt(got))
	}
	if out.UserID != "u1" || out.AmountRequested != "10" || out.AmountToReceive != "10" || out.ToAddress != "0xdest" {
		t.Errorf("payout = %+v", out)
	}
	if out.FeeInfo == nil || out.FeeInfo.FeeMode != "client" || out.FeeInfo.EstimatedFiat != "0.40" || out.FeeInfo.TotalFeePaidFiat != "" {
		t.Errorf("fee_info = %+v", out.FeeInfo)
	}
	// Not paid yet: completed_at is null.
	if out.CompletedAt != "" {
		t.Errorf("completed_at = %q, want empty before paid", out.CompletedAt)
	}
}

// A paid payout carries the moment it reached finality and the fee actually
// paid, on Info and on History items.
func TestPayoutInfo_PaidCarriesCompletedAtAndFee(t *testing.T) {
	const item = `{"uuid":"p1","order_id":"o1","user_id":"u1","status":"paid",
		"amount_requested":"10","amount_to_receive":"9.9","to_address":"0xdest",
		"fee_info":{"fee_mode":"mix","estimated_fiat":"0.40","limit_fiat":"2","limit_currency":"USD","total_fee_paid_fiat":"0.37"},
		"sources":[{"address":"0xa","network":"ETH_MAINNET","coin":"USDT","amount_crypto":"10","txid":"0x1","confirmations":12}],
		"confirmations":12,"required_confirmations":12,
		"created_at":"2026-09-14T10:00:00Z","completed_at":"2026-09-14T10:04:00Z"}`
	check := func(where string, p PayoutInfo) {
		t.Helper()
		if !p.Succeeded() || p.CompletedAt != "2026-09-14T10:04:00Z" {
			t.Errorf("%s: status/completed_at = %s/%q", where, p.Status, p.CompletedAt)
		}
		if p.UserID != "u1" || p.AmountRequested != "10" || p.AmountToReceive != "9.9" {
			t.Errorf("%s: amounts = %+v", where, p)
		}
		f := p.FeeInfo
		if f == nil || f.FeeMode != "mix" || f.EstimatedFiat != "0.40" || f.LimitFiat != "2" ||
			f.LimitCurrency != "USD" || f.TotalFeePaidFiat != "0.37" {
			t.Errorf("%s: fee_info = %+v", where, f)
		}
	}

	var path, body string
	srv := captureServer(t, item, &path, &body)
	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	info, err := c.Payouts.Info(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	check("info", *info)

	hist := captureServer(t, `{"items":[`+item+`],"meta":{"page":1,"page_size":50,"total":1}}`, &path, &body)
	c, _ = New("m", "k", WithBaseURL(hist.URL), WithRetries(0))
	page, err := c.Payouts.History(context.Background(), HistoryQuery{PageSize: 50})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d", len(page.Items))
	}
	check("history", page.Items[0])
}

// Every confirmation field on a payout is optional, and absent is not 0: a
// payout still in the queue has sent nothing, while a count of 0 says a
// transaction exists and is not yet counted. Decoding into plain ints would
// make the two indistinguishable.
func TestPayoutInfo_ConfirmationsAbsentIsNotZero(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{
		"uuid":"p2","order_id":"o2","status":"queue",
		"sources":[{"address":"0xa","network":"ETH_MAINNET","coin":"ETH","amount_crypto":"1","estimated_fee":"0.0001","estimated_fee_fiat":"0.20"}],
		"service_operations":[{"type":"gas_refuel","context":"payout_prepare","status":"planned","network":"ETH_MAINNET","coin":"ETH","amount_native":"0.001","from_address":"0xsvc","to_address":"0xa"}],
		"created_at":"2026-09-14T10:00:00Z","completed_at":null
	}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	queued, err := c.Payouts.Info(context.Background(), "p2")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if queued.Confirmations != nil {
		t.Errorf("confirmations = %d, want absent", *queued.Confirmations)
	}
	if queued.Sources[0].Confirmations != nil {
		t.Errorf("sources[0].confirmations = %d, want absent", *queued.Sources[0].Confirmations)
	}
	if queued.ServiceOperations[0].Confirmations != nil {
		t.Errorf("service_operations[0].confirmations = %d, want absent", *queued.ServiceOperations[0].Confirmations)
	}

	// Broadcast, not yet counted: the source has a txid and no count, the
	// payout's own count is 0 - present.
	var sent PayoutInfo
	if err := json.Unmarshal([]byte(`{"uuid":"p3","status":"confirm_check",
		"sources":[{"address":"0xa","txid":"0x1"}],"confirmations":0}`), &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Confirmations == nil || *sent.Confirmations != 0 {
		t.Errorf("confirmations = %v, want 0", derefInt(sent.Confirmations))
	}
	if sent.Sources[0].Confirmations != nil {
		t.Errorf("sources[0].confirmations = %d, want absent", *sent.Sources[0].Confirmations)
	}

	// Re-marshalling keeps the distinction in both directions.
	b, err := json.Marshal(sent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"confirmations":0`) {
		t.Errorf("a count of 0 was dropped on re-marshal: %s", b)
	}
	b, err = json.Marshal(queued)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"confirmations"`) {
		t.Errorf("an absent count was invented on re-marshal: %s", b)
	}
}

// History items and the idempotent replay of Execute are the same public view
// as Info, so the counts ride on both.
func TestPayoutHistoryAndExecuteReplay_CarryConfirmations(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"items":[
		{"uuid":"p1","order_id":"o1","status":"paid","sources":[{"address":"0xa","txid":"0x1","confirmations":30}],"confirmations":30},
		{"uuid":"p2","order_id":"o2","status":"queue","sources":[{"address":"0xb"}]}
	],"meta":{"page":1,"page_size":50,"total":2}}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	page, err := c.Payouts.History(context.Background(), HistoryQuery{PageSize: 50})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if path != "/v1/payout/history" {
		t.Errorf("path = %q", path)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d", len(page.Items))
	}
	if got := page.Items[0].Confirmations; got == nil || *got != 30 {
		t.Errorf("items[0].confirmations = %v, want 30", derefInt(got))
	}
	if got := page.Items[0].Sources[0].Confirmations; got == nil || *got != 30 {
		t.Errorf("items[0].sources[0].confirmations = %v, want 30", derefInt(got))
	}
	if page.Items[1].Confirmations != nil || page.Items[1].Sources[0].Confirmations != nil {
		t.Errorf("queued item carries a count: %+v", page.Items[1])
	}

	replay := captureServer(t, `{"uuid":"p1","order_id":"o1","status":"confirm_check",
		"sources":[{"address":"0xa","txid":"0x1","confirmations":2}],"confirmations":2}`, &path, &body)
	c, _ = New("m", "k", WithBaseURL(replay.URL), WithRetries(0))
	out, err := c.Payouts.Execute(context.Background(), &ExecutePayoutRequest{OrderID: "o1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if path != "/v1/payout/execute" {
		t.Errorf("path = %q", path)
	}
	if got := out.Confirmations; got == nil || *got != 2 {
		t.Errorf("confirmations = %v, want 2", derefInt(got))
	}
}

// Both transaction fields are always sent. A confirmed record carries the
// count it was confirmed at and the threshold applied. A broadcasted record
// carries 0 while it is not in a block (Execute's answer included) and, once it
// is, the count it has reached so far, below the threshold - still broadcasted,
// not confirmed.
func TestTransactionInfo_ConfirmationsDecode(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"uuid":"t1","status":"confirmed","network":"ETH_MAINNET","chain_family":"EVM","type":"native",
		"from_address":"0xa","to_address":"0xb","value":"1","tx_hash":"0x1",
		"confirmations":12,"required_confirmations":12,
		"expires_at":"2026-09-14T10:10:00Z","created_at":"2026-09-14T10:00:00Z","completed_at":"2026-09-14T10:03:00Z"}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	info, err := c.Transactions.Info(context.Background(), "t1")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if path != "/v1/transaction/info" {
		t.Errorf("path = %q", path)
	}
	if info.Confirmations != 12 || info.RequiredConfirmations != 12 {
		t.Errorf("confirmations = %d/%d, want 12/12", info.Confirmations, info.RequiredConfirmations)
	}
	if info.CompletedAt != "2026-09-14T10:03:00Z" || info.ErrorReason != "" {
		t.Errorf("completed_at/error_reason = %q/%q, want 2026-09-14T10:03:00Z/empty", info.CompletedAt, info.ErrorReason)
	}

	hist := captureServer(t, `{"items":[
		{"uuid":"t2","status":"failed","network":"BSC_MAINNET","from_address":"0xa","error_reason":"reverted","confirmations":0,"required_confirmations":15},
		{"uuid":"t3","status":"broadcasted","network":"ETH_MAINNET","from_address":"0xa","tx_hash":"0x3","confirmations":0,"required_confirmations":12},
		{"uuid":"t4","status":"broadcasted","network":"ETH_MAINNET","from_address":"0xa","tx_hash":"0x4","confirmations":5,"required_confirmations":12}
	],"meta":{"page":1,"page_size":20,"total":3}}`, &path, &body)
	c, _ = New("m", "k", WithBaseURL(hist.URL), WithRetries(0))
	page, err := c.Transactions.History(context.Background(), HistoryQuery{})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if path != "/v1/transaction/history" {
		t.Errorf("path = %q", path)
	}
	if len(page.Items) != 3 {
		t.Fatalf("items = %d", len(page.Items))
	}
	if page.Items[0].Confirmations != 0 || page.Items[0].RequiredConfirmations != 15 {
		t.Errorf("failed item = %d/%d, want 0/15", page.Items[0].Confirmations, page.Items[0].RequiredConfirmations)
	}
	if page.Items[0].ErrorReason != "reverted" {
		t.Errorf("failed item error_reason = %q, want reverted", page.Items[0].ErrorReason)
	}
	if page.Items[1].CompletedAt != "" {
		t.Errorf("broadcasted item completed_at = %q, want empty", page.Items[1].CompletedAt)
	}
	if page.Items[1].Confirmations != 0 || page.Items[1].RequiredConfirmations != 12 {
		t.Errorf("broadcasted item not in a block = %d/%d, want 0/12", page.Items[1].Confirmations, page.Items[1].RequiredConfirmations)
	}
	// In a block but short of the depth: the count is live, and the record is
	// neither terminal nor succeeded.
	inBlock := page.Items[2]
	if inBlock.Confirmations != 5 || inBlock.RequiredConfirmations != 12 {
		t.Errorf("broadcasted item in a block = %d/%d, want 5/12", inBlock.Confirmations, inBlock.RequiredConfirmations)
	}
	if inBlock.IsTerminal() || inBlock.Succeeded() {
		t.Errorf("broadcasted 5/12 reads terminal=%v succeeded=%v, want neither", inBlock.IsTerminal(), inBlock.Succeeded())
	}

	exec := captureServer(t, `{"uuid":"t3","status":"broadcasted","network":"ETH_MAINNET","from_address":"0xa","tx_hash":"0x3","confirmations":0,"required_confirmations":12}`, &path, &body)
	c, _ = New("m", "k", WithBaseURL(exec.URL), WithRetries(0))
	sent, err := c.Transactions.Execute(context.Background(), &ExecuteTransactionRequest{UUID: "t3"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if path != "/v1/transaction/execute" {
		t.Errorf("path = %q", path)
	}
	if sent.Confirmations != 0 || sent.RequiredConfirmations != 12 {
		t.Errorf("execute = %d/%d, want 0/12", sent.Confirmations, sent.RequiredConfirmations)
	}

	// The keys are always present on the wire, 0 included, and re-marshalling
	// must not turn a real 0 into an absent field.
	b, err := json.Marshal(sent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"confirmations":0`) || !strings.Contains(string(b), `"required_confirmations":12`) {
		t.Errorf("re-marshalled transaction lost a count: %s", b)
	}
}

// A broadcasted transaction whose count rises is still in flight: the helper
// keeps polling through 0/12 and 5/12 and returns only at confirmed.
func TestWaitForTransaction_RisingCountIsNotTerminal(t *testing.T) {
	steps := []string{
		`{"uuid":"t1","status":"broadcasted","network":"ETH_MAINNET","from_address":"0xa","tx_hash":"0x1","confirmations":0,"required_confirmations":12}`,
		`{"uuid":"t1","status":"broadcasted","network":"ETH_MAINNET","from_address":"0xa","tx_hash":"0x1","confirmations":5,"required_confirmations":12}`,
		`{"uuid":"t1","status":"confirmed","network":"ETH_MAINNET","from_address":"0xa","tx_hash":"0x1","confirmations":13,"required_confirmations":12}`,
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i := int(calls.Add(1)) - 1
		if i >= len(steps) {
			i = len(steps) - 1
		}
		_, _ = w.Write([]byte(steps[i]))
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	final, err := WaitForTransaction(context.Background(), c, "t1", PollOptions{Interval: time.Millisecond, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("WaitForTransaction: %v", err)
	}
	if n := int(calls.Load()); n != len(steps) {
		t.Errorf("polls = %d, want %d", n, len(steps))
	}
	if !final.Succeeded() || final.Confirmations != 13 || final.RequiredConfirmations != 12 {
		t.Errorf("final = %s %d/%d, want confirmed 13/12", final.Status, final.Confirmations, final.RequiredConfirmations)
	}
}

// The payout webhook goes through the typed handler: the top-level count is
// typed, and the per-entry counts decode out of the raw sources and
// service_operations into the SDK's own types.
func TestPayoutWebhookEvent_Confirmations(t *testing.T) {
	const apiKey = "test_api_key_123"
	paid := `{"event":"payout.paid","uuid":"p1","order_id":"o1","user_id":"u1","status":"paid",
		"amount_requested":"10","amount_to_receive":"10","to_address":"0xdest",
		"sources":[
			{"address":"0xa","network":"ETH_MAINNET","coin":"USDT","amount_crypto":"6","txid":"0x1","confirmations":14},
			{"address":"0xb","network":"ETH_MAINNET","coin":"USDT","amount_crypto":"4","txid":"0x2","confirmations":12}
		],
		"service_operations":[{"type":"gas_refuel","context":"payout_prepare","status":"done","network":"ETH_MAINNET","coin":"ETH","amount_native":"0.001","from_address":"0xsvc","to_address":"0xb","txid":"0x0","confirmations":20}],
		"confirmations":12,"created_at":"2026-09-14T10:00:00Z","completed_at":"2026-09-14T10:05:00Z"}`

	evt := deliverWebhook[PayoutWebhookEvent](t, apiKey, paid)
	if got := evt.Confirmations; got == nil || *got != 12 {
		t.Errorf("confirmations = %v, want 12", derefInt(got))
	}
	var sources []PayoutSource
	if err := json.Unmarshal(evt.Sources, &sources); err != nil {
		t.Fatalf("decode sources: %v", err)
	}
	if len(sources) != 2 || sources[0].Confirmations == nil || *sources[0].Confirmations != 14 ||
		sources[1].Confirmations == nil || *sources[1].Confirmations != 12 {
		t.Errorf("sources = %+v", sources)
	}
	if len(sources) == 2 && (sources[0].TxID != "0x1" || sources[0].AmountCrypto != "6" ||
		sources[1].TxID != "0x2" || sources[1].AmountCrypto != "4" || sources[1].Network != ChainEthMainnet) {
		t.Errorf("sources txid/amount_crypto not decoded: %+v", sources)
	}
	var ops []PayoutServiceOperation
	if err := json.Unmarshal(evt.ServiceOperations, &ops); err != nil {
		t.Fatalf("decode service_operations: %v", err)
	}
	if len(ops) != 1 || ops[0].TxID != "0x0" || ops[0].Confirmations == nil || *ops[0].Confirmations != 20 {
		t.Errorf("service_operations = %+v", ops)
	}

	// A payout that failed before sending anything carries no count anywhere.
	failed := `{"event":"payout.system_fail","uuid":"p2","order_id":"o2","status":"system_fail",
		"sources":[{"address":"0xa","network":"ETH_MAINNET","coin":"ETH","amount_crypto":"1"}],
		"service_operations":[],"error_reason":"insufficient gas","created_at":"2026-09-14T10:00:00Z"}`
	evt = deliverWebhook[PayoutWebhookEvent](t, apiKey, failed)
	if evt.Confirmations != nil {
		t.Errorf("confirmations = %d, want absent", *evt.Confirmations)
	}
	sources = nil
	if err := json.Unmarshal(evt.Sources, &sources); err != nil {
		t.Fatalf("decode sources: %v", err)
	}
	if len(sources) != 1 || sources[0].Confirmations != nil {
		t.Errorf("sources = %+v, want one entry without a count", sources)
	}
}

// The transaction webhook body is the same view as /v1/transaction/info.
func TestTransactionWebhookEvent_Confirmations(t *testing.T) {
	const apiKey = "test_api_key_123"
	evt := deliverWebhook[TransactionWebhookEvent](t, apiKey, `{"event":"transaction.confirmed","uuid":"t1","status":"confirmed",
		"network":"TRON_MAINNET","chain_family":"TRON","type":"token","from_address":"Ta","to_address":"Tb","value":"1000000",
		"contract":"Tc","tx_hash":"abc","confirmations":19,"required_confirmations":19,
		"expires_at":"2026-09-14T10:00:45Z","created_at":"2026-09-14T10:00:00Z","completed_at":"2026-09-14T10:01:10Z"}`)
	if evt.Confirmations != 19 || evt.RequiredConfirmations != 19 {
		t.Errorf("confirmed = %d/%d, want 19/19", evt.Confirmations, evt.RequiredConfirmations)
	}

	evt = deliverWebhook[TransactionWebhookEvent](t, apiKey, `{"event":"transaction.expired","uuid":"t2","status":"expired",
		"network":"ETH_MAINNET","chain_family":"EVM","type":"native","from_address":"0xa","to_address":"0xb",
		"confirmations":0,"required_confirmations":12,"created_at":"2026-09-14T10:00:00Z","completed_at":"2026-09-14T10:10:00Z"}`)
	if evt.Confirmations != 0 || evt.RequiredConfirmations != 12 {
		t.Errorf("expired = %d/%d, want 0/12", evt.Confirmations, evt.RequiredConfirmations)
	}
}

// deliverWebhook signs body the way the platform does, passes it through
// WebhookHandler, and returns the event the handler received.
func deliverWebhook[T any](t *testing.T, apiKey, body string) T {
	t.Helper()
	var got T
	called := false
	h := WebhookHandler[T](apiKey, func(_ http.ResponseWriter, _ *http.Request, evt T) {
		called = true
		got = evt
	})
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header = signWebhook(t, apiKey, time.Now().Unix(), "dlv-1", []byte(body))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !called {
		t.Fatalf("webhook not delivered: status %d body=%s", rr.Code, rr.Body.String())
	}
	return got
}

func derefInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// A payout is paid only at the network's finality depth, and the depth rides on
// every public view of it. It is optional on the wire: absent decodes as 0 and
// must not be invented on re-marshal.
func TestPayout_RequiredConfirmations(t *testing.T) {
	var path, body string

	// A first Execute answers before anything is sent: no counts yet, but the
	// depth the payout will be held to is already known.
	first := captureServer(t, `{"uuid":"p1","order_id":"o1","status":"queue",
		"sources":[{"address":"0xa","network":"ETH_MAINNET","coin":"ETH","amount_crypto":"1"}],
		"required_confirmations":12,"created_at":"2026-09-14T10:00:00Z","completed_at":null}`, &path, &body)
	c, _ := New("m", "k", WithBaseURL(first.URL), WithRetries(0))
	queued, err := c.Payouts.Execute(context.Background(), &ExecutePayoutRequest{OrderID: "o1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if queued.RequiredConfirmations != 12 {
		t.Errorf("execute required_confirmations = %d, want 12", queued.RequiredConfirmations)
	}
	if queued.Confirmations != nil || queued.Sources[0].Confirmations != nil {
		t.Errorf("a queued payout carries a count: %+v", queued)
	}

	// In a block but not final: confirm_check, the count below the depth.
	info := captureServer(t, `{"uuid":"p1","order_id":"o1","status":"confirm_check",
		"sources":[{"address":"0xa","txid":"0x1","confirmations":4},{"address":"0xb","txid":"0x2","confirmations":13}],
		"confirmations":4,"required_confirmations":12}`, &path, &body)
	c, _ = New("m", "k", WithBaseURL(info.URL), WithRetries(0))
	confirming, err := c.Payouts.Info(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if confirming.Status != PayoutStatusConfirmCheck || confirming.IsTerminal() || confirming.Succeeded() {
		t.Errorf("status = %q, want confirm_check, not terminal, not succeeded", confirming.Status)
	}
	if got := confirming.Confirmations; got == nil || *got != 4 || confirming.RequiredConfirmations != 12 {
		t.Errorf("confirmations = %v/%d, want 4/12", derefInt(got), confirming.RequiredConfirmations)
	}

	// History: a payout with the depth and one without (an answer from a
	// platform that does not send it) decode side by side.
	hist := captureServer(t, `{"items":[
		{"uuid":"p1","status":"paid","sources":[{"address":"0xa","txid":"0x1","confirmations":12}],"confirmations":12,"required_confirmations":12},
		{"uuid":"p0","status":"paid","sources":[{"address":"0xc","txid":"0x0","confirmations":1}],"confirmations":1}
	],"meta":{"page":1,"page_size":20,"total":2}}`, &path, &body)
	c, _ = New("m", "k", WithBaseURL(hist.URL), WithRetries(0))
	page, err := c.Payouts.History(context.Background(), HistoryQuery{})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d", len(page.Items))
	}
	if page.Items[0].RequiredConfirmations != 12 {
		t.Errorf("items[0].required_confirmations = %d, want 12", page.Items[0].RequiredConfirmations)
	}
	if page.Items[1].RequiredConfirmations != 0 || !page.Items[1].Succeeded() {
		t.Errorf("items[1] = %+v, want paid without a depth", page.Items[1])
	}

	// Re-marshalling keeps a sent depth and does not invent an absent one.
	b, err := json.Marshal(page.Items[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"required_confirmations":12`) {
		t.Errorf("a sent depth was dropped on re-marshal: %s", b)
	}
	b, err = json.Marshal(page.Items[1])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"required_confirmations"`) {
		t.Errorf("an absent depth was invented on re-marshal: %s", b)
	}
}

// payout.paid carries the depth the payout was held to; a payout from before
// the platform waited for finality does not, and the event must still decode.
func TestPayoutWebhookEvent_RequiredConfirmations(t *testing.T) {
	const apiKey = "test_api_key_123"
	evt := deliverWebhook[PayoutWebhookEvent](t, apiKey, `{"event":"payout.paid","uuid":"p1","order_id":"o1","status":"paid",
		"sources":[{"address":"0xa","txid":"0x1","confirmations":19}],"service_operations":[],
		"confirmations":19,"required_confirmations":19,"created_at":"2026-09-14T10:00:00Z","completed_at":"2026-09-14T10:05:00Z"}`)
	if got := evt.Confirmations; got == nil || *got != 19 || evt.RequiredConfirmations != 19 {
		t.Errorf("paid = %v/%d, want 19/19", derefInt(got), evt.RequiredConfirmations)
	}

	evt = deliverWebhook[PayoutWebhookEvent](t, apiKey, `{"event":"payout.paid","uuid":"p0","order_id":"o0","status":"paid",
		"sources":[{"address":"0xa","txid":"0x1","confirmations":1}],"service_operations":[],
		"confirmations":1,"created_at":"2026-09-01T10:00:00Z","completed_at":"2026-09-01T10:01:00Z"}`)
	if evt.Status != PayoutStatusPaid || evt.RequiredConfirmations != 0 {
		t.Errorf("legacy paid = %q/%d, want paid without a depth", evt.Status, evt.RequiredConfirmations)
	}
	if got := evt.Confirmations; got == nil || *got != 1 {
		t.Errorf("legacy confirmations = %v, want 1", derefInt(got))
	}
}

// A sweep in transit is broadcasted with a rising count; only completed with a
// count above zero is settled. Every history item carries the network's depth,
// and a sweep the scanner saw as final without a block count reads exactly that
// depth.
func TestSweep_RequiredConfirmationsDecode(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"items":[
		{"task_id":"t1","status":"broadcasted","wallet_address":"0xa","chain":"ETH_MAINNET","sweep_confirmations":3,"required_confirmations":12},
		{"task_id":"t2","status":"completed","wallet_address":"0xa","chain":"ETH_MAINNET","sweep_confirmations":12,"required_confirmations":12},
		{"task_id":"t3","status":"completed","wallet_address":"0xa","chain":"SOLANA_MAINNET","sweep_confirmations":32,"required_confirmations":32},
		{"task_id":"t4","status":"broadcasted","wallet_address":"0xa","chain":"ETH_MAINNET","sweep_confirmations":0,"required_confirmations":12}
	],"meta":{"total":4,"page":1,"page_size":50}}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Sweeps.WalletHistory(context.Background(), SweepWalletHistoryQuery{Address: "0xa"})
	if err != nil {
		t.Fatalf("WalletHistory: %v", err)
	}
	if path != "/v1/sweeps/wallet/history" {
		t.Errorf("path = %q", path)
	}
	if len(out.Items) != 4 {
		t.Fatalf("items = %d", len(out.Items))
	}
	want := []struct {
		status        string
		confs, needed uint32
	}{
		{SweepStatusBroadcasted, 3, 12},
		{SweepStatusCompleted, 12, 12},
		{SweepStatusCompleted, 32, 32},
		{SweepStatusBroadcasted, 0, 12},
	}
	for i, w := range want {
		it := out.Items[i]
		if it.Status != w.status || it.SweepConfirmations != w.confs || it.RequiredConfirmations != w.needed {
			t.Errorf("items[%d] = %s %d/%d, want %s %d/%d", i,
				it.Status, it.SweepConfirmations, it.RequiredConfirmations, w.status, w.confs, w.needed)
		}
	}

	// An item without the field (an answer from a platform that does not send
	// it) still decodes, and re-marshalling does not invent it.
	var legacy Sweep
	if err := json.Unmarshal([]byte(`{"task_id":"t0","status":"completed","chain":"ETH_MAINNET","sweep_confirmations":1}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.RequiredConfirmations != 0 || legacy.SweepConfirmations != 1 || legacy.Status != SweepStatusCompleted {
		t.Errorf("legacy item = %+v", legacy)
	}
	b, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"required_confirmations"`) {
		t.Errorf("an absent depth was invented on re-marshal: %s", b)
	}
}

// Settled is completed with a count above zero. A completed history row with
// sweep_confirmations 0 was never observed on chain and is not settled; a
// broadcasted row with a count is not settled either.
func TestSweep_Settled(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"items":[
		{"task_id":"t1","status":"completed","wallet_address":"0xa","chain":"TRON_MAINNET","sweep_confirmations":0,"required_confirmations":12},
		{"task_id":"t2","status":"completed","wallet_address":"0xa","chain":"TRON_MAINNET","sweep_confirmations":12,"required_confirmations":12},
		{"task_id":"t3","status":"broadcasted","wallet_address":"0xa","chain":"TRON_MAINNET","sweep_confirmations":3,"required_confirmations":12},
		{"task_id":"t4","status":"completed","wallet_address":"0xa","chain":"TRON_MAINNET","required_confirmations":12},
		{"task_id":"t5","status":"failed","wallet_address":"0xa","chain":"TRON_MAINNET","sweep_confirmations":3,"required_confirmations":12}
	],"meta":{"total":5,"page":1,"page_size":50}}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Sweeps.History(context.Background(), SweepHistoryQuery{})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	want := []bool{false, true, false, false, false}
	if len(out.Items) != len(want) {
		t.Fatalf("items = %d", len(out.Items))
	}
	for i, w := range want {
		it := out.Items[i]
		if got := it.Settled(); got != w {
			t.Errorf("items[%d] %s %d/%d: Settled() = %v, want %v", i,
				it.Status, it.SweepConfirmations, it.RequiredConfirmations, got, w)
		}
	}
}

// sweep.confirmed carries both numbers; an event from a sweep service that
// predates finality has no required_confirmations and must still decode.
func TestSweepWebhookEvent_RequiredConfirmations(t *testing.T) {
	const apiKey = "test_api_key_123"
	evt := deliverWebhook[SweepWebhookEvent](t, apiKey, `{"event":"sweep.confirmed","task_id":"t2","status":"completed",
		"wallet_address":"0xb","to_address":"0xmaster","network":"ETH_MAINNET","chain_family":"EVM","asset_symbol":"USDT",
		"asset_contract":"0xdac17f958d2ee523a2206206994597c13d831ec7","asset_type":"token","amount_raw":"5000000","amount_human":"5",
		"sweep_tx_hash":"0xs","sweep_confirmations":14,"required_confirmations":12,
		"confirmed_at":"2026-09-14T10:04:00Z","type_work":"momentum","total_fee_usd":"0.91"}`)
	if evt.Event != SweepEventConfirmed || evt.Confirmations != 14 || evt.RequiredConfirmations != 12 {
		t.Errorf("event = %s %d/%d, want %s 14/12", evt.Event, evt.Confirmations, evt.RequiredConfirmations, SweepEventConfirmed)
	}
	if evt.TotalFeeUSD != "0.91" || evt.ConfirmedAt == "" {
		t.Errorf("event = %+v", evt)
	}

	evt = deliverWebhook[SweepWebhookEvent](t, apiKey, `{"event":"sweep.confirmed","task_id":"t1","status":"completed",
		"wallet_address":"0xa","network":"ETH_MAINNET","asset_symbol":"ETH","sweep_tx_hash":"0xs1","sweep_confirmations":1}`)
	if evt.Confirmations != 1 || evt.RequiredConfirmations != 0 {
		t.Errorf("legacy event = %d/%d, want 1/0", evt.Confirmations, evt.RequiredConfirmations)
	}
	b, err := json.Marshal(evt)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"required_confirmations"`) {
		t.Errorf("an absent depth was invented on re-marshal: %s", b)
	}
}

// A payout is paid only at the network's depth, so WaitForPayout without a
// Timeout waits 90 minutes; the other helpers keep 10 minutes. An explicit
// Timeout wins.
func TestWaitForPayout_DefaultTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/payout/") {
			_, _ = w.Write([]byte(`{"uuid":"p1","status":"confirm_check","confirmations":3,"required_confirmations":32}`))
			return
		}
		_, _ = w.Write([]byte(`{"uuid":"t1","status":"broadcasted","confirmations":3,"required_confirmations":32}`))
	}))
	t.Cleanup(srv.Close)
	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))

	wait := func(f func(ctx context.Context) error) error {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		return f(ctx)
	}

	err := wait(func(ctx context.Context) error {
		p, err := WaitForPayout(ctx, c, "p1", PollOptions{Interval: time.Millisecond})
		if p == nil || p.Status != PayoutStatusConfirmCheck {
			t.Errorf("last payout = %+v, want confirm_check", p)
		}
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "in 1h30m0s") {
		t.Errorf("WaitForPayout default timeout: err = %v, want 1h30m0s", err)
	}

	err = wait(func(ctx context.Context) error {
		_, err := WaitForPayout(ctx, c, "p1", PollOptions{Interval: time.Millisecond, Timeout: 7 * time.Minute})
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "in 7m0s") {
		t.Errorf("WaitForPayout explicit timeout: err = %v, want 7m0s", err)
	}

	err = wait(func(ctx context.Context) error {
		_, err := WaitForTransaction(ctx, c, "t1", PollOptions{Interval: time.Millisecond})
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "in 10m0s") {
		t.Errorf("WaitForTransaction default timeout: err = %v, want 10m0s", err)
	}
}

// The in-flight payout statuses the platform sends are not terminal; only paid
// succeeds.
func TestPayout_InFlightStatuses(t *testing.T) {
	inFlight := map[string]string{
		PayoutStatusQueue:           "queue",
		PayoutStatusRefueling:       "refueling",
		PayoutStatusRefuelConfirmed: "refuel_confirmed",
		PayoutStatusSending:         "sending",
		PayoutStatusBroadcasting:    "broadcasting",
		PayoutStatusInMempool:       "in_mempool",
		PayoutStatusConfirmCheck:    "confirm_check",
	}
	for c, want := range inFlight {
		if c != want {
			t.Errorf("constant = %q, want %q", c, want)
		}
		if p := (PayoutInfo{Status: c}); p.IsTerminal() || p.Succeeded() {
			t.Errorf("%q reads terminal=%v succeeded=%v", c, p.IsTerminal(), p.Succeeded())
		}
	}
	if len(inFlight) != 7 {
		t.Errorf("in-flight constants = %d distinct, want 7", len(inFlight))
	}
	if p := (PayoutInfo{Status: PayoutStatusSystemFail}); !p.IsTerminal() || p.Succeeded() {
		t.Errorf("system_fail = terminal %v succeeded %v", p.IsTerminal(), p.Succeeded())
	}
	if p := (PayoutInfo{Status: PayoutStatusPaid}); !p.IsTerminal() || !p.Succeeded() {
		t.Errorf("paid = terminal %v succeeded %v", p.IsTerminal(), p.Succeeded())
	}
}
