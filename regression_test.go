package cryptochief

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureServer spins an httptest server that records the request path and
// body and returns a fixed JSON response. Used to assert the exact wire shape
// the SDK sends.
func captureServer(t *testing.T, resp string, path, body *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*path = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		*body = string(b)
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestPayIn_CryptoModeWireShape asserts the pay-in create wire format the
// gateway expects: mode is lowercase ("crypto") and asset is a {coin,network}
// object — not a bare string.
func TestPayIn_CryptoModeWireShape(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"uuid":"u1","status":"pending"}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	_, err := c.PayIns.Create(context.Background(), &CreatePayInRequest{
		OrderID:      "o1",
		UserID:       "u",
		Mode:         PayInModeCrypto,
		AmountCrypto: "50.5",
		Asset:        &Asset{Coin: "USDT", Network: ChainTronMainnet},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if path != "/v1/payments/order/create" {
		t.Errorf("path: %q", path)
	}
	if !strings.Contains(body, `"mode":"crypto"`) {
		t.Errorf("mode must be lowercase object value, body=%s", body)
	}
	if !strings.Contains(body, `"asset":{"coin":"USDT","network":"TRON_MAINNET"}`) {
		t.Errorf("asset must serialize as an object, body=%s", body)
	}
}

// TestPayIn_FiatAssetsPolicyWireShape verifies the FIAT-mode "assets" field
// serializes as an allow/exclude policy object, matching the gateway DTO.
func TestPayIn_FiatAssetsPolicyWireShape(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"uuid":"u1","status":"waiting_asset_select"}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	_, err := c.PayIns.Create(context.Background(), &CreatePayInRequest{
		OrderID:    "o1",
		UserID:     "u",
		Mode:       PayInModeFiat,
		AmountFiat: "100.00",
		Currency:   "USD",
		Assets:     &AssetsPolicy{Allow: []Asset{{Coin: "USDT", Network: ChainEthMainnet}}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.Contains(body, `"mode":"fiat"`) {
		t.Errorf("mode must be lowercase, body=%s", body)
	}
	if !strings.Contains(body, `"assets":{"allow":[{"coin":"USDT","network":"ETH_MAINNET"}]}`) {
		t.Errorf("assets must serialize as an allow/exclude object, body=%s", body)
	}
}

// TestPayout_AutoConvertPolicyWireShape asserts auto_convert_policy serializes
// as an allow/exclude object — the shape the gateway models as *AssetsPolicy.
func TestPayout_AutoConvertPolicyWireShape(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"uuid":"u1","status":"queue"}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	_, err := c.Payouts.Execute(context.Background(), &ExecutePayoutRequest{
		OrderID:           "o1",
		UserID:            "u",
		Network:           ChainEthMainnet,
		Coin:              "ETH",
		Amount:            "0.1",
		ToAddress:         "0xabc",
		URLCallback:       "https://x/cb",
		AutoConvert:       true,
		AutoConvertPolicy: &AssetsPolicy{Allow: []Asset{{Coin: "USDT", Network: ChainEthMainnet}}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(body, `"auto_convert_policy":{"allow":[{"coin":"USDT","network":"ETH_MAINNET"}]}`) {
		t.Errorf("auto_convert_policy must serialize as an object, body=%s", body)
	}
}

// TestPayIn_HistoryPath asserts PayIns.History posts to the gateway's pay-in
// history route, POST /v1/payments/history.
func TestPayIn_HistoryPath(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"items":[],"meta":{"page":1,"page_size":20,"total":0}}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	if _, err := c.PayIns.History(context.Background(), HistoryQuery{Page: 1}); err != nil {
		t.Fatalf("History: %v", err)
	}
	if path != "/v1/payments/history" {
		t.Errorf("PayIns.History path = %q, want /v1/payments/history", path)
	}
}

// TestCredits_BalanceWireShape asserts Credits.Balance posts a signed empty
// JSON object to /v1/credits/balance and maps every response field —
// including a negative usd_balance, which the endpoint returns for postpaid
// projects in debt.
func TestCredits_BalanceWireShape(t *testing.T) {
	const apiKey = "k"
	var path, body, sig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		sig = r.Header.Get(headerSignature)
		_, _ = io.WriteString(w, `{"credits_balance":-15200000,"usd_balance":"-1.52","is_postpaid":true,"debt_limit_credits":500000000,"can_execute_gas_operations":false,"gas_ops_min_credits":3000000,"timestamp":"2026-08-18T12:00:00Z"}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", apiKey, WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Credits.Balance(context.Background())
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	if path != "/v1/credits/balance" {
		t.Errorf("path: %q", path)
	}
	if body != "{}" {
		t.Errorf("body must be an empty JSON object, got %q", body)
	}
	if want := signBody([]byte(body), apiKey); sig != want {
		t.Errorf("signature mismatch: got %s want %s", sig, want)
	}
	if out.CreditsBalance != -15200000 {
		t.Errorf("CreditsBalance: %d", out.CreditsBalance)
	}
	if out.USDBalance != "-1.52" {
		t.Errorf("USDBalance: %q", out.USDBalance)
	}
	if !out.IsPostpaid {
		t.Error("IsPostpaid: want true")
	}
	if out.DebtLimitCredits != 500000000 {
		t.Errorf("DebtLimitCredits: %d", out.DebtLimitCredits)
	}
	if out.CanExecuteGasOperations {
		t.Error("CanExecuteGasOperations: want false")
	}
	if out.GasOpsMinCredits != 3000000 {
		t.Errorf("GasOpsMinCredits: %d", out.GasOpsMinCredits)
	}
	if out.Timestamp != "2026-08-18T12:00:00Z" {
		t.Errorf("Timestamp: %q", out.Timestamp)
	}
}

// TestCredits_TopupWireShape asserts Credits.Topup posts the signed canonical
// body to /v1/credits/topup (keys sorted, redirect urls included when set) and
// maps every response field, including the optional order_uuid / expired_at.
func TestCredits_TopupWireShape(t *testing.T) {
	const apiKey = "k"
	var path, body, sig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		sig = r.Header.Get(headerSignature)
		_, _ = io.WriteString(w, `{"invoice_id":901,"payment_link":"https://pay.example/i/901","amount":"250","currency":"USDT","status":"pending","order_uuid":"ord-1","expired_at":1766000000}`)
	}))
	t.Cleanup(srv.Close)

	c, _ := New("m", apiKey, WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Credits.Topup(context.Background(), CreditsTopupRequest{
		Amount:     "250",
		Currency:   "USDT",
		URLSuccess: "https://shop.example/ok",
		URLError:   "https://shop.example/fail",
	})
	if err != nil {
		t.Fatalf("Topup: %v", err)
	}
	if path != "/v1/credits/topup" {
		t.Errorf("path: %q", path)
	}
	if want := `{"amount":"250","currency":"USDT","url_error":"https://shop.example/fail","url_success":"https://shop.example/ok"}`; body != want {
		t.Errorf("body = %s, want %s", body, want)
	}
	if want := signBody([]byte(body), apiKey); sig != want {
		t.Errorf("signature mismatch: got %s want %s", sig, want)
	}
	if out.InvoiceID != 901 {
		t.Errorf("InvoiceID: %d", out.InvoiceID)
	}
	if out.PaymentLink != "https://pay.example/i/901" {
		t.Errorf("PaymentLink: %q", out.PaymentLink)
	}
	if out.Amount != "250" {
		t.Errorf("Amount: %q", out.Amount)
	}
	if out.Currency != "USDT" {
		t.Errorf("Currency: %q", out.Currency)
	}
	if out.Status != "pending" {
		t.Errorf("Status: %q", out.Status)
	}
	if out.OrderUUID != "ord-1" {
		t.Errorf("OrderUUID: %q", out.OrderUUID)
	}
	if out.ExpiredAt != 1766000000 {
		t.Errorf("ExpiredAt: %d", out.ExpiredAt)
	}
}

// TestCredits_TopupOmitsEmptyOptionalURLs asserts unset url_success/url_error
// never reach the wire as "" — the exact-body check proves omission — and that
// a response without order_uuid/expired_at maps to the zero values.
func TestCredits_TopupOmitsEmptyOptionalURLs(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"invoice_id":902,"payment_link":"https://pay.example/i/902","amount":"25.50","currency":"USDC","status":"pending"}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Credits.Topup(context.Background(), CreditsTopupRequest{
		Amount:   "25.50",
		Currency: "USDC",
	})
	if err != nil {
		t.Fatalf("Topup: %v", err)
	}
	if path != "/v1/credits/topup" {
		t.Errorf("path: %q", path)
	}
	if want := `{"amount":"25.50","currency":"USDC"}`; body != want {
		t.Errorf("empty optional urls must be omitted, body = %s, want %s", body, want)
	}
	if out.InvoiceID != 902 {
		t.Errorf("InvoiceID: %d", out.InvoiceID)
	}
	if out.OrderUUID != "" {
		t.Errorf("OrderUUID must be empty when absent, got %q", out.OrderUUID)
	}
	if out.ExpiredAt != 0 {
		t.Errorf("ExpiredAt must be 0 when absent, got %d", out.ExpiredAt)
	}
}

// countingRW is a minimal ResponseWriter that records WriteHeader calls, so we
// can assert WebhookHandler writes the response status exactly once.
type countingRW struct {
	hdr   http.Header
	codes []int
	body  []byte
}

func (c *countingRW) Header() http.Header {
	if c.hdr == nil {
		c.hdr = http.Header{}
	}
	return c.hdr
}
func (c *countingRW) WriteHeader(code int)        { c.codes = append(c.codes, code) }
func (c *countingRW) Write(b []byte) (int, error) { c.body = append(c.body, b...); return len(b), nil }

func TestWebhookHandler_StatusWrites(t *testing.T) {
	const apiKey = "k"
	canon, err := canonicalJSON(map[string]any{
		"event": "payout.paid", "uuid": "abc", "order_id": "o1", "status": "paid",
	})
	if err != nil {
		t.Fatal(err)
	}
	sig := signBody(canon, apiKey)

	newReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(canon))
		req.Header.Set(WebhookHeader, sig)
		return req
	}

	t.Run("silent handler defaults to one 200", func(t *testing.T) {
		h := WebhookHandler[PayoutWebhookEvent](apiKey, func(http.ResponseWriter, *http.Request, PayoutWebhookEvent) {})
		crw := &countingRW{}
		h.ServeHTTP(crw, newReq())
		if len(crw.codes) != 1 || crw.codes[0] != http.StatusOK {
			t.Fatalf("want exactly one 200, got codes=%v", crw.codes)
		}
	})

	t.Run("handler-written status is not overwritten", func(t *testing.T) {
		h := WebhookHandler[PayoutWebhookEvent](apiKey, func(w http.ResponseWriter, _ *http.Request, _ PayoutWebhookEvent) {
			w.WriteHeader(http.StatusAccepted)
		})
		crw := &countingRW{}
		h.ServeHTTP(crw, newReq())
		// The handler's own status is preserved; no extra default 200 follows.
		if len(crw.codes) != 1 || crw.codes[0] != http.StatusAccepted {
			t.Fatalf("want exactly one 202, got codes=%v", crw.codes)
		}
	})
}
