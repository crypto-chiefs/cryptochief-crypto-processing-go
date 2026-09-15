package cryptochief

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// A withdrawal in a block but not yet final reads confirm_check with its count
// below the depth; it is not terminal. Once the count reaches the depth it is
// completed, and the count stays on the record.
func TestWithdrawalInfo_ConfirmationsDecode(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{
		"uuid":"w1","status":"confirm_check",
		"from_address":"TFrom","to_address":"TTo","amount":"100.5","network":"TRON_MAINNET","coin":"USDT",
		"need_refuel":true,"refuel_tx_hash":"r1","refuel_status":"done",
		"tx_hash":"t1","confirmations":4,"required_confirmations":20,
		"estimated_fee_fiat":"1.20","fee_mode":"service","created_at":"2026-09-15T10:00:00Z"
	}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	w, err := c.Withdrawals.Info(context.Background(), "w1")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if path != "/v1/withdrawal/info" {
		t.Errorf("path = %q", path)
	}
	if !strings.Contains(body, `"uuid":"w1"`) {
		t.Errorf("body = %s", body)
	}
	if w.Status != WithdrawalStatusConfirmCheck || w.IsTerminal() || w.Succeeded() {
		t.Errorf("status = %q, want confirm_check, not terminal, not succeeded", w.Status)
	}
	if got := w.Confirmations; got == nil || *got != 4 || w.RequiredConfirmations != 20 {
		t.Errorf("confirmations = %v/%d, want 4/20", derefInt(got), w.RequiredConfirmations)
	}
	if !w.NeedRefuel || w.RefuelTxHash != "r1" || w.RefuelStatus != "done" || w.FeeMode != "service" ||
		w.EstimatedFeeFiat != "1.20" || w.TxHash != "t1" || w.Network != ChainTronMainnet {
		t.Errorf("withdrawal = %+v", w)
	}
	if w.CompletedAt != "" {
		t.Errorf("completed_at = %q on a withdrawal still confirming", w.CompletedAt)
	}

	done := captureServer(t, `{
		"uuid":"w1","status":"completed","amount":"100.5","network":"TRON_MAINNET","coin":"USDT",
		"need_refuel":false,"tx_hash":"t1","confirmations":20,"required_confirmations":20,
		"estimated_fee_fiat":"1.20","actual_fee_fiat":"1.18","fee_mode":"service",
		"created_at":"2026-09-15T10:00:00Z","completed_at":"2026-09-15T10:02:30Z"
	}`, &path, &body)
	c, _ = New("m", "k", WithBaseURL(done.URL), WithRetries(0))
	w, err = c.Withdrawals.Info(context.Background(), "w1")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if !w.IsTerminal() || !w.Succeeded() {
		t.Errorf("status = %q, want completed, terminal, succeeded", w.Status)
	}
	if got := w.Confirmations; got == nil || *got != 20 || w.RequiredConfirmations != 20 {
		t.Errorf("confirmations = %v/%d, want 20/20", derefInt(got), w.RequiredConfirmations)
	}
	if w.ActualFeeFiat != "1.18" || w.CompletedAt != "2026-09-15T10:02:30Z" {
		t.Errorf("withdrawal = %+v", w)
	}
}

// Confirmations is optional, and absent is not 0: a queued withdrawal has sent
// nothing, while a count of 0 says the count was recorded. Decoding into a plain
// int would make the two indistinguishable.
func TestWithdrawal_ConfirmationsAbsentIsNotZero(t *testing.T) {
	var queued Withdrawal
	if err := json.Unmarshal([]byte(`{"uuid":"w2","status":"queue","amount":"1","network":"ETH_MAINNET",
		"need_refuel":false,"required_confirmations":12,"estimated_fee_fiat":"0.40","fee_mode":"client",
		"created_at":"2026-09-15T10:00:00Z"}`), &queued); err != nil {
		t.Fatal(err)
	}
	if queued.Confirmations != nil {
		t.Errorf("confirmations = %d, want absent", *queued.Confirmations)
	}
	if queued.RequiredConfirmations != 12 {
		t.Errorf("required_confirmations = %d, want 12 even before sending", queued.RequiredConfirmations)
	}

	var zero Withdrawal
	if err := json.Unmarshal([]byte(`{"uuid":"w3","status":"confirm_check","tx_hash":"0x1",
		"confirmations":0,"required_confirmations":12}`), &zero); err != nil {
		t.Fatal(err)
	}
	if zero.Confirmations == nil || *zero.Confirmations != 0 {
		t.Errorf("confirmations = %v, want 0", derefInt(zero.Confirmations))
	}

	// Re-marshalling keeps the distinction in both directions.
	b, err := json.Marshal(zero)
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

// History items are the same view as Info. The page mixes every shape a
// merchant meets: a completed withdrawal with its count, one completed under the
// old first-block rule (no count), a failed one with its reason, and a BTC
// withdrawal still in the mempool.
func TestWithdrawalHistory_CarriesConfirmations(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"items":[
		{"uuid":"w1","status":"completed","network":"ETH_MAINNET","coin":"USDT","amount":"100","tx_hash":"0x1",
			"confirmations":14,"required_confirmations":12,"created_at":"2026-09-15T10:00:00Z","completed_at":"2026-09-15T10:04:00Z"},
		{"uuid":"w0","status":"completed","network":"ETH_MAINNET","coin":"USDT","amount":"5","tx_hash":"0x0",
			"required_confirmations":12,"created_at":"2026-08-01T10:00:00Z","completed_at":"2026-08-01T10:00:40Z"},
		{"uuid":"w9","status":"failed","network":"BSC_MAINNET","coin":"BNB","amount":"50",
			"error_reason":"TX_CONFIRM_TIMEOUT","required_confirmations":15,"created_at":"2026-09-14T15:00:00Z"},
		{"uuid":"w8","status":"in_mempool","network":"BTC_MAINNET","coin":"BTC","amount":"0.01","tx_hash":"abc",
			"required_confirmations":2,"created_at":"2026-09-15T09:00:00Z"}
	],"meta":{"page":1,"page_size":20,"total":4,"total_pages":1}}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	page, err := c.Withdrawals.History(context.Background(), HistoryQuery{PageSize: 20})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if path != "/v1/withdrawal/history" {
		t.Errorf("path = %q", path)
	}
	if len(page.Items) != 4 || page.Meta.Total != 4 || page.Meta.TotalPages != 1 {
		t.Fatalf("page = %+v", page)
	}

	settled := page.Items[0]
	if got := settled.Confirmations; !settled.Succeeded() || got == nil || *got != 14 || settled.RequiredConfirmations != 12 {
		t.Errorf("items[0] = %s %v/%d, want completed 14/12", settled.Status, derefInt(got), settled.RequiredConfirmations)
	}

	// Completed before the platform waited for finality: no count, and still
	// settled - decide on Status.
	legacy := page.Items[1]
	if !legacy.Succeeded() || legacy.Confirmations != nil || legacy.RequiredConfirmations != 12 {
		t.Errorf("items[1] = %s %v/%d, want completed without a count", legacy.Status, derefInt(legacy.Confirmations), legacy.RequiredConfirmations)
	}

	failed := page.Items[2]
	if failed.Status != WithdrawalStatusFailed || !failed.IsTerminal() || failed.Succeeded() {
		t.Errorf("items[2] status = %q, want failed, terminal", failed.Status)
	}
	if failed.ErrorReason != "TX_CONFIRM_TIMEOUT" || failed.CompletedAt != "" || failed.Confirmations != nil {
		t.Errorf("items[2] = %+v", failed)
	}

	mempool := page.Items[3]
	if mempool.Status != WithdrawalStatusInMempool || mempool.IsTerminal() || mempool.Confirmations != nil || mempool.RequiredConfirmations != 2 {
		t.Errorf("items[3] = %+v", mempool)
	}
}

// The status set is the platform's own. The payout names must not creep in:
// a withdrawal is never "paid" or "system_fail", and neither is terminal here.
func TestWithdrawal_StatusSet(t *testing.T) {
	terminal := map[string]bool{
		WithdrawalStatusQueue:           false,
		WithdrawalStatusRefueling:       false,
		WithdrawalStatusRefuelConfirmed: false,
		WithdrawalStatusSending:         false,
		WithdrawalStatusBroadcasting:    false,
		WithdrawalStatusInMempool:       false,
		WithdrawalStatusConfirmCheck:    false,
		WithdrawalStatusCompleted:       true,
		WithdrawalStatusFailed:          true,
	}
	want := []string{"queue", "refueling", "refuel_confirmed", "sending", "broadcasting",
		"in_mempool", "confirm_check", "completed", "failed"}
	if len(terminal) != len(want) {
		t.Fatalf("status constants = %d, want %d distinct", len(terminal), len(want))
	}
	for _, s := range want {
		isTerminal, ok := terminal[s]
		if !ok {
			t.Errorf("no constant for %q", s)
			continue
		}
		if got := (Withdrawal{Status: s}).IsTerminal(); got != isTerminal {
			t.Errorf("IsTerminal(%q) = %v, want %v", s, got, isTerminal)
		}
	}
	for _, s := range []string{"paid", "system_fail"} {
		w := Withdrawal{Status: s}
		if w.IsTerminal() || w.Succeeded() {
			t.Errorf("%q is treated as a withdrawal outcome", s)
		}
	}
	// The deprecated constant is not produced by the API; it stays terminal so
	// code that references it keeps its meaning.
	if w := (Withdrawal{Status: WithdrawalStatusCancelled}); !w.IsTerminal() || w.Succeeded() {
		t.Errorf("deprecated cancelled: terminal=%v succeeded=%v", w.IsTerminal(), w.Succeeded())
	}
}

// A response from a platform that does not send required_confirmations still
// decodes, and re-marshalling does not invent the field.
func TestWithdrawal_RequiredConfirmationsAbsent(t *testing.T) {
	var w Withdrawal
	if err := json.Unmarshal([]byte(`{"uuid":"w0","status":"completed","network":"ETH_MAINNET","amount":"1","tx_hash":"0x0"}`), &w); err != nil {
		t.Fatal(err)
	}
	if w.RequiredConfirmations != 0 || w.Confirmations != nil || !w.Succeeded() {
		t.Errorf("withdrawal = %+v", w)
	}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"required_confirmations"`) {
		t.Errorf("an absent depth was invented on re-marshal: %s", b)
	}
}
