package cryptochief

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifyWebhookSignature(t *testing.T) {
	const apiKey = "test_api_key_123"
	// Fixed body / hash regression vector. The verifier re-canonicalises
	// the body before hashing, so any byte-equivalent payload (sorted vs
	// unsorted keys) verifies against the same signature.
	body := []byte(`{"amount":"0.0001","coin":"ETH","from_addresses":["0x111","0x222"],"network":"ETH_SEPOLIA","to_address":"0xAbC"}`)
	const want = "97bd68e4e4dc86b6dad8aa06e1f7b63d"

	if err := VerifyWebhookSignature(apiKey, body, want); err != nil {
		t.Fatalf("VerifyWebhookSignature: %v", err)
	}
	// Bad signature.
	if err := VerifyWebhookSignature(apiKey, body, "0000"); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature, got %v", err)
	}
	// Empty body / sig.
	if err := VerifyWebhookSignature(apiKey, nil, want); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("empty body should fail, got %v", err)
	}
	// Missing api key.
	if err := VerifyWebhookSignature("", body, want); err == nil {
		t.Fatalf("empty api key should fail")
	}
}

func TestVerifyWebhookSignature_AcceptsBothCanonicalAndUnsorted(t *testing.T) {
	const apiKey = "test_api_key_123"
	// Same logical payload, unsorted keys — should still verify because we
	// re-canonicalise before hashing.
	unsorted := []byte(`{"to_address":"0xAbC","coin":"ETH","network":"ETH_SEPOLIA","amount":"0.0001","from_addresses":["0x111","0x222"]}`)
	const want = "97bd68e4e4dc86b6dad8aa06e1f7b63d"
	if err := VerifyWebhookSignature(apiKey, unsorted, want); err != nil {
		t.Fatalf("VerifyWebhookSignature (unsorted): %v", err)
	}
}

func TestWebhookHandler(t *testing.T) {
	const apiKey = "test_api_key_123"
	body := `{"event":"payout.paid","uuid":"abc","order_id":"o1","status":"paid"}`
	canon, err := canonicalJSON(map[string]any{
		"event":    "payout.paid",
		"uuid":     "abc",
		"order_id": "o1",
		"status":   "paid",
	})
	if err != nil {
		t.Fatal(err)
	}
	sig := signBody(canon, apiKey)

	called := false
	h := WebhookHandler[PayoutWebhookEvent](apiKey, func(w http.ResponseWriter, r *http.Request, evt PayoutWebhookEvent) {
		called = true
		if evt.UUID != "abc" || evt.Status != "paid" {
			t.Errorf("unexpected event: %+v", evt)
		}
		w.WriteHeader(http.StatusOK)
	})

	t.Run("ok", func(t *testing.T) {
		called = false
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Signature", sig)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			b, _ := io.ReadAll(rr.Result().Body)
			t.Fatalf("status %d body=%s", rr.Code, b)
		}
		if !called {
			t.Fatal("handler was not invoked")
		}
	})

	t.Run("bad signature → 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Signature", "deadbeef")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status %d", rr.Code)
		}
	})

	t.Run("GET → 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status %d", rr.Code)
		}
	})
}
