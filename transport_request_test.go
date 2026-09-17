package cryptochief

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// TestRequest_SignedGET sends a GET through the client and has the mock
// gateway verify it: the method is signed, the body is empty and a bodiless
// request carries no Content-Type.
func TestRequest_SignedGET(t *testing.T) {
	g, srv := newMockGateway(t, "")
	g.body = func(*http.Request) string { return `{"credits":"12.5"}` }

	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))
	var out struct {
		Credits string `json:"credits"`
	}
	if err := c.Request(context.Background(), http.MethodGet, "/v1/balance", nil, &out); err != nil {
		t.Fatalf("GET /v1/balance: %v", err)
	}
	if out.Credits != "12.5" {
		t.Errorf("credits = %q", out.Credits)
	}
	sent := g.sent()
	if len(sent) != 1 {
		t.Fatalf("accepted %d requests", len(sent))
	}
	s := sent[0]
	if s.Method != http.MethodGet {
		t.Errorf("method = %q", s.Method)
	}
	if len(s.Body) != 0 {
		t.Errorf("body = %q, want none", s.Body)
	}
	if s.ContentType != "" {
		t.Errorf("Content-Type = %q, want none on a bodiless request", s.ContentType)
	}
	if err := verifyHMACv1(s, hmacTestAPIKey, "/v1/balance"); err != nil {
		t.Error(err)
	}
}

// TestRequest_MethodUpperCase checks a method in any mix of cases reaches the
// wire in the case it is signed in — otherwise a lower-case method signs GET
// and sends get.
func TestRequest_MethodUpperCase(t *testing.T) {
	g, srv := newMockGateway(t, "")

	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))
	for _, method := range []string{"get", "Get", "gEt", "GET"} {
		if err := c.Request(context.Background(), method, "/v1/balance", nil, nil); err != nil {
			t.Fatalf("%s /v1/balance: %v", method, err)
		}
		s := g.sent()[len(g.sent())-1]
		if s.Method != http.MethodGet {
			t.Errorf("%s: method on the wire = %q, want GET", method, s.Method)
		}
		if err := verifyHMACv1(s, hmacTestAPIKey, "/v1/balance"); err != nil {
			t.Errorf("%s: %v", method, err)
		}
	}
}

// TestRequest_MethodNonASCII: an HTTP method is an RFC 9110 token, so a
// non-ASCII one never leaves the client — it is refused before the request is
// built, not folded into some other method by Unicode case mapping.
func TestRequest_MethodNonASCII(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("%s %s reached the server", r.Method, r.RequestURI)
	}))
	defer srv.Close()

	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))
	for _, method := range []string{"pöst", "poſt", "ıNFO", "ЗАПРОС"} {
		err := c.Request(context.Background(), method, "/v1/balance", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "invalid method") {
			t.Errorf("%q: %v, want it refused as an invalid method", method, err)
		}
	}

	// The signing function keeps the bytes it was given: a–z go up, the rest
	// stay, so the string to sign is not the Unicode upper case.
	in := HMACv1Input{
		Timestamp: strconv.FormatInt(hmacTestUnix, 10),
		Nonce:     "0123456789abcdef0123456789abcdef",
		Method:    "poſt",
		Path:      "/v1/balance",
		Merchant:  hmacTestMerchant,
	}
	sts, err := StringToSignHMACv1(in)
	if err != nil {
		t.Fatal(err)
	}
	if line := strings.Split(sts, "\n")[3]; line != "POſT" {
		t.Errorf("method line = %q, want %q", line, "POſT")
	}
}

// TestRequest_MethodIsSigned checks the method is part of the string to sign:
// the same route under another method gets another signature.
func TestRequest_MethodIsSigned(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.capture(r)
		_, _ = io.WriteString(w, `null`)
	}))
	defer srv.Close()

	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))
	c.now = fixedClock(hmacTestUnix)
	ctx := context.Background()
	if err := c.Request(ctx, http.MethodGet, "/v1/balance", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Request(ctx, http.MethodDelete, "/v1/balance", nil, nil); err != nil {
		t.Fatal(err)
	}
	sent := rec.all()
	for i, want := range []string{http.MethodGet, http.MethodDelete} {
		if sent[i].Method != want {
			t.Fatalf("attempt %d: method = %q, want %q", i, sent[i].Method, want)
		}
		if err := verifyHMACv1(sent[i], hmacTestAPIKey, "/v1/balance"); err != nil {
			t.Errorf("%s: %v", want, err)
		}
	}
	if sent[0].HMAC == sent[1].HMAC {
		t.Error("GET and DELETE produced the same signature")
	}
}

// TestRequest_PathSignedPercentDecoded checks a path carrying a percent escape
// goes on the wire escaped and is signed decoded — the form the server reads
// from r.URL.Path. The query keeps the spelling it was written in.
func TestRequest_PathSignedPercentDecoded(t *testing.T) {
	g, srv := newMockGateway(t, "")
	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))

	for _, path := range []string{
		"/v1/orders/payout%2F8814",
		"/v1/wallets/info%20x",
		"/v1/orders/%D1%8F",
		"/v1/orders/a%7Eb",
		"/v1/orders/a%2Bb",
		"/v1/orders/100%25",
	} {
		if err := c.Request(context.Background(), http.MethodGet, path, nil, nil); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		s := g.sent()[len(g.sent())-1]
		if s.RequestURI != path {
			t.Errorf("%s: request URI = %q, want the escaped spelling as written", path, s.RequestURI)
		}
		// The mock gateway signed s.URLPath, the decoded path, and accepted
		// the request; verifyHMACv1 pins the same string to sign here.
		if err := verifyHMACv1(s, hmacTestAPIKey, s.URLPath); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}

	// A fragment never reaches the server, so it is not part of the signed
	// route either.
	if err := c.Request(context.Background(), http.MethodGet, "/v1/orders/x#frag?a=1", nil, nil); err != nil {
		t.Fatalf("path with a fragment: %v", err)
	}
	if s := g.sent()[len(g.sent())-1]; s.URLPath != "/v1/orders/x" || s.RawQuery != "" {
		t.Errorf("fragment: path = %q, query = %q", s.URLPath, s.RawQuery)
	}

	// The query is signed as the URL carries it, not decoded.
	if err := c.Request(context.Background(), http.MethodGet,
		"/v1/orders/payout%2F8814?a=1&b=%D1%8F&c", nil, nil); err != nil {
		t.Fatalf("escaped path with a query: %v", err)
	}
	s := g.sent()[len(g.sent())-1]
	if s.URLPath != "/v1/orders/payout/8814" {
		t.Errorf("path the server signs = %q", s.URLPath)
	}
	if s.RawQuery != "a=1&b=%D1%8F&c" {
		t.Errorf("query = %q, want the raw spelling", s.RawQuery)
	}
	if err := verifyHMACv1(s, hmacTestAPIKey, "/v1/orders/payout/8814"); err != nil {
		t.Error(err)
	}
}

// TestRequest_PathNonASCII checks a route and a query written with non-ASCII
// characters rather than escapes: the path goes on the wire percent-encoded
// and is signed decoded, the query goes and is signed in the spelling it was
// written in.
func TestRequest_PathNonASCII(t *testing.T) {
	g, srv := newMockGateway(t, "")
	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))

	for _, tc := range []struct {
		path, wantRoute, wantQuery, wantURI string
	}{
		{
			path: "/v1/заказ/№1", wantRoute: "/v1/заказ/№1",
			wantURI: "/v1/%D0%B7%D0%B0%D0%BA%D0%B0%D0%B7/%E2%84%961",
		},
		{
			path: "/v1/orders/я?q=я&e=%D1%8F", wantRoute: "/v1/orders/я",
			wantQuery: "q=я&e=%D1%8F",
			wantURI:   "/v1/orders/%D1%8F?q=я&e=%D1%8F",
		},
		{
			path: "/v1/%D0%B7%D0%B0%D0%BA%D0%B0%D0%B7/%2F%20x?a=%20b&c=д",
			// The escaped and the literal spelling of the same route sign the
			// same string.
			wantRoute: "/v1/заказ// x", wantQuery: "a=%20b&c=д",
			wantURI: "/v1/%D0%B7%D0%B0%D0%BA%D0%B0%D0%B7/%2F%20x?a=%20b&c=д",
		},
	} {
		if err := c.Request(context.Background(), http.MethodGet, tc.path, nil, nil); err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		s := g.sent()[len(g.sent())-1]
		if s.RequestURI != tc.wantURI {
			t.Errorf("%s: request URI = %q, want %q", tc.path, s.RequestURI, tc.wantURI)
		}
		if s.URLPath != tc.wantRoute {
			t.Errorf("%s: path the server signs = %q, want %q", tc.path, s.URLPath, tc.wantRoute)
		}
		if s.RawQuery != tc.wantQuery {
			t.Errorf("%s: query = %q, want %q", tc.path, s.RawQuery, tc.wantQuery)
		}
		if err := verifyHMACv1(s, hmacTestAPIKey, tc.wantRoute); err != nil {
			t.Errorf("%s: %v", tc.path, err)
		}
	}
}

// TestRequest_PathDecodingToLineBreak checks an escape that decodes to CR or
// LF is refused before the request goes out: the server would read the same
// line break out of r.URL.Path and refuse the signature.
func TestRequest_PathDecodingToLineBreak(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("%s %s reached the server", r.Method, r.RequestURI)
	}))
	defer srv.Close()

	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))
	for _, path := range []string{"/v1/orders/a%0Ab", "/v1/orders/a%0Db"} {
		err := c.Request(context.Background(), http.MethodGet, path, nil, nil)
		if !errors.Is(err, ErrHMACv1LineBreak) {
			t.Errorf("%s: %v, want ErrHMACv1LineBreak", path, err)
		}
	}
}

// TestRequest_PathSignedPercentDecodedPOST checks the decoded path applies to
// the service methods as well — they share the transport — and that a request
// with a body still carries Content-Type.
func TestRequest_PathSignedPercentDecodedPOST(t *testing.T) {
	g, srv := newMockGateway(t, "/prefix")

	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL+"/prefix"), WithRetries(0))
	if err := c.do(context.Background(), "/v1/wallets/info%20x", map[string]string{"a": "1"}, nil); err != nil {
		t.Fatalf("escaped path: %v", err)
	}
	s := g.sent()[0]
	if s.ContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json on a request with a body", s.ContentType)
	}
	if err := verifyHMACv1(s, hmacTestAPIKey, "/v1/wallets/info x"); err != nil {
		t.Error(err)
	}
}

// TestRequest_UnescapedPathUnchanged checks a route without percent escapes is
// signed exactly as written — every route of the processing API.
func TestRequest_UnescapedPathUnchanged(t *testing.T) {
	g, srv := newMockGateway(t, "")

	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))
	for _, path := range []string{"/v1/payout/execute", "/v1/orders/payout-8814", "/v1/a+b/c~d"} {
		if err := c.Request(context.Background(), http.MethodPost, path, map[string]string{}, nil); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	for i, path := range []string{"/v1/payout/execute", "/v1/orders/payout-8814", "/v1/a+b/c~d"} {
		if err := verifyHMACv1(g.sent()[i], hmacTestAPIKey, path); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

func TestRequest_BadArguments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("%s %s reached the server", r.Method, r.URL)
	}))
	defer srv.Close()

	c, _ := New(hmacTestMerchant, hmacTestAPIKey, WithBaseURL(srv.URL), WithRetries(0))
	ctx := context.Background()
	for name, tc := range map[string]struct {
		method, path, want string
	}{
		"no method":      {"", "/v1/balance", "method is required"},
		"relative path":  {http.MethodGet, "v1/balance", `must start with "/"`},
		"invalid escape": {http.MethodGet, "/v1/orders/%zz", "invalid URL escape"},
		"escape cut off": {http.MethodGet, "/v1/orders/%2", "invalid URL escape"},
		"line break":     {"GET\nPOST", "/v1/balance", "invalid method"},
	} {
		err := c.Request(ctx, tc.method, tc.path, nil, nil)
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want it to mention %q", name, err, tc.want)
		}
	}
}
