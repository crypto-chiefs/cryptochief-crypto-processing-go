package cryptochief

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// A label is optional in the API and optional on the wire: set, it must reach
// the platform under "label"; unset, it must stay off the body entirely rather
// than going out as an empty string the platform would store as a name.
func TestWalletGenerate_LabelWireShape(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"type":"static","address":"0xdead","chain_family":"EVM","frozen":false,"master_wallet_address":"0xmaster","callback_url":null,"label":"customer 4242"}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	w, err := c.Wallets.Generate(context.Background(), &GenerateWalletRequest{
		WalletType:          WalletTypeStatic,
		ChainFamily:         FamilyEVM,
		MasterWalletAddress: "0xmaster",
		Label:               "customer 4242",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if path != "/v1/wallets/generate" {
		t.Errorf("path = %q", path)
	}
	if !strings.Contains(body, `"label":"customer 4242"`) {
		t.Errorf("label missing from body: %s", body)
	}
	// The name comes back with the wallet, so a bulk creator can log what it
	// just made without a second round trip.
	if w.Label != "customer 4242" {
		t.Errorf("wallet label = %q, want the label just sent", w.Label)
	}

	var noLabelBody string
	srv2 := captureServer(t, `{"type":"master","address":"0xbeef","chain_family":"EVM","frozen":false,"master_wallet_address":null,"callback_url":null,"label":null}`, &path, &noLabelBody)
	c2, _ := New("m", "k", WithBaseURL(srv2.URL), WithRetries(0))
	w2, err := c2.Wallets.Generate(context.Background(), &GenerateWalletRequest{
		WalletType:  WalletTypeMaster,
		ChainFamily: FamilyEVM,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Contains(noLabelBody, "label") {
		t.Errorf("unset label must be omitted: %s", noLabelBody)
	}
	if w2.Label != "" {
		t.Errorf("an unnamed wallet must read as unnamed, got %q", w2.Label)
	}
}

// Re-binding carries exactly two fields, both required and both named the way
// the platform names them. A misspelt master field would silently rebind
// nothing.
func TestWalletRebindMaster_WireShape(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"type":"transit","address":"0xdead","chain_family":"EVM","frozen":false,"master_wallet_address":"0xnew","callback_url":null}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	w, err := c.Wallets.RebindMaster(context.Background(), "0xdead", "0xnew")
	if err != nil {
		t.Fatalf("RebindMaster: %v", err)
	}
	if path != "/v1/wallets/rebind-master" {
		t.Errorf("path = %q", path)
	}

	var sent map[string]any
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatalf("body is not json: %v (%s)", err, body)
	}
	if len(sent) != 2 {
		t.Errorf("body must carry address and master_wallet_address only, got %v", sent)
	}
	if sent["address"] != "0xdead" || sent["master_wallet_address"] != "0xnew" {
		t.Errorf("sent = %v", sent)
	}

	// The answer is the wallet-info shape: the new master must be readable
	// without a second round trip.
	if w.Type != WalletTypeTransit || w.MasterWalletAddress != "0xnew" {
		t.Errorf("wallet = %+v", w)
	}
	if w.CallbackURL != "" {
		t.Errorf("a transit wallet has no callback, got %q", w.CallbackURL)
	}
}

// Clearing a webhook is an empty string, not an absent field: if the SDK
// omitted it the platform would see no instruction and the old URL would keep
// receiving deposits.
func TestWalletCallbackURL_EmptyIsSentNotOmitted(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"type":"static","address":"0xdead","chain_family":"EVM","frozen":false,"master_wallet_address":"0xmaster","callback_url":null}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	w, err := c.Wallets.ClearCallbackURL(context.Background(), "0xdead")
	if err != nil {
		t.Fatalf("ClearCallbackURL: %v", err)
	}
	if path != "/v1/wallets/callback-url" {
		t.Errorf("path = %q", path)
	}

	var cleared map[string]any
	if err := json.Unmarshal([]byte(body), &cleared); err != nil {
		t.Fatalf("body is not json: %v (%s)", err, body)
	}
	got, present := cleared["callback_url"]
	if !present {
		t.Fatalf("callback_url must go on the wire to clear it, body = %s", body)
	}
	if got != "" {
		t.Errorf("callback_url = %v, want an empty string", got)
	}
	if cleared["address"] != "0xdead" {
		t.Errorf("sent = %v", cleared)
	}
	if w.CallbackURL != "" {
		t.Errorf("cleared wallet still reports a callback: %q", w.CallbackURL)
	}

	// Setting one is the same call with a value.
	var setBody string
	srv2 := captureServer(t, `{"type":"static","address":"0xdead","chain_family":"EVM","frozen":false,"master_wallet_address":"0xmaster","callback_url":"https://example.test/hook"}`, &path, &setBody)
	c2, _ := New("m", "k", WithBaseURL(srv2.URL), WithRetries(0))
	w2, err := c2.Wallets.SetCallbackURL(context.Background(), "0xdead", "https://example.test/hook")
	if err != nil {
		t.Fatalf("SetCallbackURL: %v", err)
	}
	if !strings.Contains(setBody, `"callback_url":"https://example.test/hook"`) {
		t.Errorf("callback_url missing from body: %s", setBody)
	}
	if w2.CallbackURL != "https://example.test/hook" {
		t.Errorf("wallet = %+v", w2)
	}
}

// Renaming carries exactly two fields, and an empty label is the way a name is
// removed: if the SDK omitted it the platform would see no instruction and the
// old name would stay on the wallet.
func TestWalletLabel_EmptyIsSentNotOmitted(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"type":"master","address":"0xbeef","chain_family":"EVM","frozen":false,"master_wallet_address":null,"callback_url":null,"label":null}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	w, err := c.Wallets.ClearLabel(context.Background(), "0xbeef")
	if err != nil {
		t.Fatalf("ClearLabel: %v", err)
	}
	if path != "/v1/wallets/label" {
		t.Errorf("path = %q", path)
	}

	var cleared map[string]any
	if err := json.Unmarshal([]byte(body), &cleared); err != nil {
		t.Fatalf("body is not json: %v (%s)", err, body)
	}
	if len(cleared) != 2 {
		t.Errorf("body must carry address and label only, got %v", cleared)
	}
	got, present := cleared["label"]
	if !present {
		t.Fatalf("label must go on the wire to clear it, body = %s", body)
	}
	if got != "" {
		t.Errorf("label = %v, want an empty string", got)
	}
	if cleared["address"] != "0xbeef" {
		t.Errorf("sent = %v", cleared)
	}
	if w.Label != "" {
		t.Errorf("cleared wallet still reports a name: %q", w.Label)
	}

	// Setting one is the same call with a value — and it names any wallet
	// type, a master included, unlike the deposit webhook.
	var setBody string
	srv2 := captureServer(t, `{"type":"master","address":"0xbeef","chain_family":"EVM","frozen":false,"master_wallet_address":null,"callback_url":null,"label":"treasury EVM"}`, &path, &setBody)
	c2, _ := New("m", "k", WithBaseURL(srv2.URL), WithRetries(0))
	w2, err := c2.Wallets.SetLabel(context.Background(), "0xbeef", "treasury EVM")
	if err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	if !strings.Contains(setBody, `"label":"treasury EVM"`) {
		t.Errorf("label missing from body: %s", setBody)
	}
	if w2.Label != "treasury EVM" || w2.Type != WalletTypeMaster {
		t.Errorf("wallet = %+v", w2)
	}
}

// The pay-ins of one deposit address are ordinary orders in the ordinary paged
// envelope: the address must reach the platform, the optional filters must stay
// off the wire when unset, and the rows must decode as PayIn records rather than
// into a second, parallel order type.
func TestWalletPayInHistory_WireShapeAndDecode(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"items":[
		{"uuid":"0a1b2c3d-4e5f-6789-abcd-ef0123456789","order_id":"invoice-1002","status":"paid","amount_crypto":"10.5","payment_coin":"USDT","payment_network":"TRON_MAINNET","to_address":"TQrY8bYc2yQ8sM8nJ1sZ9c2Zx7L2wq7pQb"}
	],"meta":{"page":1,"page_size":20,"total":1}}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Wallets.PayInHistory(context.Background(), WalletPayInHistoryQuery{
		Address: "TQrY8bYc2yQ8sM8nJ1sZ9c2Zx7L2wq7pQb",
	})
	if err != nil {
		t.Fatalf("PayInHistory: %v", err)
	}
	if path != "/v1/wallets/history" {
		t.Errorf("path = %q", path)
	}

	var sent map[string]any
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatalf("body is not json: %v (%s)", err, body)
	}
	if len(sent) != 1 || sent["address"] != "TQrY8bYc2yQ8sM8nJ1sZ9c2Zx7L2wq7pQb" {
		t.Errorf("body must carry the address alone when nothing else is set, got %v", sent)
	}

	if len(out.Items) != 1 {
		t.Fatalf("items = %d", len(out.Items))
	}
	p := out.Items[0]
	if p.OrderID != "invoice-1002" || p.Status != PayInStatusPaid || !p.Succeeded() {
		t.Errorf("order = %+v, want the pay-in record the history endpoints return", p)
	}
	if p.PaymentNetwork != ChainTronMainnet || p.AmountCrypto != "10.5" {
		t.Errorf("order = %+v", p)
	}
	if out.Meta.Page != 1 || out.Meta.PageSize != 20 || out.Meta.Total != 1 {
		t.Errorf("meta = %+v", out.Meta)
	}

	// The date bounds and the pagination go on the wire under the platform's own
	// names when they are set.
	var filteredBody string
	srv2 := captureServer(t, `{"items":[],"meta":{"page":2,"page_size":50,"total":0}}`, &path, &filteredBody)
	c2, _ := New("m", "k", WithBaseURL(srv2.URL), WithRetries(0))
	page, err := c2.Wallets.PayInHistory(context.Background(), WalletPayInHistoryQuery{
		Address:  "0xabc",
		DateFrom: "2026-01-01T00:00:00+00:00",
		DateTo:   "2026-02-01T00:00:00+00:00",
		Page:     2,
		PageSize: 50,
	})
	if err != nil {
		t.Fatalf("PayInHistory: %v", err)
	}
	for _, want := range []string{
		`"address":"0xabc"`,
		`"date_from":"2026-01-01T00:00:00+00:00"`,
		`"date_to":"2026-02-01T00:00:00+00:00"`,
		`"page":2`,
		`"page_size":50`,
	} {
		if !strings.Contains(filteredBody, want) {
			t.Errorf("body missing %s: %s", want, filteredBody)
		}
	}
	// An address the project does not own is an empty page, not an error.
	if len(page.Items) != 0 {
		t.Errorf("items = %d, want an empty page", len(page.Items))
	}
}

// The wallet-info shape always carries master_wallet_address, callback_url and
// label, and sends null when there is none. A master wallet with no name has
// none of the three; decoding must not fail, and all must read as unset.
func TestWallet_NullMasterCallbackAndLabelDecode(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"type":"master","address":"0xbeef","chain_family":"EVM","frozen":false,"master_wallet_address":null,"callback_url":null,"label":null}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	w, err := c.Wallets.Info(context.Background(), "0xbeef")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if w.Type != WalletTypeMaster || w.Address != "0xbeef" || w.ChainFamily != FamilyEVM {
		t.Errorf("wallet = %+v", w)
	}
	if w.MasterWalletAddress != "" || w.CallbackURL != "" || w.Label != "" {
		t.Errorf("null must decode as unset, got master=%q callback=%q label=%q", w.MasterWalletAddress, w.CallbackURL, w.Label)
	}
	if w.Frozen {
		t.Errorf("frozen = true, want false")
	}
}
