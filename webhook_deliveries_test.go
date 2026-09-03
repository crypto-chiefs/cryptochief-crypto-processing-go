package cryptochief

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const deliveryJSON = `{
	"uuid":"44444444-4444-4444-8444-444444444444","event_type":"invoice.paid","reference":"order-1",
	"target_url":"https://m.example/hook","status":"failed","attempts":3,"max_attempts":10,"resend_count":1,
	"last_error":"HTTP 500","last_http_status":500,"next_attempt_at":null,"delivered_at":null,
	"created_at":"2026-09-03T10:00:00Z","superseded_by":null,
	"attempt_history":[
		{"attempt":3,"http_status":500,"error":"HTTP 500","duration_ms":120,"target_url":"https://m.example/hook",
		 "created_at":"2026-09-03T10:02:00Z","response_body":"<html>oops","response_content_type":"text/html","response_truncated":true},
		{"attempt":2,"http_status":null,"error":"dial tcp: connection refused","duration_ms":null,"target_url":"https://m.example/hook",
		 "created_at":null,"response_body":null,"response_content_type":null,"response_truncated":false}
	],
	"payload":{"body":"{\"event\":\"invoice.paid\"}","bytes":24,"truncated":false}
}`

// The wire shape of a delivery read, and that "not recorded" survives as nil
// rather than collapsing into zero: an attempt nothing answered has no status
// and no body, only the transport error.
func TestWebhooks_InfoWireShape(t *testing.T) {
	var path, body string
	srv := captureServer(t, deliveryJSON, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	d, err := c.Webhooks.Info(context.Background(), "44444444-4444-4444-8444-444444444444")
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if path != "/v1/webhooks/info" || !strings.Contains(body, `"uuid":"44444444-4444-4444-8444-444444444444"`) {
		t.Errorf("request = %s %s", path, body)
	}
	if d.Status != WebhookDeliveryFailed || d.Attempts != 3 || d.MaxAttempts != 10 || d.ResendCount != 1 {
		t.Errorf("delivery = %+v", d)
	}
	if d.LastHTTPStatus == nil || *d.LastHTTPStatus != 500 || d.DeliveredAt != nil || d.SupersededBy != nil {
		t.Errorf("nullable fields: last_http_status=%v delivered_at=%v superseded_by=%v", d.LastHTTPStatus, d.DeliveredAt, d.SupersededBy)
	}
	if len(d.AttemptHistory) != 2 {
		t.Fatalf("attempts = %d", len(d.AttemptHistory))
	}
	answered, silent := d.AttemptHistory[0], d.AttemptHistory[1]
	if answered.HTTPStatus == nil || *answered.HTTPStatus != 500 || answered.ResponseBody == nil || !answered.ResponseTruncated {
		t.Errorf("answered attempt = %+v", answered)
	}
	if silent.HTTPStatus != nil || silent.ResponseBody != nil || silent.CreatedAt != nil || silent.Error == nil {
		t.Errorf("an attempt nothing answered must keep its nils: %+v", silent)
	}
	if d.Payload.Bytes != 24 || d.Payload.Body == "" {
		t.Errorf("payload = %+v", d.Payload)
	}
}

// The three routes and their bodies. The static-deposit resend is addressed by
// the DEPOSIT's uuid, not a delivery's, and answers per delivery inside a list.
func TestWebhooks_ResendRoutes(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{"uuid":"dep-1","deliveries":[{"uuid":"d-1","event_type":"static_deposit.paid","reference":"dep-1","status":"delivered","queued":true,"attempts":2,"resend_count":1}],"queued":1,"total":1}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Webhooks.ResendStaticDeposit(context.Background(), "dep-1")
	if err != nil {
		t.Fatalf("ResendStaticDeposit: %v", err)
	}
	if path != "/v1/static-deposits/resend" || !strings.Contains(body, `"uuid":"dep-1"`) {
		t.Errorf("request = %s %s", path, body)
	}
	if out.Queued != 1 || len(out.Deliveries) != 1 || !out.Deliveries[0].Queued || out.Deliveries[0].Status != WebhookDeliveryDelivered {
		t.Errorf("result = %+v", out)
	}
}

// A refusal is an *APIError with the machine code where every gateway code
// lives - Code - and the detail (which event superseded, how long to wait)
// still in Raw. Resend must not swallow it into a "queued:false" result.
func TestWebhooks_RefusalsAreAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "static-deposits"):
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"ok":false,"error":"NO_DELIVERIES","msg":"no webhook was ever queued for this deposit"}`))
		default:
			w.Header().Set("Retry-After", "37")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"error":"RESEND_TOO_SOON","msg":"try again shortly","retry_after_seconds":37}`))
		}
	}))
	defer srv.Close()

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))

	_, err := c.Webhooks.Resend(context.Background(), "44444444-4444-4444-8444-444444444444")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %T %v", err, err)
	}
	if apiErr.Code != CodeResendTooSoon || apiErr.HTTPStatus != http.StatusTooManyRequests {
		t.Errorf("code=%q status=%d", apiErr.Code, apiErr.HTTPStatus)
	}
	if !strings.Contains(string(apiErr.Raw), `"retry_after_seconds":37`) {
		t.Errorf("the detail must survive in Raw: %s", apiErr.Raw)
	}

	_, err = c.Webhooks.ResendStaticDeposit(context.Background(), "dep-1")
	if !errors.As(err, &apiErr) || apiErr.Code != CodeNoDeliveries {
		t.Errorf("static deposit without a webhook: %v", err)
	}
}
