package cryptochief

import "context"

// WebhooksService reads and re-fires the platform's outbound webhooks - the
// deliveries the platform made TO your endpoint. Access via Client.Webhooks.
//
// A delivery is named by the uuid the platform put on it in the
// X-Webhook-Delivery header ([WebhookDeliveryHeader]). It is the same across
// every attempt and every resend of that delivery, which makes it the natural
// idempotency key for your receiver, and it is the only handle there is: the
// API has no listing of deliveries, and the payload names the order or deposit,
// not the delivery. Keep it when you log an incoming webhook.
//
// The verification helpers for INCOMING webhooks are in webhook.go
// ([VerifyWebhookSignature], [WebhookHandler]); this service is the other
// direction.
type WebhooksService struct{ c *Client }

// Delivery statuses in WebhookDelivery.Status.
const (
	WebhookDeliveryPending    = "pending"     // queued, not yet attempted (or waiting for a retry)
	WebhookDeliveryInProgress = "in_progress" // a worker holds it right now
	WebhookDeliveryDelivered  = "delivered"   // your endpoint answered 2xx
	WebhookDeliveryFailed     = "failed"      // every attempt so far was refused or timed out
	WebhookDeliveryCancelled  = "cancelled"   // superseded by a newer event before it was ever sent
)

// WebhookDelivery is one outbound webhook, with every attempt the platform made
// and the body it sent. Nullable wire fields are pointers so "not recorded" stays
// distinguishable from zero.
type WebhookDelivery struct {
	UUID      string `json:"uuid"`
	EventType string `json:"event_type"`
	// Reference is the object the event was about - the order or static
	// deposit uuid - what you already hold.
	Reference   string `json:"reference"`
	TargetURL   string `json:"target_url"`
	Status      string `json:"status"`
	Attempts    int    `json:"attempts"`
	MaxAttempts int    `json:"max_attempts"`
	// ResendCount is how many times a resend was asked for, by API or from the
	// dashboard.
	ResendCount    int     `json:"resend_count"`
	LastError      *string `json:"last_error"`
	LastHTTPStatus *int    `json:"last_http_status"`
	NextAttemptAt  *string `json:"next_attempt_at"`
	DeliveredAt    *string `json:"delivered_at"`
	CreatedAt      string  `json:"created_at"`
	// SupersededBy names the NEWER event for the same object, when there is
	// one. A superseded delivery cannot be resent (see Resend); resend the
	// latest event instead.
	SupersededBy   *string          `json:"superseded_by"`
	AttemptHistory []WebhookAttempt `json:"attempt_history"`
	Payload        WebhookPayload   `json:"payload"`
}

// WebhookAttempt is one POST the platform made to your endpoint. Newest first
// in WebhookDelivery.AttemptHistory.
type WebhookAttempt struct {
	Attempt int `json:"attempt"`
	// HTTPStatus is nil when nothing answered (DNS, connect, TLS or timeout);
	// Error then holds the transport error.
	HTTPStatus *int    `json:"http_status"`
	Error      *string `json:"error"`
	DurationMS *int64  `json:"duration_ms"`
	TargetURL  string  `json:"target_url"`
	// CreatedAt is nil for attempts recorded before the platform kept the time.
	CreatedAt *string `json:"created_at"`
	// ResponseBody is what your endpoint answered, as the platform saw it.
	// Capped; ResponseTruncated says whether it was cut.
	ResponseBody        *string `json:"response_body"`
	ResponseContentType *string `json:"response_content_type"`
	ResponseTruncated   bool    `json:"response_truncated"`
}

// WebhookPayload is the body the platform sent. Body is the bytes as they went
// out; Bytes is the whole size even when Body was cut.
type WebhookPayload struct {
	Body      string `json:"body"`
	Bytes     int    `json:"bytes"`
	Truncated bool   `json:"truncated"`
}

// WebhookResendResult is what a resend did.
//
// On this platform a resend is synchronous: the POST to your endpoint happens
// before the answer comes back, so Queued=true arrives with Status already
// "delivered" or "failed" for that attempt.
type WebhookResendResult struct {
	UUID        string `json:"uuid"`
	EventType   string `json:"event_type"`
	Reference   string `json:"reference"`
	Status      string `json:"status"`
	Queued      bool   `json:"queued"`
	Attempts    int    `json:"attempts"`
	ResendCount int    `json:"resend_count"`
	// Reason is set when Queued is false: one of the Code* refusal codes below.
	Reason            string `json:"reason,omitempty"`
	SupersededBy      string `json:"superseded_by,omitempty"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}

// StaticDepositResendResult reports the resend of a static deposit's webhook.
// Deliveries has one entry - the newest delivery for the deposit - kept as a
// list so the shape matches the white-label platform, which may requeue several.
type StaticDepositResendResult struct {
	UUID       string                `json:"uuid"`
	Deliveries []WebhookResendResult `json:"deliveries"`
	Queued     int                   `json:"queued"`
	Total      int                   `json:"total"`
}

// Info reads one delivery by the uuid from its X-Webhook-Delivery header. A
// delivery that is not this project's is CodeNotFound, the same as one that
// does not exist.
func (s *WebhooksService) Info(ctx context.Context, deliveryUUID string) (*WebhookDelivery, error) {
	var out WebhookDelivery
	if err := s.c.do(ctx, "/v1/webhooks/info", map[string]string{"uuid": deliveryUUID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Resend sends one delivery to your endpoint again, right now.
//
// Refused with an *APIError whose Code is:
//   - CodeDeliverySuperseded (409) - a newer event exists for the same object.
//     Re-sending invoice.in_mempool after invoice.paid would tell your system
//     the order went backwards, so only the latest event may be resent; the
//     newer event's name is in the error message. Permanent.
//   - CodeDeliveryInFlight (409) - a worker is delivering it right now, or it
//     is already scheduled for an automatic retry. Try again in a moment.
//   - CodeResendTooSoon (429) - this delivery was resent under a minute ago.
//     The gateway sets Retry-After.
//
// A successful manual delivery is billed as /v1/webhook/resend; a refused one
// is not.
func (s *WebhooksService) Resend(ctx context.Context, deliveryUUID string) (*WebhookResendResult, error) {
	var out WebhookResendResult
	if err := s.c.do(ctx, "/v1/webhooks/resend", map[string]string{"uuid": deliveryUUID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResendStaticDeposit re-fires the NEWEST webhook of one static deposit, named
// by the deposit's own uuid - for when you have the deposit and not the
// delivery. Older events of the deposit are superseded and are not resent.
//
// Refused with CodeNoDeliveries (409) when the deposit is yours but no webhook
// was ever queued for it: it arrived on a static wallet with no callback_url.
// The per-delivery refusals of Resend apply as well.
func (s *WebhooksService) ResendStaticDeposit(ctx context.Context, depositUUID string) (*StaticDepositResendResult, error) {
	var out StaticDepositResendResult
	if err := s.c.do(ctx, "/v1/static-deposits/resend", map[string]string{"uuid": depositUUID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
