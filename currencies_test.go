package cryptochief

import (
	"context"
	"encoding/json"
	"testing"
)

// /v1/currencies/fiats answers with a BARE ARRAY, not the {"items":[…]} envelope
// most list endpoints use. A decoder written for the envelope compiles fine and
// fails only against the real platform, so the array is what the test feeds it.
func TestFiats_BareArrayDecodes(t *testing.T) {
	var path, body string
	srv := captureServer(t, `[
		{"code":"JMD","name":"Jamaican Dollar"},
		{"code":"KYD","name":"Cayman Islands Dollar"},
		{"code":"SEK","name":"Swedish Krona"}
	]`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	fiats, err := c.Currencies.Fiats(context.Background())
	if err != nil {
		t.Fatalf("Fiats: %v", err)
	}
	if path != "/v1/currencies/fiats" {
		t.Errorf("path = %q", path)
	}
	// Nothing to filter by, but the empty object still has to be signed.
	if body != "{}" {
		t.Errorf("body = %q, want an empty object", body)
	}

	if len(fiats) != 3 {
		t.Fatalf("fiats = %d, want the whole array decoded", len(fiats))
	}
	if fiats[0].Code != "JMD" || fiats[0].Name != "Jamaican Dollar" {
		t.Errorf("fiats[0] = %+v", fiats[0])
	}
	if fiats[2].Code != "SEK" || fiats[2].Name != "Swedish Krona" {
		t.Errorf("fiats[2] = %+v", fiats[2])
	}
}

// The whole point of /v1/currencies/cryptos is by_exchange: which exchange can
// quote which ticker. One exchange decoding is not evidence the map does, so the
// fixture carries four of them, with tickers that overlap and tickers that do
// not.
func TestCryptos_ByExchangeMapDecodes(t *testing.T) {
	var path, body string
	srv := captureServer(t, `{
		"by_exchange": {
			"binance": ["0G","1000CAT","1000CHEEMS","1000SATS"],
			"bybit":   ["0G","1INCH","AAVE"],
			"exmo":    ["AAVE","ADA","BCH"],
			"kucoin":  ["0G","1INCH","A2Z","AAVE"]
		},
		"count": 2529,
		"quote": "USDT",
		"tickers": ["0G","1000CAT","1000CHEEMS","1000SATS","1INCH","A2Z","AAVE","ADA","BCH"]
	}`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	out, err := c.Currencies.Cryptos(context.Background())
	if err != nil {
		t.Fatalf("Cryptos: %v", err)
	}
	if path != "/v1/currencies/cryptos" {
		t.Errorf("path = %q", path)
	}
	// Nothing to filter by, but the empty object still has to be signed.
	if body != "{}" {
		t.Errorf("body = %q, want an empty object", body)
	}

	if len(out.ByExchange) != 4 {
		t.Fatalf("by_exchange = %d exchanges, want all four decoded", len(out.ByExchange))
	}
	if got := out.ByExchange["binance"]; len(got) != 4 || got[0] != "0G" || got[3] != "1000SATS" {
		t.Errorf("by_exchange[binance] = %v", got)
	}
	// exmo is the one exchange here that does not carry 0G — a map collapsed
	// into a single list would lose exactly that.
	if got := out.ByExchange["exmo"]; len(got) != 3 || got[0] != "AAVE" || got[2] != "BCH" {
		t.Errorf("by_exchange[exmo] = %v", got)
	}
	if got := out.ByExchange["kucoin"]; len(got) != 4 || got[2] != "A2Z" {
		t.Errorf("by_exchange[kucoin] = %v", got)
	}

	if out.Quote != "USDT" {
		t.Errorf("quote = %q, want the asset the rates are against", out.Quote)
	}
	// count is the platform's own tally of the union and is far larger than the
	// truncated fixture — it must be reported as sent, not derived from tickers.
	if out.Count != 2529 {
		t.Errorf("count = %d, want the value the platform sent", out.Count)
	}
	if len(out.Tickers) != 9 || out.Tickers[0] != "0G" || out.Tickers[8] != "BCH" {
		t.Errorf("tickers = %v", out.Tickers)
	}
}

// The service builds this array from a Go slice, so an EMPTY result reaches the
// wire as a literal `null` rather than `[]`. A method whose signature promises a
// list must answer that with an empty list: no error, no nil slice, no decode
// failure.
func TestFiats_NullBodyIsAnEmptyList(t *testing.T) {
	var path, body string
	srv := captureServer(t, `null`, &path, &body)

	c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
	fiats, err := c.Currencies.Fiats(context.Background())
	if err != nil {
		t.Fatalf("Fiats on a null body: %v, want no error", err)
	}
	if fiats == nil {
		t.Fatal("fiats = nil, want an empty slice - a nil slice re-marshals as null and spreads the defect")
	}
	if len(fiats) != 0 {
		t.Fatalf("fiats = %v, want it empty", fiats)
	}
	out, err := json.Marshal(fiats)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != "[]" {
		t.Errorf("re-marshalled = %s, want []", out)
	}
	for range fiats { // must not panic
		t.Error("an empty list yielded an element")
	}
}

// /v1/currencies/cryptos can answer `null` for the whole body, and its map is
// built the same way — so by_exchange, and each exchange's list inside it, can
// be null on their own too. None of the three is an error, and none may leave
// the caller holding a nil collection: a nil map cannot be assigned to.
func TestCryptos_NullBodyAndNullsInsideAreEmptyCollections(t *testing.T) {
	cases := []struct {
		name string
		resp string
	}{
		{"whole body null", `null`},
		{"nested nulls", `{"tickers":null,"by_exchange":null,"quote":"USDT","count":0}`},
		{"null inside the map", `{"tickers":[],"by_exchange":{"binance":null},"quote":"USDT","count":0}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var path, body string
			srv := captureServer(t, tc.resp, &path, &body)

			c, _ := New("m", "k", WithBaseURL(srv.URL), WithRetries(0))
			out, err := c.Currencies.Cryptos(context.Background())
			if err != nil {
				t.Fatalf("Cryptos: %v, want no error", err)
			}
			if out == nil {
				t.Fatal("out = nil, want a usable value")
			}
			if out.Tickers == nil {
				t.Error("Tickers = nil, want an empty slice")
			}
			if out.ByExchange == nil {
				t.Fatal("ByExchange = nil, want an empty map - assigning to a nil map panics")
			}
			for exchange, tickers := range out.ByExchange {
				if tickers == nil {
					t.Errorf("ByExchange[%q] = nil, want an empty slice", exchange)
				}
			}
			// A returned map must be usable, not just readable.
			out.ByExchange["kraken"] = []string{"BTC"}

			// A ticker nobody carries simply has no provider - it must not panic
			// on the way to that answer.
			if got := out.ByExchange["exmo"]; len(got) != 0 {
				t.Errorf("ByExchange[exmo] = %v, want nothing", got)
			}
		})
	}
}
