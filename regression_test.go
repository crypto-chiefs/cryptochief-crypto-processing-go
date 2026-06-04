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
