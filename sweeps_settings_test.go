package cryptochief

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The wire shape of a settings read. An empty Address is a legitimate question -
// "what is the project's own default" - so it must stay off the wire rather
// than going out as an empty string the platform would read as an address.
func TestSweepSettings_ReadWireShape(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{
		"wallet_address":"0xabc","network_code":"ETH_MAINNET",
		"effective":{"type_work":"threshold","threshold_amount_usd":"250","fee_mode":"mix","source":"wallet"},
		"override":{"network_code":"","type_work":"threshold","threshold_amount_usd":"250","fee_mode":null,"source":"merchant","locked":false},
		"project_default":{"type_work":"momentum","fee_mode":"client"}
	}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Sweeps.Settings(context.Background(), SweepSettingsQuery{Address: "0xabc"})
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if path != "/v1/sweeps/settings" {
		t.Errorf("path = %q", path)
	}
	if strings.Contains(body, "network_code") {
		t.Errorf("empty network_code must be omitted, body = %s", body)
	}

	if out.Effective.TypeWork != SweepModeThreshold || out.Effective.ThresholdUSD != "250" {
		t.Errorf("effective = %+v", out.Effective)
	}
	if out.Effective.Source != "wallet" {
		t.Errorf("effective source = %q, want the layer the mode came from", out.Effective.Source)
	}
	// The three layers must stay distinguishable: inherited fee mode reads as
	// nil on the override while the effective policy still has a value.
	if out.Override == nil || out.Override.FeeMode != nil {
		t.Errorf("override = %+v, want a non-nil override with fee_mode inherited", out.Override)
	}
	if out.Override.TypeWork == nil || *out.Override.TypeWork != SweepModeThreshold {
		t.Errorf("override type_work = %v", out.Override.TypeWork)
	}
	if out.ProjectDefault.TypeWork != SweepModeMomentum {
		t.Errorf("project default = %+v", out.ProjectDefault)
	}
}

// Writing one field must not restate the others, and clearing one is expressed
// by naming it in fields with no value - the distinction the whole Fields
// mechanism exists for.
func TestSweepSettings_UpdateWireShape(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"effective":{"type_work":"momentum","fee_mode":"mix"},"override":null,"project_default":{"type_work":"momentum","fee_mode":"mix"}}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	mode := SweepModeThreshold
	threshold := "500"
	if _, err := c.Sweeps.UpdateSettings(context.Background(), SweepSettingsUpdate{
		Address:      "0xabc",
		TypeWork:     &mode,
		ThresholdUSD: &threshold,
	}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if path != "/v1/sweeps/settings/update" {
		t.Errorf("path = %q", path)
	}

	var sent map[string]any
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatalf("body is not json: %v (%s)", err, body)
	}
	if sent["type_work"] != SweepModeThreshold || sent["threshold_amount_usd"] != "500" {
		t.Errorf("sent = %v", sent)
	}
	// fee_mode was not being written; sending it as null would clear it.
	if _, present := sent["fee_mode"]; present {
		t.Errorf("untouched fee_mode must not go on the wire, body = %s", body)
	}

	// Clearing: named in fields, no value.
	var clearBody string
	srv2 := captureServer(t, `{"effective":{"type_work":"momentum","fee_mode":"mix"},"override":null,"project_default":{"type_work":"momentum","fee_mode":"mix"}}`, &path, &clearBody)
	c2, _ := New("m", "k", WithBaseURL(srv2.URL), WithRetries(0))
	if _, err := c2.Sweeps.UpdateSettings(context.Background(), SweepSettingsUpdate{
		Address: "0xabc",
		Fields:  []string{"type_work"},
	}); err != nil {
		t.Fatalf("UpdateSettings clear: %v", err)
	}
	var cleared map[string]any
	if err := json.Unmarshal([]byte(clearBody), &cleared); err != nil {
		t.Fatalf("body is not json: %v (%s)", err, clearBody)
	}
	if _, present := cleared["type_work"]; present {
		t.Errorf("a cleared field must carry no value, body = %s", clearBody)
	}
	fields, _ := cleared["fields"].([]any)
	if len(fields) != 1 || fields[0] != "type_work" {
		t.Errorf("fields = %v, want the cleared field named", cleared["fields"])
	}
}

// A sweep is broadcast first and confirmed after. Both facts must survive
// decoding, or a caller cannot tell a sent sweep from a settled one - which is
// exactly what the old optimistic "completed" hid.
func TestSweep_ConfirmationFieldsDecode(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"items":[
		{"task_id":"t1","status":"broadcasted","wallet_address":"0xa","chain":"ETH_MAINNET","sweep_confirmations":2,"type_work":"threshold","total_fee_usd":"1.20"},
		{"task_id":"t2","status":"completed","wallet_address":"0xb","chain":"ETH_MAINNET","sweep_confirmations":12,"completed_at":"2026-08-28T10:00:00Z","real_sweep_fee_usd":"0.98"}
	],"meta":{"total":2,"page":1,"page_size":50}}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Sweeps.History(context.Background(), SweepHistoryQuery{PageSize: 50})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("items = %d", len(out.Items))
	}
	if out.Items[0].Status != SweepStatusBroadcasted || out.Items[0].SweepConfirmations != 2 {
		t.Errorf("broadcast item = %+v", out.Items[0])
	}
	if out.Items[0].CompletedAt != "" {
		t.Errorf("a sweep still in flight must not carry completed_at, got %q", out.Items[0].CompletedAt)
	}
	if out.Items[0].TypeWork != "threshold" || out.Items[0].TotalFeeUSD != "1.20" {
		t.Errorf("type_work/total_fee_usd not decoded: %+v", out.Items[0])
	}
	if out.Items[1].Status != SweepStatusCompleted || out.Items[1].CompletedAt == "" {
		t.Errorf("confirmed item = %+v", out.Items[1])
	}
	if out.Items[1].RealSweepFeeUSD != "0.98" {
		t.Errorf("real fee not decoded: %+v", out.Items[1])
	}
}

// gas_source appears in all three layers and means different things in each: a
// null override says "this layer does not decide", while the effective value is
// always concrete. Collapsing the two would report an inherited "rented" wallet
// as one nobody is renting energy for.
func TestSweepSettings_GasSourceLayersDecode(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{
		"wallet_address":"TQrY8b","network_code":"TRON_MAINNET",
		"effective":{"type_work":"momentum","fee_mode":"mix","gas_source":"rented","source":"project"},
		"override":{"network_code":"","type_work":"momentum","threshold_amount_usd":null,"fee_mode":null,"gas_source":null,"source":"merchant","locked":false},
		"project_default":{"type_work":"momentum","fee_mode":"mix","gas_source":"rented"}
	}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Sweeps.Settings(context.Background(), SweepSettingsQuery{
		Address:     "TQrY8b",
		NetworkCode: ChainTronMainnet,
	})
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if path != "/v1/sweeps/settings" {
		t.Errorf("path = %q", path)
	}

	// The effective layer is what will actually happen, and it is never absent.
	if out.Effective.GasSource != SweepGasSourceRented {
		t.Errorf("effective gas_source = %q, want the concrete value the platform resolved", out.Effective.GasSource)
	}
	// A null in the override is "not decided", not "switched off". The wallet is
	// still having energy rented for it - by the project, one layer down.
	if out.Override == nil {
		t.Fatalf("override missing")
	}
	if out.Override.GasSource != nil {
		t.Errorf("override gas_source = %q, want nil for an inherited value", *out.Override.GasSource)
	}
	if out.ProjectDefault.GasSource != SweepGasSourceRented {
		t.Errorf("project default gas_source = %q", out.ProjectDefault.GasSource)
	}

	// A wallet that decides for itself reads back as a value, not a nil - that is
	// the whole difference between the two layers.
	var ownBody string
	srv2 := captureServer(t, `{
		"wallet_address":"TQrY8b","network_code":"",
		"effective":{"type_work":"momentum","fee_mode":"mix","gas_source":"native","source":"wallet"},
		"override":{"network_code":"","type_work":null,"threshold_amount_usd":null,"fee_mode":null,"gas_source":"native","source":"merchant","locked":false},
		"project_default":{"type_work":"momentum","fee_mode":"mix","gas_source":"rented"}
	}`, &path, &ownBody)
	c2, _ := New("m", "k", WithBaseURL(srv2.URL), WithRetries(0))
	own, err := c2.Sweeps.Settings(context.Background(), SweepSettingsQuery{Address: "TQrY8b"})
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if own.Override == nil || own.Override.GasSource == nil || *own.Override.GasSource != SweepGasSourceNative {
		t.Fatalf("override gas_source = %v, want the wallet's own value", own.Override.GasSource)
	}
	if own.Effective.GasSource != SweepGasSourceNative || own.Effective.Source != "wallet" {
		t.Errorf("effective = %+v", own.Effective)
	}
}

// Writing gas_source and clearing it are two different calls: a value goes on
// the wire, and dropping the override means naming the field in fields with no
// value. Omitting it entirely leaves the stored value alone - which is NOT the
// same as choosing "native", because an unset wallet is rented by default.
func TestSweepSettings_GasSourceWireShape(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"effective":{"type_work":"momentum","fee_mode":"mix","gas_source":"native","source":"wallet"},"override":null,"project_default":{"type_work":"momentum","fee_mode":"mix","gas_source":"rented"}}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	gas := SweepGasSourceNative
	out, err := c.Sweeps.UpdateSettings(context.Background(), SweepSettingsUpdate{
		Address:     "TQrY8b",
		NetworkCode: ChainTronMainnet,
		GasSource:   &gas,
	})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if path != "/v1/sweeps/settings/update" {
		t.Errorf("path = %q", path)
	}

	var sent map[string]any
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatalf("body is not json: %v (%s)", err, body)
	}
	if sent["gas_source"] != SweepGasSourceNative {
		t.Errorf("gas_source = %v, want the opt-out sent explicitly", sent["gas_source"])
	}
	// Nothing else was being written, so nothing else may go on the wire.
	if _, present := sent["fee_mode"]; present {
		t.Errorf("untouched fee_mode must not go on the wire, body = %s", body)
	}
	if out.Effective.GasSource != SweepGasSourceNative {
		t.Errorf("effective gas_source = %q, want the write echoed back", out.Effective.GasSource)
	}

	// Clearing: named in fields, no value. That is the only way to drop this one
	// override and keep the rest.
	var clearBody string
	srv2 := captureServer(t, `{"effective":{"type_work":"momentum","fee_mode":"mix","gas_source":"rented","source":"project"},"override":{"network_code":"","type_work":"momentum","threshold_amount_usd":null,"fee_mode":null,"gas_source":null,"source":"merchant","locked":false},"project_default":{"type_work":"momentum","fee_mode":"mix","gas_source":"rented"}}`, &path, &clearBody)
	c2, _ := New("m", "k", WithBaseURL(srv2.URL), WithRetries(0))
	cleared, err := c2.Sweeps.UpdateSettings(context.Background(), SweepSettingsUpdate{
		Address: "TQrY8b",
		Fields:  []string{"gas_source"},
	})
	if err != nil {
		t.Fatalf("UpdateSettings clear: %v", err)
	}
	var sentClear map[string]any
	if err := json.Unmarshal([]byte(clearBody), &sentClear); err != nil {
		t.Fatalf("body is not json: %v (%s)", err, clearBody)
	}
	if _, present := sentClear["gas_source"]; present {
		t.Errorf("a cleared field must carry no value, body = %s", clearBody)
	}
	fields, _ := sentClear["fields"].([]any)
	if len(fields) != 1 || fields[0] != "gas_source" {
		t.Errorf("fields = %v, want gas_source named", sentClear["fields"])
	}
	// Cleared means inherited: the override stops deciding and the effective
	// value falls back to the project's - not to "off".
	if cleared.Override == nil || cleared.Override.GasSource != nil {
		t.Errorf("override = %+v, want gas_source back to inherited", cleared.Override)
	}
	if cleared.Effective.GasSource != SweepGasSourceRented {
		t.Errorf("effective gas_source = %q, want the inherited value", cleared.Effective.GasSource)
	}
}

// status and search must reach both history endpoints under those names, and
// must stay off the wire when unset: an empty status would narrow the page to
// nothing instead of including every status.
func TestSweepHistory_StatusAndSearchWireShape(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"items":[],"meta":{"page":1,"page_size":20,"total":0}}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	if _, err := c.Sweeps.History(context.Background(), SweepHistoryQuery{
		Status: SweepStatusSkipped,
		Search: "0x77EDde",
	}); err != nil {
		t.Fatalf("History: %v", err)
	}
	if path != "/v1/sweeps/history" {
		t.Errorf("path = %q", path)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatalf("body is not json: %v (%s)", err, body)
	}
	if sent["status"] != SweepStatusSkipped || sent["search"] != "0x77EDde" {
		t.Errorf("sent = %v", sent)
	}
	if _, present := sent["mode"]; present {
		t.Errorf("an unset mode must not go on the wire, body = %s", body)
	}

	// The wallet variant takes the same two filters alongside its required
	// address.
	var walletBody string
	srv2 := captureServer(t, `{"items":[],"meta":{"page":1,"page_size":20,"total":0}}`, &path, &walletBody)
	c2, _ := New("m", "k", WithBaseURL(srv2.URL), WithRetries(0))
	if _, err := c2.Sweeps.WalletHistory(context.Background(), SweepWalletHistoryQuery{
		Address: "0xabc",
		Mode:    SweepModeForce,
		Status:  SweepStatusCompleted,
		Search:  "898cdbd0",
	}); err != nil {
		t.Fatalf("WalletHistory: %v", err)
	}
	if path != "/v1/sweeps/wallet/history" {
		t.Errorf("path = %q", path)
	}
	var sentWallet map[string]any
	if err := json.Unmarshal([]byte(walletBody), &sentWallet); err != nil {
		t.Fatalf("body is not json: %v (%s)", err, walletBody)
	}
	if sentWallet["address"] != "0xabc" || sentWallet["mode"] != string(SweepModeForce) {
		t.Errorf("sent = %v", sentWallet)
	}
	if sentWallet["status"] != SweepStatusCompleted || sentWallet["search"] != "898cdbd0" {
		t.Errorf("sent = %v", sentWallet)
	}

	// Unset, neither filter appears - the page then includes every status,
	// skipped ones among them.
	var bareBody string
	srv3 := captureServer(t, `{"items":[],"meta":{"page":1,"page_size":20,"total":0}}`, &path, &bareBody)
	c3, _ := New("m", "k", WithBaseURL(srv3.URL), WithRetries(0))
	if _, err := c3.Sweeps.History(context.Background(), SweepHistoryQuery{PageSize: 20}); err != nil {
		t.Fatalf("History: %v", err)
	}
	if strings.Contains(bareBody, "status") || strings.Contains(bareBody, "search") {
		t.Errorf("unset filters must be omitted: %s", bareBody)
	}
}

// The environment hint must reach the platform, and must stay absent when not
// set: an empty string would be a value the platform has to reject rather than
// the "use the project default" the caller meant.
func TestPayIn_EnvironmentWireShape(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"uuid":"u1","status":"pending"}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	if _, err := c.PayIns.Create(context.Background(), &CreatePayInRequest{
		OrderID: "o1", UserID: "u", Mode: PayInModeFiat,
		AmountFiat: "10", Currency: "USD",
		Environment: EnvironmentTestnet,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.Contains(body, `"environment":"testnet"`) {
		t.Errorf("environment missing from body: %s", body)
	}

	var noEnvBody string
	srv2 := captureServer(t, `{"uuid":"u2","status":"pending"}`, &path, &noEnvBody)
	c2, _ := New("m", "k", WithBaseURL(srv2.URL), WithRetries(0))
	if _, err := c2.PayIns.Create(context.Background(), &CreatePayInRequest{
		OrderID: "o2", UserID: "u", Mode: PayInModeFiat, AmountFiat: "10", Currency: "USD",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if strings.Contains(noEnvBody, "environment") {
		t.Errorf("unset environment must be omitted: %s", noEnvBody)
	}
}
