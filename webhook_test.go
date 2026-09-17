package cryptochief

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Copied without changes from
// processing-webhook-service/internal/signature/testdata/webhook_hmac_v1_vectors.json.
const (
	webhookVectorsFile   = "testdata/webhook_hmac_v1_vectors.json"
	webhookVectorsSHA256 = "15a6e1423708e8c3b9ec4fac7ee6eb383db56703605647e02308a29166722502"
)

const (
	webhookExpectOK                  = "ok"
	webhookExpectBadHeaders          = "bad_headers"
	webhookExpectTimestampOutOfRange = "timestamp_out_of_range"
	webhookExpectBadSignature        = "bad_signature"
)

// webhookV1Vector is one entry of the webhook vectors. The receiver sees
// X-CC-Timestamp = Timestamp, X-Webhook-Delivery = DeliveryID and
// X-CC-Signature = Signature, except for the headers listed in Headers: an
// empty list removes the header, two values repeat it.
type webhookV1Vector struct {
	Name         string              `json:"name"`
	APIKey       string              `json:"api_key"`
	Timestamp    int64               `json:"timestamp"`
	DeliveryID   string              `json:"delivery_id"`
	Body         *string             `json:"body"`
	BodyBase64   string              `json:"body_base64"`
	BodySHA256   string              `json:"body_sha256"`
	StringToSign string              `json:"string_to_sign"`
	Signature    string              `json:"signature"`
	Now          int64               `json:"now"`
	Expect       string              `json:"expect"`
	Headers      map[string][]string `json:"headers"`
}

func loadWebhookV1Vectors(t *testing.T) []webhookV1Vector {
	t.Helper()
	raw, err := os.ReadFile(webhookVectorsFile)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != webhookVectorsSHA256 {
		t.Fatalf("%s sha256 = %s, want %s", webhookVectorsFile, got, webhookVectorsSHA256)
	}
	var vs []webhookV1Vector
	if err := json.Unmarshal(raw, &vs); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, v := range vs {
		counts[v.Expect]++
	}
	want := map[string]int{
		webhookExpectOK:                  22,
		webhookExpectBadHeaders:          26,
		webhookExpectBadSignature:        4,
		webhookExpectTimestampOutOfRange: 2,
	}
	if len(vs) != 54 || len(counts) != len(want) {
		t.Fatalf("vectors: %d entries %v, want 54 %v", len(vs), counts, want)
	}
	for k, n := range want {
		if counts[k] != n {
			t.Fatalf("vectors: %v, want %v", counts, want)
		}
	}
	return vs
}

func (v webhookV1Vector) body(t *testing.T) []byte {
	t.Helper()
	switch {
	case v.Body != nil && v.BodyBase64 == "":
		return []byte(*v.Body)
	case v.Body == nil && v.BodyBase64 != "":
		b, err := base64.StdEncoding.DecodeString(v.BodyBase64)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	t.Fatalf("%s: exactly one of body / body_base64", v.Name)
	return nil
}

func (v webhookV1Vector) header(t *testing.T) http.Header {
	t.Helper()
	values := map[string][]string{
		HeaderTimestamp:       {strconv.FormatInt(v.Timestamp, 10)},
		HeaderWebhookDelivery: {v.DeliveryID},
		HeaderSignature:       {v.Signature},
	}
	for name, vs := range v.Headers {
		if _, known := values[name]; !known || vs == nil {
			t.Fatalf("%s: header override %q = %v", v.Name, name, vs)
		}
		values[name] = vs
	}
	h := http.Header{}
	for name, vs := range values {
		for _, val := range vs {
			h.Add(name, val)
		}
	}
	return h
}

func (v webhookV1Vector) clock() WebhookOption {
	return WithWebhookClock(func() time.Time { return time.Unix(v.Now, 0) })
}

// webhookOutcome maps a VerifyWebhook result onto the vector's expect.
func webhookOutcome(err error) string {
	headers := errors.Is(err, ErrWebhookHeaders)
	timestamp := errors.Is(err, ErrWebhookTimestamp)
	signature := errors.Is(err, ErrWebhookSignature)
	n := 0
	for _, b := range []bool{headers, timestamp, signature} {
		if b {
			n++
		}
	}
	switch {
	case err == nil:
		return webhookExpectOK
	case n != 1:
		return "ambiguous: " + err.Error()
	case headers:
		return webhookExpectBadHeaders
	case timestamp:
		return webhookExpectTimestampOutOfRange
	default:
		return webhookExpectBadSignature
	}
}

func TestWebhookV1Vectors(t *testing.T) {
	for _, v := range loadWebhookV1Vectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			body := v.body(t)
			sum := sha256.Sum256(body)
			if got := hex.EncodeToString(sum[:]); got != v.BodySHA256 {
				t.Fatalf("body_sha256 = %s, want %s", got, v.BodySHA256)
			}

			sts, err := WebhookV1StringToSign(v.Timestamp, v.DeliveryID, body)
			if err != nil {
				t.Fatal(err)
			}
			if sts != v.StringToSign {
				t.Fatalf("string_to_sign\n got: %q\nwant: %q", sts, v.StringToSign)
			}
			sig, err := SignWebhookV1(v.APIKey, v.Timestamp, v.DeliveryID, body)
			if err != nil {
				t.Fatal(err)
			}
			if sig != v.Signature {
				t.Fatalf("signature = %s, want %s", sig, v.Signature)
			}

			cases := map[string][]WebhookOption{
				"tolerance 300s": {v.clock(), WithWebhookTolerance(300 * time.Second)},
				"default":        {v.clock()},
				"tolerance 0":    {v.clock(), WithWebhookTolerance(0)},
			}
			for name, opts := range cases {
				err := VerifyWebhook(v.APIKey, body, v.header(t), opts...)
				if got := webhookOutcome(err); got != v.Expect {
					t.Errorf("%s: VerifyWebhook = %s (%v), want %s", name, got, err, v.Expect)
				}
			}
		})
	}
}

// Each refused vector passes once its single defect is removed.
func TestWebhookV1Vectors_SingleDefect(t *testing.T) {
	for _, v := range loadWebhookV1Vectors(t) {
		if v.Expect == webhookExpectOK {
			continue
		}
		fixed := v
		fixed.Headers = nil
		if v.Expect == webhookExpectTimestampOutOfRange {
			fixed.Now = v.Timestamp
		}
		if err := VerifyWebhook(v.APIKey, v.body(t), fixed.header(t), fixed.clock()); err != nil {
			t.Errorf("%s: without the defect: %v", v.Name, err)
		}
	}
}

func findWebhookV1Vector(t *testing.T, name string) webhookV1Vector {
	t.Helper()
	for _, v := range loadWebhookV1Vectors(t) {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("vector %q not found", name)
	return webhookV1Vector{}
}

func TestVerifyWebhook_HeaderNames(t *testing.T) {
	v := findWebhookV1Vector(t, "payout_paid")
	body := v.body(t)

	lower := http.Header{
		strings.ToLower(HeaderTimestamp):       {strconv.FormatInt(v.Timestamp, 10)},
		strings.ToLower(HeaderWebhookDelivery): {v.DeliveryID},
		strings.ToLower(HeaderSignature):       {v.Signature},
	}
	if err := VerifyWebhook(v.APIKey, body, lower, v.clock()); err != nil {
		t.Errorf("lower-case names: %v", err)
	}

	// One value under two spellings of the name is a repeat.
	h := v.header(t)
	h["x-cc-signature"] = []string{v.Signature}
	if err := VerifyWebhook(v.APIKey, body, h, v.clock()); !errors.Is(err, ErrWebhookHeaders) {
		t.Errorf("two spellings: %v, want ErrWebhookHeaders", err)
	}

	// Names match ignoring ASCII case only: U+017F and U+212A fold to "s" and
	// "k" in Unicode but do not make those names.
	ts := strconv.FormatInt(v.Timestamp, 10)
	longS := http.Header{
		"X-Cc-Time\u017ftamp": {ts},
		"X-Webhook-Delivery":  {v.DeliveryID},
		"X-Cc-Signature":      {v.Signature},
	}
	if err := VerifyWebhook(v.APIKey, body, longS, v.clock()); !errors.Is(err, ErrWebhookHeaders) {
		t.Errorf("timestamp name with U+017F only: %v, want ErrWebhookHeaders", err)
	}
	h = v.header(t)
	h["X-Webhoo\u212a-Delivery"] = []string{v.DeliveryID}
	h["x-cc-\u017fignature"] = []string{v.Signature}
	if err := VerifyWebhook(v.APIKey, body, h, v.clock()); err != nil {
		t.Errorf("extra names with U+212A and U+017F: %v, want nil", err)
	}
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{HeaderTimestamp, "x-cc-timestamp", true},
		{HeaderTimestamp, "X-CC-TIMESTAMP", true},
		{HeaderTimestamp, "X-CC-Time\u017ftamp", false},
		{HeaderWebhookDelivery, "X-Webhoo\u212a-Delivery", false},
		{"@", "`", false},
		{"[", "{", false},
		{"", "", true},
	} {
		if got := asciiEqualFold(tc.a, tc.b); got != tc.want {
			t.Errorf("asciiEqualFold(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}

	// Request signature headers are not webhook headers.
	h = v.header(t)
	h.Del(HeaderSignature)
	h.Set("Signature", strings.TrimPrefix(v.Signature, "v1="))
	h.Set("X-Webhook-Signature", v.Signature)
	if err := VerifyWebhook(v.APIKey, body, h, v.clock()); !errors.Is(err, ErrWebhookHeaders) {
		t.Errorf("Signature and X-Webhook-Signature only: %v, want ErrWebhookHeaders", err)
	}
}

func TestVerifyWebhook_Options(t *testing.T) {
	v := findWebhookV1Vector(t, "payout_paid")
	body := v.body(t)
	at := func(unix int64) WebhookOption {
		return WithWebhookClock(func() time.Time { return time.Unix(unix, 0) })
	}

	if err := VerifyWebhook(v.APIKey, body, v.header(t), at(v.Timestamp+600), WithWebhookTolerance(600*time.Second)); err != nil {
		t.Errorf("600s tolerance at +600: %v", err)
	}
	if err := VerifyWebhook(v.APIKey, body, v.header(t), at(v.Timestamp+601), WithWebhookTolerance(600*time.Second)); !errors.Is(err, ErrWebhookTimestamp) {
		t.Errorf("600s tolerance at +601: %v", err)
	}
	if err := VerifyWebhook(v.APIKey, body, v.header(t), at(v.Timestamp-301), WithWebhookTolerance(-time.Second)); !errors.Is(err, ErrWebhookTimestamp) {
		t.Errorf("negative tolerance at -301: %v", err)
	}
	// The real clock is far from the vector timestamp.
	if err := VerifyWebhook(v.APIKey, body, v.header(t)); !errors.Is(err, ErrWebhookTimestamp) {
		t.Errorf("time.Now: %v", err)
	}
	if err := VerifyWebhook(v.APIKey, body, v.header(t), WithWebhookClock(nil)); !errors.Is(err, ErrWebhookTimestamp) {
		t.Errorf("nil clock: %v", err)
	}

	// Headers are checked before the timestamp, the timestamp before the
	// signature.
	h := v.header(t)
	h.Set(HeaderSignature, "v1=zz")
	if err := VerifyWebhook(v.APIKey, body, h, at(v.Timestamp+10_000)); !errors.Is(err, ErrWebhookHeaders) {
		t.Errorf("bad header and stale timestamp: %v", err)
	}
	if err := VerifyWebhook(v.APIKey+"x", body, v.header(t), at(v.Timestamp+10_000)); !errors.Is(err, ErrWebhookTimestamp) {
		t.Errorf("wrong key and stale timestamp: %v", err)
	}
}

// The timestamp header carries the number the signature was built from: any
// other spelling of it is refused, so one signature never stands for two
// header values.
func TestVerifyWebhook_TimestampFormat(t *testing.T) {
	v := findWebhookV1Vector(t, "payout_paid")
	body := v.body(t)
	canonical := strconv.FormatInt(v.Timestamp, 10)

	verify := func(ts string) error {
		h := v.header(t)
		h.Set(HeaderTimestamp, ts)
		return VerifyWebhook(v.APIKey, body, h, v.clock())
	}

	if err := verify(canonical); err != nil {
		t.Fatalf("%s: %v", canonical, err)
	}
	for _, ts := range []string{
		"0" + canonical,
		"00" + canonical,
		"00",
		"+" + canonical,
		"1e9",
		"",
	} {
		if err := verify(ts); !errors.Is(err, ErrWebhookHeaders) {
			t.Errorf("%q: %v, want ErrWebhookHeaders", ts, err)
		}
	}
	// Spaces and tabs are trimmed before the format check.
	if err := verify(" \t" + canonical + "\t "); err != nil {
		t.Errorf("padded value: %v", err)
	}
	// "0" is a canonical number: it passes the format check and fails the
	// window.
	if err := verify("0"); !errors.Is(err, ErrWebhookTimestamp) {
		t.Errorf(`"0": %v, want ErrWebhookTimestamp`, err)
	}

	for _, tc := range []struct {
		s    string
		want bool
	}{
		{"1789430400", true},
		{"0", true},
		{"01789430400", false},
		{"00", false},
		{"", false},
		{"1 ", false},
		{"-1", false},
		{"１", false},
	} {
		if got := isCanonicalDecimal(tc.s); got != tc.want {
			t.Errorf("isCanonicalDecimal(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestVerifyWebhook_EmptyKey(t *testing.T) {
	v := findWebhookV1Vector(t, "payout_paid")
	err := VerifyWebhook("", v.body(t), http.Header{}, v.clock())
	if err == nil || isWebhookRefusal(err) {
		t.Fatalf("empty key: %v", err)
	}
	if _, err := SignWebhookV1("", v.Timestamp, v.DeliveryID, v.body(t)); err == nil {
		t.Fatal("SignWebhookV1 with empty key: no error")
	}
}

func TestSignWebhookV1_RejectsInput(t *testing.T) {
	const key = "test_api_key_123"
	cases := map[string]struct {
		ts       int64
		delivery string
	}{
		"zero timestamp":        {0, "d"},
		"negative timestamp":    {-1, "d"},
		"empty delivery id":     {1789430400, ""},
		"delivery id with LF":   {1789430400, "d\n"},
		"delivery id with dot":  {1789430400, "d.1"},
		"delivery id 129 chars": {1789430400, strings.Repeat("a", 129)},
	}
	for name, c := range cases {
		if _, err := WebhookV1StringToSign(c.ts, c.delivery, nil); err == nil {
			t.Errorf("%s: WebhookV1StringToSign accepted", name)
		}
		if _, err := SignWebhookV1(key, c.ts, c.delivery, nil); err == nil {
			t.Errorf("%s: SignWebhookV1 accepted", name)
		}
	}
}

// signWebhook returns the headers the platform sends with body at ts.
func signWebhook(t *testing.T, apiKey string, ts int64, deliveryID string, body []byte) http.Header {
	t.Helper()
	sig, err := SignWebhookV1(apiKey, ts, deliveryID, body)
	if err != nil {
		t.Fatal(err)
	}
	m := hmac.New(sha256.New, []byte(apiKey))
	m.Write([]byte(webhookV1Scope + "\n" + strconv.FormatInt(ts, 10) + "\n" + deliveryID + "\n" + sha256Hex(body)))
	if want := "v1=" + hex.EncodeToString(m.Sum(nil)); sig != want {
		t.Fatalf("SignWebhookV1 = %s, want %s", sig, want)
	}
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set(HeaderWebhookDelivery, deliveryID)
	h.Set(HeaderTimestamp, strconv.FormatInt(ts, 10))
	h.Set(HeaderSignature, sig)
	return h
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// decodesAs reports whether WebhookHandler decodes body into T.
func decodesAs[T any](body []byte) bool {
	var v T
	return json.NewDecoder(bytes.NewReader(body)).Decode(&v) == nil
}

// TestWebhookHandler_VectorsOverHTTP sends every vector through a real HTTP
// client to WebhookHandler. Header values with CR or LF cannot be sent by
// net/http; those vectors go to ServeHTTP directly.
func TestWebhookHandler_VectorsOverHTTP(t *testing.T) {
	type got struct {
		called bool
		evt    map[string]any
	}
	for _, v := range loadWebhookV1Vectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			body := v.body(t)
			var g got
			h := WebhookHandler[map[string]any](v.APIKey, func(w http.ResponseWriter, _ *http.Request, evt map[string]any) {
				g.called = true
				g.evt = evt
			}, v.clock())

			wantStatus := http.StatusUnauthorized
			if v.Expect == webhookExpectOK {
				wantStatus = http.StatusOK
				if !decodesAs[map[string]any](body) {
					wantStatus = http.StatusBadRequest
				}
			}

			header := v.header(t)
			rawHeader := false
			for _, vs := range header {
				for _, val := range vs {
					if strings.ContainsAny(val, "\r\n") {
						rawHeader = true
					}
				}
			}

			var status int
			if rawHeader {
				req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
				req.Header = header
				rr := httptest.NewRecorder()
				h.ServeHTTP(rr, req)
				status = rr.Code
			} else {
				srv := httptest.NewServer(h)
				defer srv.Close()
				req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/webhook", bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				for name, vs := range header {
					for _, val := range vs {
						req.Header.Add(name, val)
					}
				}
				req.Header.Set("Content-Type", "application/json")
				resp, err := srv.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				status = resp.StatusCode
			}

			if status != wantStatus {
				t.Fatalf("status %d, want %d (expect %s)", status, wantStatus, v.Expect)
			}
			if g.called != (wantStatus == http.StatusOK) {
				t.Fatalf("handler called = %v", g.called)
			}
		})
	}
}

func TestWebhookHandler_PayoutEvent(t *testing.T) {
	v := findWebhookV1Vector(t, "payout_paid")
	body := v.body(t)
	var evt PayoutWebhookEvent
	srv := httptest.NewServer(WebhookHandler[PayoutWebhookEvent](v.APIKey, func(w http.ResponseWriter, _ *http.Request, e PayoutWebhookEvent) {
		evt = e
		w.WriteHeader(http.StatusAccepted)
	}, v.clock()))
	defer srv.Close()

	post := func(body []byte, header http.Header) int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader(body))
		req.Header = header
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}

	if status := post(body, v.header(t)); status != http.StatusAccepted {
		t.Fatalf("status %d, want 202", status)
	}
	if evt.Event != "payout.paid" || evt.OrderID != "payout?batch=7&row=3" {
		t.Errorf("event: %+v", evt)
	}

	tampered := bytes.Replace(body, []byte(`"payout.paid"`), []byte(`"payout.system_fail"`), 1)
	if bytes.Equal(tampered, body) {
		t.Fatal("tampering did not change the body")
	}
	if status := post(tampered, v.header(t)); status != http.StatusUnauthorized {
		t.Errorf("tampered body: status %d, want 401", status)
	}

	// Re-encoded JSON is other bytes and does not verify.
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	indented, _ := json.MarshalIndent(parsed, "", "  ")
	if status := post(indented, v.header(t)); status != http.StatusUnauthorized {
		t.Errorf("re-encoded body: status %d, want 401", status)
	}
}

func TestWebhookHandler_Responses(t *testing.T) {
	const apiKey = "test_api_key_123"
	body := []byte(`{"event":"payout.paid","uuid":"abc","order_id":"o1","status":"paid"}`)
	ts := time.Now().Unix()

	called := false
	h := WebhookHandler[PayoutWebhookEvent](apiKey, func(w http.ResponseWriter, r *http.Request, evt PayoutWebhookEvent) {
		called = true
		if evt.UUID != "abc" || evt.Status != "paid" {
			t.Errorf("unexpected event: %+v", evt)
		}
	})

	serve := func(h http.Handler, method string, body []byte, header http.Header) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/", bytes.NewReader(body))
		for k, vs := range header {
			req.Header[k] = vs
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}

	t.Run("ok", func(t *testing.T) {
		called = false
		rr := serve(h, http.MethodPost, body, signWebhook(t, apiKey, ts, "dlv-1", body))
		if rr.Code != http.StatusOK || !called {
			t.Fatalf("status %d called=%v body=%s", rr.Code, called, rr.Body.String())
		}
	})

	t.Run("wrong key → 401", func(t *testing.T) {
		called = false
		rr := serve(h, http.MethodPost, body, signWebhook(t, apiKey+"x", ts, "dlv-1", body))
		if rr.Code != http.StatusUnauthorized || called {
			t.Fatalf("status %d called=%v", rr.Code, called)
		}
	})

	t.Run("no headers → 401", func(t *testing.T) {
		rr := serve(h, http.MethodPost, body, nil)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status %d", rr.Code)
		}
	})

	t.Run("stale timestamp → 401", func(t *testing.T) {
		rr := serve(h, http.MethodPost, body, signWebhook(t, apiKey, ts-301, "dlv-1", body))
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status %d", rr.Code)
		}
	})

	t.Run("empty key → 500", func(t *testing.T) {
		rr := serve(WebhookHandler[PayoutWebhookEvent]("", func(http.ResponseWriter, *http.Request, PayoutWebhookEvent) {}),
			http.MethodPost, body, signWebhook(t, apiKey, ts, "dlv-1", body))
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("status %d", rr.Code)
		}
	})

	t.Run("not JSON → 400", func(t *testing.T) {
		raw := []byte("not json")
		rr := serve(h, http.MethodPost, raw, signWebhook(t, apiKey, ts, "dlv-1", raw))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status %d", rr.Code)
		}
	})

	t.Run("GET → 405", func(t *testing.T) {
		rr := serve(h, http.MethodGet, nil, nil)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status %d", rr.Code)
		}
	})
}

func ExampleVerifyWebhook() {
	apiKey := "test_api_key_123"
	body := []byte(`{"event":"payout.paid","uuid":"abc"}`)
	ts := time.Now().Unix()
	sig, _ := SignWebhookV1(apiKey, ts, "7c9e6679-7425-40de-944b-e07fc1f90ae7", body)

	header := http.Header{}
	header.Set(HeaderWebhookDelivery, "7c9e6679-7425-40de-944b-e07fc1f90ae7")
	header.Set(HeaderTimestamp, strconv.FormatInt(ts, 10))
	header.Set(HeaderSignature, sig)

	err := VerifyWebhook(apiKey, body, header)
	fmt.Println(err)

	err = VerifyWebhook(apiKey, append(body, ' '), header)
	fmt.Println(errors.Is(err, ErrWebhookSignature))
	// Output:
	// <nil>
	// true
}
